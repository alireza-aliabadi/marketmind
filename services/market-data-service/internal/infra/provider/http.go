// Package provider contains transport policy shared only by this service's adapters.
package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	app "marketmind/services/market-data-service/internal/application/market"
	domain "marketmind/services/market-data-service/internal/domain/market"
)

const MaxBodyBytes = 32 * 1024 * 1024

func Client() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("provider redirects are not permitted")
		},
	}
}

// Endpoint overrides are deliberately restricted to loopback fixture servers.
func Endpoint(raw, production string) (string, error) {
	if raw == "" {
		return production, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		(u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		return "", fmt.Errorf("endpoint overrides must be loopback fixture URLs")
	}
	return strings.TrimRight(raw, "/"), nil
}

func Wait(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Get bounds body size, retry count, backoff and individual request duration.
// Error messages never include credential-bearing URLs or provider response bodies.
func Get(ctx context.Context, client *http.Client, target string, headers http.Header,
	gate chan struct{}, interval time.Duration) ([]byte, error) {
	return GetLimited(ctx, client, target, headers, gate, interval, "", nil)
}

func GetLimited(ctx context.Context, client *http.Client, target string, headers http.Header,
	gate chan struct{}, interval time.Duration, name string, limiter app.RequestLimiter) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case gate <- struct{}{}:
		}
		var release func() error
		if limiter != nil {
			var err error
			release, err = limiter.Acquire(ctx, name, interval)
			if err != nil {
				<-gate
				return nil, err
			}
		}
		body, retryAfter, err := getOnce(ctx, client, target, headers)
		// Serializing starts plus a minimum gap is conservative even for fast failures.
		waitErr := Wait(ctx, interval)
		if release != nil {
			if err := release(); err != nil && waitErr == nil {
				waitErr = err
			}
		}
		<-gate
		if waitErr != nil {
			return nil, waitErr
		}
		if err == nil {
			return body, nil
		}
		if err != app.ERRRateLimited && err != app.ERRUnavailable {
			return nil, err
		}
		if attempt == 2 || retryAfter > 5*time.Second {
			return nil, err
		}
		delay := 250 * time.Millisecond * time.Duration(1<<attempt)
		if retryAfter > delay {
			delay = retryAfter
		}
		if err = Wait(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, app.ERRUnavailable
}

func getOnce(ctx context.Context, client *http.Client, target string, headers http.Header) ([]byte, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, app.ERRInvalidResponse
	}
	req.Header = headers.Clone()
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, app.ERRUnavailable
	}
	defer response.Body.Close()
	var statusErr error
	switch {
	case response.StatusCode == 404:
		statusErr = app.ERRNotFound
	case response.StatusCode == 401 || response.StatusCode == 403:
		statusErr = app.ERRAccessDenied
	case response.StatusCode == 429:
		statusErr = app.ERRRateLimited
	case response.StatusCode >= 500:
		statusErr = app.ERRUnavailable
	case response.StatusCode != 200:
		statusErr = app.ERRInvalidResponse
	}
	if statusErr != nil {
		return nil, retryDelay(response.Header.Get("Retry-After")), statusErr
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, MaxBodyBytes+1))
	if err != nil || len(body) > MaxBodyBytes || !json.Valid(body) {
		return nil, 0, app.ERRInvalidResponse
	}
	return body, 0, nil
}

func retryDelay(raw string) time.Duration {
	if seconds, err := strconv.Atoi(raw); err == nil && seconds >= 0 {
		if seconds > 5 {
			return 6 * time.Second // Stop instead of violating a long Retry-After.
		}
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(raw); err == nil {
		return max(time.Until(date), 0)
	}
	return 0
}

func Source(name, externalID, sourceURL string, body []byte) domain.Provenance {
	hash := sha256.Sum256(body)
	policy := domain.LicensePolicy{Classification: "restricted", RetentionAllowed: false}
	if name == "sec-edgar" {
		policy = domain.LicensePolicy{Classification: "sec-public-metadata", RetentionAllowed: true,
			PermissionReference: "https://www.sec.gov/search-filings/edgar-search-assistance/accessing-edgar-data"}
	}
	return domain.Provenance{
		Provider: name, ExternalID: externalID, URL: sourceURL,
		RetrievedAt: time.Now().UTC(), PayloadSHA256: hex.EncodeToString(hash[:]),
		License: policy,
	}
}
