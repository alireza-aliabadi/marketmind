package identity_test

import (
	"errors"
	"testing"
	"time"

	"marketmind/services/api-gateway/internal/domain/identity"
)

func TestPrincipalValidation(t *testing.T) {
	now := time.Now()
	for _, name := range []string{"valid", "nil", "missing-subject", "missing-tenant", "missing-expiry", "expired", "future-issued-at", "unsafe-permission"} {
		t.Run(name, func(t *testing.T) {
			claims := &identity.UserClaims{Subject: "user", TenantID: "org", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
			switch name {
			case "nil":
				claims = nil
			case "missing-subject":
				claims.Subject = ""
			case "missing-tenant":
				claims.TenantID = ""
			case "missing-expiry":
				claims.ExpiresAt = time.Time{}
			case "expired":
				claims.ExpiresAt = now
			case "future-issued-at":
				claims.IssuedAt = now.Add(time.Minute)
			case "unsafe-permission":
				claims.Permissions = []string{"sample:read,admin"}
			}
			err := claims.Validate(now)
			if (err == nil) != (name == "valid") {
				t.Fatalf("unexpected validation result: %v", err)
			}
			if name == "expired" && !errors.Is(err, identity.ErrExpiredToken) {
				t.Fatal("expiry boundary was not rejected")
			}
		})
	}
}

func TestPrincipalPermissionPolicy(t *testing.T) {
	for _, role := range []identity.Role{identity.RoleViewer, identity.RoleAnalyst, identity.RoleAdmin, "unknown"} {
		claims := &identity.UserClaims{Roles: []identity.Role{role}}
		if claims.HasPermission(identity.PermissionResearchWrite) != (role == identity.RoleAnalyst || role == identity.RoleAdmin) {
			t.Fatal("incorrect research write policy")
		}
		if claims.HasPermission(identity.PermissionSampleRead) != (role != "unknown") || claims.HasPermission("unregistered:action") || claims.HasRole("unknown") {
			t.Fatal("unknown roles/actions must fail closed")
		}
	}
	claims := &identity.UserClaims{Permissions: []string{identity.PermissionSampleRead}}
	if !claims.HasPermission(identity.PermissionSampleRead) || claims.HasPermission(identity.PermissionResearchWrite) {
		t.Fatal("explicit action permission was not scoped")
	}
	var missing *identity.UserClaims
	if missing.HasRole(identity.RoleAdmin) || missing.HasPermission(identity.PermissionSampleRead) || missing.IsValidForTenant("") {
		t.Fatal("missing principal must deny authorization")
	}
}
