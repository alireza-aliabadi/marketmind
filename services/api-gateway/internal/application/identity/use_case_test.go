package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	app "marketmind/services/api-gateway/internal/application/identity"
	domain "marketmind/services/api-gateway/internal/domain/identity"
)

type tokenVerifier struct{ claims *domain.UserClaims }

func (v tokenVerifier) VerifyToken(context.Context, string) (*domain.UserClaims, error) {
	return v.claims, nil
}

func TestAuthenticationRequiresValidTenantPrincipal(t *testing.T) {
	for _, claims := range []*domain.UserClaims{nil, {Subject: "user"}} {
		useCase := app.NewUseCase(tokenVerifier{claims})
		if _, err := useCase.Authenticate(context.Background(), "opaque-token"); !errors.Is(err, domain.ErrInvalidToken) {
			t.Fatal("invalid verifier result accepted")
		}
	}
	claims := &domain.UserClaims{Subject: "user", TenantID: "org-alpha", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	useCase := app.NewUseCase(tokenVerifier{claims})
	if _, err := useCase.Authenticate(context.Background(), ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatal("empty token accepted")
	}
	if _, err := useCase.Authenticate(context.Background(), "opaque-token"); err != nil {
		t.Fatal(err)
	}
	if useCase.EnforceTenantIsolation(claims, "org-alpha") != nil || useCase.EnforceTenantIsolation(claims, "") != nil ||
		!errors.Is(useCase.EnforceTenantIsolation(claims, "org-beta"), domain.ErrTenantMismatch) ||
		!errors.Is(useCase.EnforceTenantIsolation(nil, ""), domain.ErrTenantMismatch) {
		t.Fatal("tenant isolation policy failed")
	}
}
