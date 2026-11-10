package identity

import (
	"context"
	domain "marketmind/services/api-gateway/internal/domain/identity"
)

type TokenVerifierPort interface {
	// parse and validate tokens cryptographically.
	VerifyToken(ctx context.Context, tokenString string) (*domain.UserClaims, error)
}