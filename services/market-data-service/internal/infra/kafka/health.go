package kafka

import (
	"context"
	"fmt"
	"strconv"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	app "marketmind/services/market-data-service/internal/application/ingestion"
)

// Check reads actual partition/config metadata. In production it refuses a
// silently under-replicated or weak-ISR topic; it never alters existing topics.
func (c *Client) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	connection, err := c.dialer.DialContext(ctx, "tcp", c.brokers[0])
	if err != nil {
		return fmt.Errorf("Kafka readiness unavailable")
	}
	defer connection.Close()
	deadline, _ := ctx.Deadline()
	_ = connection.SetDeadline(deadline)
	topics := []string{app.FilingsTopic, app.EconomicsTopic, app.PricesTopic, app.DLQTopic}
	partitions, err := connection.ReadPartitions(topics...)
	if err != nil {
		return fmt.Errorf("Kafka topic metadata unavailable")
	}
	counts := map[string]int{}
	for _, partition := range partitions {
		if partition.Error != nil || partition.Leader.Host == "" {
			return fmt.Errorf("Kafka topic leader unavailable")
		}
		if c.policy.Production && (len(partition.Replicas) < c.policy.Replicas || len(partition.Isr) < c.policy.MinISR) {
			return fmt.Errorf("Kafka topic does not meet production replication/ISR policy")
		}
		counts[partition.Topic]++
	}
	for _, topic := range topics {
		if counts[topic] < c.policy.Partitions {
			return fmt.Errorf("required Kafka partitions missing")
		}
	}
	if !c.policy.Production {
		return nil
	}
	var resources []kafkago.DescribeConfigRequestResource
	for _, topic := range topics {
		resources = append(resources, kafkago.DescribeConfigRequestResource{ResourceType: kafkago.ResourceTypeTopic, ResourceName: topic, ConfigNames: []string{"min.insync.replicas", "retention.ms", "max.message.bytes"}})
	}
	response, err := (&kafkago.Client{Addr: kafkago.TCP(c.brokers...), Transport: c.transport}).DescribeConfigs(ctx, &kafkago.DescribeConfigsRequest{Resources: resources})
	if err != nil || len(response.Resources) != len(topics) {
		return fmt.Errorf("Kafka durability configuration unavailable")
	}
	for _, resource := range response.Resources {
		if resource.Error != nil {
			return fmt.Errorf("Kafka topic configuration denied")
		}
		values := map[string]int64{}
		for _, entry := range resource.ConfigEntries {
			value, err := strconv.ParseInt(entry.ConfigValue, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid Kafka durability configuration")
			}
			values[entry.ConfigName] = value
		}
		maxRetention := c.policy.RawRetention
		if resource.ResourceName == app.DLQTopic {
			maxRetention = c.policy.DLQRetention
		}
		if values["min.insync.replicas"] < int64(c.policy.MinISR) || values["retention.ms"] <= 0 || values["retention.ms"] > maxRetention.Milliseconds() || values["max.message.bytes"] < 4*1024*1024 {
			return fmt.Errorf("Kafka topic violates durability/retention/size policy")
		}
	}
	return nil
}
