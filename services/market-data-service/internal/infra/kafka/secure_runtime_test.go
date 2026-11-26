package kafka

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	app "marketmind/services/market-data-service/internal/application/ingestion"
	"marketmind/services/market-data-service/internal/infra/security"
)

func TestSecureKafkaRuntime(t *testing.T) {
	if os.Getenv("RUN_MARKET_SECURE_TESTS") != "1" {
		t.Skip("opt-in secure private broker")
	}
	address := os.Getenv("MARKET_SECURE_BROKER")
	ctx, stop := context.WithTimeout(context.Background(), 60*time.Second)
	defer stop()
	makeClient := func(password string, ca string) *Client {
		client, err := NewConfigured(Config{Brokers: []string{address}, TLS: true, TLSFiles: security.TLSFiles{CA: ca}, Mechanism: "plain", Username: "ingestor", Password: password})
		if err != nil {
			t.Fatal(err)
		}
		dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, address)
		}
		client.dialer.DialFunc = dial
		client.transport.Dial = dial
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
	client := makeClient("synthetic-secret", os.Getenv("MARKET_SECURE_CA"))
	if err := client.EnsureTopics(ctx); err != nil {
		t.Fatal("TLS/SASL provisioning failed", err)
	}
	if err := client.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := client.Publish(ctx, app.Delivery{Topic: app.FilingsTopic, Key: "secure-wire", Value: []byte(`{"synthetic":true}`)}); err != nil {
		t.Fatal(err)
	}
	// Hash partition is deterministic for this probe; scan all three with short bounds.
	found := false
	for partition := 0; partition < 3; partition++ {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		delivery, err := client.ReadPartition(probe, app.FilingsTopic, partition, "secure-wire")
		cancel()
		if err == nil && delivery.Key == "secure-wire" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("encrypted authenticated wire message not received")
	}
	bad := makeClient("wrong-password", os.Getenv("MARKET_SECURE_CA"))
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	err := bad.Check(probe)
	cancel()
	if err == nil {
		t.Fatal("invalid SASL credentials accepted")
	}
	untrusted := makeClient("synthetic-secret", "")
	probe, cancel = context.WithTimeout(ctx, 3*time.Second)
	err = untrusted.Check(probe)
	cancel()
	if err == nil {
		t.Fatal("untrusted self-signed broker accepted")
	}
	client.policy.Production = true
	client.policy.Replicas = 3
	client.policy.MinISR = 2
	if err := client.Check(ctx); err == nil {
		t.Fatal("single-replica broker falsely marked production-ready")
	}
	t.Log("actual TLS/SASL produce/read, wrong credentials/CA denial and weak-replication readiness denial passed")
}
