package kafka

import (
	"testing"

	"marketmind/services/market-data-service/internal/infra/security"
)

func TestProductionTransportAndDurabilityPolicies(t *testing.T) {
	base := Config{Brokers: []string{"broker.example:9093"}, Production: true, TLS: true, Mechanism: "scram-sha-512", Username: "ingestor", Password: "not-logged"}
	client, err := NewConfigured(base)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if client.transport.TLS == nil || client.transport.TLS.InsecureSkipVerify || client.policy.Replicas != 3 || client.policy.MinISR != 2 {
		t.Fatal("insecure production defaults")
	}
	for name, mutate := range map[string]func(*Config){
		"plaintext":          func(c *Config) { c.TLS = false },
		"single replica":     func(c *Config) { c.Replicas = 1 },
		"weak ISR":           func(c *Config) { c.MinISR = 1 },
		"no authentication":  func(c *Config) { c.Mechanism = "" },
		"missing password":   func(c *Config) { c.Password = "" },
		"unsupported auth":   func(c *Config) { c.Mechanism = "unsafe" },
		"missing mTLS files": func(c *Config) { c.Mechanism = "mtls"; c.TLSFiles = security.TLSFiles{} },
	} {
		t.Run(name, func(t *testing.T) {
			c := base
			mutate(&c)
			if client, err := NewConfigured(c); err == nil {
				client.Close()
				t.Fatal("invalid production policy accepted")
			}
		})
	}
}
