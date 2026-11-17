package kafka

import (
	"crypto/tls"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go/sasl"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/segmentio/kafka-go/sasl/scram"
	"marketmind/services/market-data-service/internal/infra/security"
)

type Config struct {
	Brokers                       []string
	Production                    bool
	TLS                           bool
	TLSFiles                      security.TLSFiles
	Mechanism, Username, Password string
	Partitions, Replicas, MinISR  int
	RawRetention, DLQRetention    time.Duration
}

func (c *Config) defaults() {
	if c.Partitions == 0 {
		c.Partitions = 3
	}
	if c.Replicas == 0 {
		if c.Production {
			c.Replicas = 3
		} else {
			c.Replicas = 1
		}
	}
	if c.MinISR == 0 {
		if c.Production {
			c.MinISR = 2
		} else {
			c.MinISR = 1
		}
	}
	if c.RawRetention == 0 {
		c.RawRetention = 7 * 24 * time.Hour
	}
	if c.DLQRetention == 0 {
		c.DLQRetention = 14 * 24 * time.Hour
	}
}

func (c Config) security() (*tls.Config, sasl.Mechanism, error) {
	if c.Production && (!c.TLS || c.Replicas < 3 || c.MinISR < 2) {
		return nil, nil, fmt.Errorf("production Kafka requires TLS and at least 3 replicas/2 in-sync replicas")
	}
	if c.Partitions < 1 || c.Partitions > 100 || c.Replicas < c.MinISR || c.MinISR < 1 || c.RawRetention <= 0 || c.DLQRetention <= 0 {
		return nil, nil, fmt.Errorf("invalid Kafka durability/retention policy")
	}
	var transportTLS *tls.Config
	var mechanism sasl.Mechanism
	var err error
	if c.TLS {
		transportTLS, err = security.TLS(c.TLSFiles)
		if err != nil {
			return nil, nil, err
		}
	}
	if c.Mechanism != "" && c.Mechanism != "mtls" && !c.TLS {
		return nil, nil, fmt.Errorf("SASL credentials may not be sent without TLS")
	}
	switch c.Mechanism {
	case "":
		if c.Production {
			return nil, nil, fmt.Errorf("production Kafka authentication is required")
		}
	case "mtls":
		if transportTLS == nil || len(transportTLS.Certificates) == 0 {
			return nil, nil, fmt.Errorf("Kafka mTLS certificate required")
		}
	case "plain", "scram-sha-256", "scram-sha-512":
		if c.Username == "" || c.Password == "" {
			return nil, nil, fmt.Errorf("Kafka SASL credentials required")
		}
		if c.Mechanism == "plain" {
			mechanism = plain.Mechanism{Username: c.Username, Password: c.Password}
		} else {
			algorithm := scram.SHA512
			if c.Mechanism == "scram-sha-256" {
				algorithm = scram.SHA256
			}
			mechanism, err = scram.Mechanism(algorithm, c.Username, c.Password)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid Kafka SCRAM configuration")
			}
		}
	default:
		return nil, nil, fmt.Errorf("unsupported Kafka authentication mechanism")
	}
	return transportTLS, mechanism, nil
}

func NewConfigured(config Config) (*Client, error) {
	config.defaults()
	transportTLS, mechanism, err := config.security()
	if err != nil {
		return nil, err
	}
	client, err := newClient(config.Brokers, "")
	if err != nil {
		return nil, err
	}
	client.policy = config
	client.dialer.TLS, client.dialer.SASLMechanism = transportTLS, mechanism
	client.transport.TLS, client.transport.SASL = transportTLS, mechanism
	return client, nil
}
