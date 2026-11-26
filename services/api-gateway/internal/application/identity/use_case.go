package identity

import (
	"context"
	"time"

	domain "marketmind/services/api-gateway/internal/domain/identity"
)

type UseCase struct {
	verifier TokenVerifierPort
}

func NewUseCase(verifier TokenVerifierPort) *UseCase {
	return &UseCase{
		verifier: verifier,
	}
}

func (u *UseCase) Authenticate(ctx context.Context, tokenString string) (*domain.UserClaims, error) {
	if tokenString == "" {
		return nil, domain.ErrUnauthorized
	}
	claims, err := u.verifier.VerifyToken(ctx, tokenString)
	if err != nil {
		return nil, err
	}
	if err := claims.Validate(time.Now()); err != nil {
		return nil, err
	}
	return claims, nil
}

func (u *UseCase) EnforceTenantIsolation(claims *domain.UserClaims, targetTenantID string) error {
	if !claims.IsValidForTenant(targetTenantID) {
		return domain.ErrTenantMismatch
	}
	return nil
}
