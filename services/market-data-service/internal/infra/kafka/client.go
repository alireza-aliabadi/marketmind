package kafka

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	app "marketmind/services/market-data-service/internal/application/ingestion"
)

type Client struct {
	brokers   []string
	dialer    *kafkago.Dialer
	writer    *kafkago.Writer
	transport *kafkago.Transport
	policy    Config
}

var _ app.Publisher = (*Client)(nil)

func New(brokers []string) (*Client, error) { return newClient(brokers, "") }

// Docker publishes an ephemeral loopback port but the disposable single-node
// broker advertises its internal address. Translate only in integration tests.
func NewSingleBrokerFixture(address string) (*Client, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1") {
		return nil, fmt.Errorf("fixture broker must be loopback")
	}
	return newClient([]string{address}, address)
}

func newClient(brokers []string, fixture string) (*Client, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("Kafka bootstrap addresses required")
	}
	for _, address := range brokers {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return nil, fmt.Errorf("invalid Kafka address")
		}
	}
	dial := (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	if fixture != "" {
		dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, fixture)
		}
	}
	dialer := &kafkago.Dialer{Timeout: 10 * time.Second, DualStack: true, DialFunc: dial}
	transport := &kafkago.Transport{Dial: dial}
	writer := &kafkago.Writer{Addr: kafkago.TCP(brokers...), Balancer: &kafkago.Hash{},
		RequiredAcks: kafkago.RequireAll, MaxAttempts: 1, Async: false,
		WriteTimeout: 10 * time.Second, ReadTimeout: 10 * time.Second, BatchTimeout: 10 * time.Millisecond,
		Transport: transport, AllowAutoTopicCreation: false}
	policy := Config{Brokers: brokers}
	policy.defaults()
	return &Client{brokers: brokers, dialer: dialer, writer: writer, transport: transport, policy: policy}, nil
}

func (c *Client) Close() error {
	err := c.writer.Close()
	c.transport.CloseIdleConnections()
	return err
}

func (c *Client) EnsureTopics(ctx context.Context) error {
	connection, err := c.dialer.DialContext(ctx, "tcp", c.brokers[0])
	if err != nil {
		return fmt.Errorf("Kafka bootstrap unavailable")
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(15 * time.Second))
	controller, err := connection.Controller()
	if err != nil {
		return fmt.Errorf("Kafka controller unavailable")
	}
	admin, err := c.dialer.DialContext(ctx, "tcp", net.JoinHostPort(controller.Host, fmt.Sprint(controller.Port)))
	if err != nil {
		return fmt.Errorf("Kafka controller unavailable")
	}
	defer admin.Close()
	_ = admin.SetDeadline(time.Now().Add(15 * time.Second))
	var topics []kafkago.TopicConfig
	for _, topic := range []string{app.FilingsTopic, app.EconomicsTopic, app.PricesTopic, app.DLQTopic} {
		retention := fmt.Sprint(c.policy.RawRetention.Milliseconds())
		if topic == app.DLQTopic {
			retention = fmt.Sprint(c.policy.DLQRetention.Milliseconds())
		}
		topics = append(topics, kafkago.TopicConfig{Topic: topic, NumPartitions: c.policy.Partitions, ReplicationFactor: c.policy.Replicas,
			ConfigEntries: []kafkago.ConfigEntry{{ConfigName: "retention.ms", ConfigValue: retention},
				{ConfigName: "min.insync.replicas", ConfigValue: fmt.Sprint(c.policy.MinISR)},
				{ConfigName: "max.message.bytes", ConfigValue: "4194304"}}})
	}
	if err := admin.CreateTopics(topics...); err != nil {
		return fmt.Errorf("Kafka topic provisioning failed: %w", err)
	}
	return nil
}

func (c *Client) Publish(ctx context.Context, delivery app.Delivery) error {
	if len(delivery.Value) > 4*1024*1024 {
		return fmt.Errorf("Kafka delivery exceeds size limit")
	}
	if err := c.writer.WriteMessages(ctx, kafkago.Message{Topic: delivery.Topic, Key: []byte(delivery.Key), Value: delivery.Value}); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("Kafka publication failed")
	}
	return nil
}

type messageReader interface {
	FetchMessage(context.Context) (kafkago.Message, error)
	CommitMessages(context.Context, ...kafkago.Message) error
	Close() error
}

type Consumer struct {
	reader    messageReader
	processor app.Processor
	mu        sync.Mutex
	pending   *kafkago.Message
	handled   bool
}

func (c *Client) Consumer(group string, processor app.Processor) (*Consumer, error) {
	if !app.ValidToken(group) {
		return nil, fmt.Errorf("valid consumer group required")
	}
	reader := kafkago.NewReader(kafkago.ReaderConfig{Brokers: c.brokers,
		GroupID: group, GroupTopics: []string{app.FilingsTopic, app.EconomicsTopic, app.PricesTopic},
		Dialer: c.dialer, MinBytes: 1, MaxBytes: 4 * 1024 * 1024, MaxWait: 500 * time.Millisecond,
		CommitInterval: 0, StartOffset: kafkago.FirstOffset})
	return &Consumer{reader: reader, processor: processor}, nil
}

func (c *Consumer) Close() error { return c.reader.Close() }

func (c *Consumer) Next(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		message, err := c.reader.FetchMessage(ctx)
		if err != nil {
			return err
		}
		c.pending = &message
	}
	message := c.pending
	// FetchMessage advances the in-memory reader position before offset commit.
	// Retain this exact message across handling/commit failures, otherwise a later
	// commit on the same partition could permanently skip the unacknowledged one.
	if !c.handled {
		if err := c.processor.Handle(ctx, app.Delivery{Topic: message.Topic, Key: string(message.Key), Value: message.Value}); err != nil {
			return err
		}
		c.handled = true
	}
	if err := c.reader.CommitMessages(ctx, *message); err != nil {
		return err
	}
	c.pending, c.handled = nil, false
	return nil
}

// ReadFirst is a bounded partition probe for contract/runtime acceptance only.
func (c *Client) ReadPartition(ctx context.Context, topic string, partition int, key string) (app.Delivery, error) {
	reader := kafkago.NewReader(kafkago.ReaderConfig{Brokers: c.brokers, Topic: topic, Partition: partition,
		Dialer: c.dialer, MinBytes: 1, MaxBytes: 4 * 1024 * 1024, MaxWait: 250 * time.Millisecond})
	defer reader.Close()
	if err := reader.SetOffset(kafkago.FirstOffset); err != nil {
		return app.Delivery{}, err
	}
	for range 100 {
		message, err := reader.ReadMessage(ctx)
		if err != nil {
			return app.Delivery{}, err
		}
		if key == "" || string(message.Key) == key {
			return app.Delivery{Topic: message.Topic, Key: string(message.Key), Value: message.Value}, nil
		}
	}
	return app.Delivery{}, fmt.Errorf("diagnostic partition scan limit reached")
}
