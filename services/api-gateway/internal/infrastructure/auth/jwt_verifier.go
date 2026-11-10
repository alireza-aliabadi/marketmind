package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	domain "marketmind/services/api-gateway/internal/domain/identity"
)

type JWTVerifier struct {
	secretKey []byte
}

func NewJWTVerifier(secret string) *JWTVerifier {
	return &JWTVerifier{
		secretKey: []byte(secret),
	}
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type jwtRawClaims struct {
	Sub         string        `json:"sub"`
	TenantID    string        `json:"tenant_id"`
	Email       string        `json:"email"`
	Roles       []domain.Role `json:"roles"`
	Permissions []string      `json:"permissions"`
	Iat         int64         `json:"iat"`
	Exp         int64         `json:"exp"`
}

func (v *JWTVerifier) VerifyToken(ctx context.Context, tokenStr string) (*domain.UserClaims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, domain.ErrInvalidToken
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, domain.ErrInvalidToken
	}

	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Alg != "HS256" {
		return nil, domain.ErrInvalidToken
	}

	// Signature verification
	signedContent := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, v.secretKey)
	mac.Write([]byte(signedContent))
	expectedSignature := mac.Sum(nil)

	actualSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(actualSignature, expectedSignature) {
		return nil, domain.ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, domain.ErrInvalidToken
	}

	var rawClaims jwtRawClaims
	if err := json.Unmarshal(payloadBytes, &rawClaims); err != nil {
		return nil, domain.ErrInvalidToken
	}

	now := time.Now().Unix()
	if rawClaims.Exp > 0 && now > rawClaims.Exp {
		return nil, domain.ErrExpiredToken
	}

	return &domain.UserClaims{
		Subject:     rawClaims.Sub,
		TenantID:    rawClaims.TenantID,
		Email:       rawClaims.Email,
		Roles:       rawClaims.Roles,
		Permissions: rawClaims.Permissions,
		IssuedAt:    time.Unix(rawClaims.Iat, 0),
		ExpiresAt:   time.Unix(rawClaims.Exp, 0),
	}, nil
}
