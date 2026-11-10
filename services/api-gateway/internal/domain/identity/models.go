package identity

import (
	"errors"
	"time"
)

var (
	ErrInvalidToken   = errors.New("invalid or malformed authentication token")
	ErrExpiredToken   = errors.New("authentication token has expired")
	ErrTenantMismatch = errors.New("tenant isolation violation: cross-tenant access forbidden")
	ErrUnauthorized   = errors.New("unauthorized")
	ErrForbidden      = errors.New("insufficient permissions for resource")
)

type Role string

const (
	RoleAdmin   Role = "admin"
	RoleAnalyst Role = "analyst"
	RoleViewer  Role = "viewer"
)

type UserClaims struct {
	Subject     string    `json:"sub"`
	TenantID    string    `json:"tenant_id"`
	Email       string    `json:"email"`
	Roles       []Role    `json:"roles"`
	Permissions []string  `json:"permissions"`
	IssuedAt    time.Time `json:"iat"`
	ExpiresAt   time.Time `json:"exp"`
}

func (c *UserClaims) HasRole(target Role) bool {
	for _, r := range c.Roles {
		if r == target || r == RoleAdmin {
			return true
		}
	}
	return false
}

func (c *UserClaims) HasPermission(target string) bool {
	if c.HasRole(RoleAdmin) {
		return true
	}
	for _, p := range c.Permissions {
		if p == target {
			return true
		}
	}
	return false
}

func (c *UserClaims) IsValidForTenant(targetTenantID string) bool {
	if targetTenantID == "" {
		return true
	}
	return c.TenantID == targetTenantID
}
