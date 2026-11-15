package identity

import (
	"errors"
	"strings"
	"time"
	"unicode"
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

const (
	PermissionSampleRead    = "sample:read"
	PermissionMarketRead    = "market:read"
	PermissionResearchRead  = "research:read"
	PermissionResearchWrite = "research:write"
)

// UserClaims is the gateway's projection of one IdP-authorized organization
// membership, not the identity service's user/organization source of truth.
type UserClaims struct {
	Issuer      string    `json:"iss"`
	Subject     string    `json:"sub"`
	TenantID    string    `json:"tenant_id"`
	Email       string    `json:"email"`
	Roles       []Role    `json:"roles"`
	Permissions []string  `json:"permissions"`
	IssuedAt    time.Time `json:"iat"`
	ExpiresAt   time.Time `json:"exp"`
}

func (c *UserClaims) Validate(now time.Time) error {
	if c == nil || !safeClaim(c.Subject) || !safeClaim(c.TenantID) ||
		c.IssuedAt.Unix() <= 0 || c.ExpiresAt.Unix() <= 0 ||
		!c.ExpiresAt.After(c.IssuedAt) || c.IssuedAt.After(now) {
		return ErrInvalidToken
	}
	if !c.ExpiresAt.After(now) {
		return ErrExpiredToken
	}
	for _, role := range c.Roles {
		if !safeClaim(string(role)) {
			return ErrInvalidToken
		}
	}
	for _, permission := range c.Permissions {
		if !safeClaim(permission) {
			return ErrInvalidToken
		}
	}
	return nil
}

func safeClaim(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsRune(value, ',') &&
		strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) == -1
}

func (c *UserClaims) HasRole(target Role) bool {
	if c == nil || (target != RoleAdmin && target != RoleAnalyst && target != RoleViewer) {
		return false
	}
	for _, r := range c.Roles {
		if r == target || r == RoleAdmin {
			return true
		}
	}
	return false
}

func (c *UserClaims) HasPermission(target string) bool {
	if c == nil {
		return false
	}
	// Unknown actions never acquire implicit administrator privileges.
	switch target {
	case PermissionSampleRead, PermissionMarketRead, PermissionResearchRead:
		if c.HasRole(RoleViewer) || c.HasRole(RoleAnalyst) {
			return true
		}
	case PermissionResearchWrite:
		if c.HasRole(RoleAnalyst) {
			return true
		}
	default:
		return false
	}
	for _, p := range c.Permissions {
		if p == target {
			return true
		}
	}
	return false
}

func (c *UserClaims) EffectivePermissions() []string {
	permissions := []string{}
	for _, permission := range []string{PermissionSampleRead, PermissionMarketRead, PermissionResearchRead, PermissionResearchWrite} {
		if c.HasPermission(permission) {
			permissions = append(permissions, permission)
		}
	}
	return permissions
}

func (c *UserClaims) IsValidForTenant(targetTenantID string) bool {
	if c == nil || c.TenantID == "" {
		return false
	}
	if targetTenantID == "" {
		return true
	}
	return c.TenantID == targetTenantID
}
