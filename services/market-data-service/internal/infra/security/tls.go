package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

type TLSFiles struct{ CA, Certificate, Key string }

func TLS(files TLSFiles) (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if files.CA != "" {
		body, err := os.ReadFile(files.CA)
		if err != nil || len(body) > 1024*1024 {
			return nil, fmt.Errorf("cannot read bounded TLS CA file")
		}
		config.RootCAs = x509.NewCertPool()
		if !config.RootCAs.AppendCertsFromPEM(body) {
			return nil, fmt.Errorf("invalid TLS CA certificate")
		}
	}
	if (files.Certificate == "") != (files.Key == "") {
		return nil, fmt.Errorf("TLS client certificate and key must be supplied together")
	}
	if files.Certificate != "" {
		certificate, err := tls.LoadX509KeyPair(files.Certificate, files.Key)
		if err != nil {
			return nil, fmt.Errorf("cannot load TLS client certificate/key")
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	// Never expose an InsecureSkipVerify option; normal hostname/chain checks stay on.
	return config, nil
}
