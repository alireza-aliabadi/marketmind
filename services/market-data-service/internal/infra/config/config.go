// Package config loads explicit process settings; it never reads a development .env.
package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	app "marketmind/services/market-data-service/internal/application/ingestion"
	market "marketmind/services/market-data-service/internal/application/market"
	"marketmind/services/market-data-service/internal/infra/security"
)

type Grant struct {
	Reference        string    `json:"reference"`
	ExpiresAt        time.Time `json:"expires_at"`
	ReviewedOn       string    `json:"reviewed_on"`
	RetentionDays    int       `json:"retention_days"`
	PrivateIngestion bool      `json:"private_ingestion"`
	Series           []string  `json:"series"`
}
type GrantFile struct {
	Version   int              `json:"version"`
	Providers map[string]Grant `json:"providers"`
}
type Config struct {
	Production                                     bool
	DatabaseURL                                    string
	Brokers                                        []string
	KafkaTLS                                       security.TLSFiles
	KafkaMechanism, KafkaUsername, KafkaPassword   string
	OperationalAddress                             string
	Grants                                         map[string]Grant
	SECUserAgent, FREDKey, MassiveKey, MassivePlan string
}

func Secret(key string) (string, error) {
	value, path := os.Getenv(key), os.Getenv(key+"_FILE")
	if value != "" && path != "" {
		return "", fmt.Errorf("choose %s or its _FILE setting, not both", key)
	}
	if path == "" {
		return strings.TrimSpace(value), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot read %s secret file", key)
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 16385))
	if err != nil || len(body) > 16384 {
		return "", fmt.Errorf("invalid or oversized %s secret file", key)
	}
	return strings.TrimSpace(string(body)), nil
}

func Load() (Config, error) {
	mode := os.Getenv("MARKET_ENV")
	if mode != "" && mode != "local" && mode != "production" {
		return Config{}, fmt.Errorf("MARKET_ENV must be local or production")
	}
	c := Config{Production: mode == "production", OperationalAddress: os.Getenv("MARKET_OPERATIONAL_ADDRESS"),
		KafkaMechanism: os.Getenv("KAFKA_SASL_MECHANISM"), KafkaUsername: os.Getenv("KAFKA_SASL_USERNAME"),
		KafkaTLS:     security.TLSFiles{CA: os.Getenv("KAFKA_TLS_CA_FILE"), Certificate: os.Getenv("KAFKA_TLS_CERT_FILE"), Key: os.Getenv("KAFKA_TLS_KEY_FILE")},
		SECUserAgent: os.Getenv("SEC_EDGAR_USER_AGENT"), MassivePlan: os.Getenv("MASSIVE_STOCKS_PLAN"), Grants: map[string]Grant{}}
	for key, destination := range map[string]*string{"MARKET_DATABASE_URL": &c.DatabaseURL,
		"KAFKA_SASL_PASSWORD": &c.KafkaPassword, "FRED_API_KEY": &c.FREDKey, "MASSIVE_API_KEY": &c.MassiveKey} {
		value, err := Secret(key)
		if err != nil {
			return Config{}, err
		}
		*destination = value
	}
	for _, broker := range strings.Split(os.Getenv("KAFKA_BOOTSTRAP_SERVERS"), ",") {
		if broker = strings.TrimSpace(broker); broker != "" {
			c.Brokers = append(c.Brokers, broker)
		}
	}
	if c.OperationalAddress == "" {
		c.OperationalAddress = "127.0.0.1:8082"
	}
	if path := os.Getenv("MARKET_PROVIDER_GRANTS_FILE"); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return Config{}, fmt.Errorf("cannot read provider grant configuration")
		}
		defer file.Close()
		decoder := json.NewDecoder(io.LimitReader(file, 65537))
		decoder.DisallowUnknownFields()
		var grants GrantFile
		if decoder.Decode(&grants) != nil || decoder.Decode(new(any)) != io.EOF || grants.Version != 1 {
			return Config{}, fmt.Errorf("invalid provider grant configuration")
		}
		c.Grants = grants.Providers
		for provider, grant := range c.Grants {
			if err := validateGrant(provider, grant); err != nil {
				return Config{}, err
			}
		}
	}
	if c.Production {
		parsed, err := url.Parse(c.DatabaseURL)
		if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" || parsed.Query().Get("sslmode") != "verify-full" {
			return Config{}, fmt.Errorf("production database requires an explicit verify-full TLS URL")
		}
		if len(c.Brokers) == 0 {
			return Config{}, fmt.Errorf("production Kafka addresses required")
		}
		if c.KafkaMechanism == "mtls" {
			if c.KafkaTLS.Certificate == "" || c.KafkaTLS.Key == "" {
				return Config{}, fmt.Errorf("production Kafka mTLS credentials required")
			}
		} else if (c.KafkaMechanism != "scram-sha-512" && c.KafkaMechanism != "scram-sha-256" && c.KafkaMechanism != "plain") || c.KafkaUsername == "" || c.KafkaPassword == "" {
			return Config{}, fmt.Errorf("production Kafka requires authenticated TLS credentials")
		}
	}
	return c, nil
}

func validateGrant(provider string, grant Grant) error {
	if provider != "sec-edgar" && provider != "fred" && provider != "massive" {
		return fmt.Errorf("unknown provider grant")
	}
	date, err := time.Parse(time.DateOnly, grant.ReviewedOn)
	if strings.TrimSpace(grant.Reference) == "" || !grant.PrivateIngestion || grant.RetentionDays < 14 || grant.RetentionDays > 365 ||
		!grant.ExpiresAt.After(time.Now().UTC()) || err != nil || date.After(time.Now().UTC()) {
		return fmt.Errorf("provider grant must explicitly cover private retention, review date and a future expiry")
	}
	if provider == "fred" && len(grant.Series) == 0 {
		return fmt.Errorf("FRED grant needs a reviewed series allowlist")
	}
	return nil
}

func (c Config) RequireProvider(name string) (Grant, error) {
	grant, exists := c.Grants[name]
	if !exists || validateGrant(name, grant) != nil {
		return Grant{}, market.ERRLicenseRestricted
	}
	return grant, nil
}

func (c Config) Ready() error {
	if !c.Production {
		return nil
	}
	if len(c.Grants) == 0 {
		return market.ERRLicenseRestricted
	}
	for provider := range c.Grants {
		if _, err := c.RequireProvider(provider); err != nil {
			return err
		}
	}
	return nil
}

// Check compares event permissions against operator-controlled configuration,
// not a self-asserted boolean from a producer. It never grants AI reuse.
func (c Config) Check(record app.Record) error {
	if !c.Production {
		return nil
	}
	for _, source := range record.Sources() {
		grant, err := c.RequireProvider(source.Provider)
		if err != nil || source.License.PermissionReference != grant.Reference || source.License.Classification == "synthetic-test-only" || source.License.AIAllowed {
			return market.ERRLicenseRestricted
		}
	}
	return nil
}
