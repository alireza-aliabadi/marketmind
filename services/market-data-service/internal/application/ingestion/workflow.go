package ingestion

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type RetryPolicy struct {
	Attempts            int
	BaseDelay, MaxDelay time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{Attempts: 3, BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second}
}

func (p RetryPolicy) Run(ctx context.Context, operation func() error) error {
	if p.Attempts < 1 || p.Attempts > 10 || p.BaseDelay < 0 || p.MaxDelay < p.BaseDelay || p.MaxDelay > time.Minute {
		return fmt.Errorf("invalid bounded retry policy")
	}
	var err error
	for attempt := 0; attempt < p.Attempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = operation()
		if err == nil || errors.Is(err, ErrInvalidData) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if attempt+1 < p.Attempts {
			timer := time.NewTimer(min(p.BaseDelay*time.Duration(1<<attempt), p.MaxDelay))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return err
}

type Processor struct {
	Repository Repository
	Decoder    Decoder
	Retry      RetryPolicy
	Policy     SourcePolicy
}

// A nil result authorizes an offset commit only after durable application or DLQ
// parking. A broken database/DLQ returns an error so Kafka redelivers the message.
func (p Processor) Handle(ctx context.Context, delivery Delivery) error {
	event, data, record, err := p.Decoder.Decode(delivery.Topic, delivery.Value)
	if err != nil || delivery.Key != data.IdempotencyKey {
		return p.Repository.Quarantine(ctx, Failure{Topic: delivery.Topic, MessageKey: delivery.Key,
			Value: delivery.Value, Code: "invalid_event", Attempts: 1, FailedAt: time.Now().UTC()})
	}
	if p.Policy != nil {
		if err := p.Policy.Check(record); err != nil {
			return err
		}
	}
	attempts := 0
	err = p.Retry.Run(ctx, func() error { attempts++; _, err := p.Repository.Apply(ctx, event, data, record); return err })
	if err == nil {
		return nil
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return p.Repository.Quarantine(ctx, Failure{Topic: delivery.Topic, MessageKey: delivery.Key,
		EventKey: data.IdempotencyKey, Value: delivery.Value, Code: "processing_failed", Attempts: attempts, FailedAt: time.Now().UTC()})
}

type Relay struct {
	Repository Repository
	Publisher  Publisher
	Retry      RetryPolicy
}

func (r Relay) Drain(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 10000 {
		return 0, fmt.Errorf("relay limit must be 1..10000")
	}
	count := 0
	for count < limit {
		handled, err := r.Repository.DispatchNext(ctx, func(delivery Delivery) DispatchResult {
			attempts := 0
			err := r.Retry.Run(ctx, func() error { attempts++; return r.Publisher.Publish(ctx, delivery) })
			return DispatchResult{Attempts: attempts, Err: err}
		})
		if err != nil {
			return count, err
		}
		if !handled {
			break
		}
		count++
	}
	return count, nil
}
