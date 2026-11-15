package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"

	"marketmind/services/api-gateway/internal/infrastructure/auth"
)

type Config struct {
	AppName       string
	AppEnv        string
	Address       string
	DownstreamURL *url.URL
	OIDC          auth.OIDCConfig
}

func Load(getenv func(string) string) (Config, error) {
	value := func(key, fallback string) string {
		if result := getenv(key); result != "" {
			return result
		}
		return fallback
	}
	config := Config{
		AppName: value("APP_NAME", "marketmind-gateway"),
		AppEnv:  value("APP_ENV", "local"),
		OIDC: auth.OIDCConfig{
			IssuerURL: getenv("OIDC_ISSUER_URL"),
			Audience:  getenv("OIDC_AUDIENCE"),
		},
	}
	allowHTTP, err := strconv.ParseBool(value("OIDC_ALLOW_INSECURE_HTTP", "false"))
	if err != nil {
		return Config{}, errors.New("OIDC_ALLOW_INSECURE_HTTP must be a boolean")
	}
	if allowHTTP && config.AppEnv != "local" && config.AppEnv != "development" && config.AppEnv != "test" {
		return Config{}, errors.New("insecure OIDC HTTP is forbidden outside local/development/test")
	}
	config.OIDC.AllowInsecureHTTP = allowHTTP
	if err := config.OIDC.Validate(); err != nil {
		return Config{}, err
	}
	port := value("GATEWAY_PORT", "8080")
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return Config{}, errors.New("GATEWAY_PORT must be between 1 and 65535")
	}
	config.Address = net.JoinHostPort(value("GATEWAY_HOST", "127.0.0.1"), port)
	target, err := url.Parse(value("DOWNSTREAM_URL", "http://127.0.0.1:8000"))
	if err != nil || target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") ||
		target.User != nil || target.Fragment != "" || target.RawQuery != "" {
		return Config{}, errors.New("DOWNSTREAM_URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	config.DownstreamURL = target
	return config, nil
}
