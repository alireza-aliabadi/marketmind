package identity_test

import (
	"errors"
	"testing"
	"time"

	"marketmind/services/identity-service/internal/domain/identity"
)

func fixture(t *testing.T) (identity.User, identity.Organization, identity.Membership) {
	t.Helper()
	created := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	organization, err := identity.NewOrganization("org-alpha", "Alpha Research", created)
	if err != nil {
		t.Fatal(err)
	}
	user, err := identity.NewUser("user-1", "https://identity.example", "oidc-sub-1", "analyst@example.test", created)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := identity.NewMembership(organization.ID, user.ID, []identity.Role{identity.RoleAnalyst}, identity.StatusActive, created)
	if err != nil {
		t.Fatal(err)
	}
	return user, organization, membership
}

func TestOrganizationUserMembershipModel(t *testing.T) {
	user, organization, membership := fixture(t)
	if user.Issuer == "" || user.Subject == "" || organization.Status != identity.StatusActive {
		t.Fatal("external identity and organization state must be explicit")
	}
	if err := membership.Authorize(user, organization, identity.PermissionResearchWrite); err != nil {
		t.Fatal(err)
	}
	// Membership in one organization never authorizes the same user in another.
	other, err := identity.NewOrganization("org-beta", "Beta Research", organization.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(membership.Authorize(user, other, identity.PermissionResearchRead), identity.ErrForbidden) {
		t.Fatal("cross-organization access was allowed")
	}
	viewer, err := identity.NewMembership(other.ID, user.ID, []identity.Role{identity.RoleViewer}, identity.StatusActive, membership.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if viewer.Authorize(user, other, identity.PermissionResearchRead) != nil || viewer.Authorize(user, other, identity.PermissionResearchWrite) == nil {
		t.Fatal("roles must be scoped to each membership")
	}
}

func TestInactiveAndMismatchedMembershipsDenyAccess(t *testing.T) {
	for _, name := range []string{"invited", "suspended-membership", "suspended-user", "suspended-organization", "wrong-user", "empty-membership", "unknown-action"} {
		t.Run(name, func(t *testing.T) {
			user, organization, membership := fixture(t)
			permission := identity.PermissionResearchRead
			switch name {
			case "invited":
				membership.Status = identity.StatusInvited
			case "suspended-membership":
				membership.Status = identity.StatusSuspended
			case "suspended-user":
				user.Status = identity.StatusSuspended
			case "suspended-organization":
				organization.Status = identity.StatusSuspended
			case "wrong-user":
				user.ID = "another-user"
			case "empty-membership":
				membership = identity.Membership{}
			case "unknown-action":
				membership.Roles = []identity.Role{identity.RoleAdmin}
				permission = "unregistered:action"
			}
			if !errors.Is(membership.Authorize(user, organization, permission), identity.ErrForbidden) {
				t.Fatal("unauthorized membership was accepted")
			}
		})
	}
}

func TestMembershipRolePolicy(t *testing.T) {
	for _, role := range []identity.Role{identity.RoleViewer, identity.RoleAnalyst, identity.RoleAdmin} {
		for _, permission := range []identity.Permission{identity.PermissionSampleRead, identity.PermissionMarketRead, identity.PermissionResearchRead, identity.PermissionResearchWrite, identity.PermissionManageOrganization} {
			t.Run(string(role)+"/"+string(permission), func(t *testing.T) {
				user, organization, membership := fixture(t)
				membership.Roles = []identity.Role{role}
				allowed := permission != identity.PermissionResearchWrite && permission != identity.PermissionManageOrganization ||
					permission == identity.PermissionResearchWrite && role != identity.RoleViewer || role == identity.RoleAdmin
				if (membership.Authorize(user, organization, permission) == nil) != allowed {
					t.Fatalf("unexpected policy for %s/%s", role, permission)
				}
			})
		}
	}
}

func TestIdentityConstructorsRejectInvalidModels(t *testing.T) {
	user, organization, membership := fixture(t)
	for _, id := range []string{"", "has spaces", "bad\nidentity"} {
		if _, err := identity.NewOrganization(id, "Example", organization.CreatedAt); err == nil {
			t.Fatal("invalid organization ID accepted")
		}
		if _, err := identity.NewUser(id, user.Issuer, user.Subject, user.Email, user.CreatedAt); err == nil {
			t.Fatal("invalid user ID accepted")
		}
		if _, err := identity.NewMembership(id, user.ID, membership.Roles, membership.Status, membership.CreatedAt); err == nil {
			t.Fatal("invalid membership ID accepted")
		}
	}
	for _, roles := range [][]identity.Role{nil, {"unknown"}, {identity.RoleAdmin, identity.RoleAdmin}} {
		if _, err := identity.NewMembership(organization.ID, user.ID, roles, identity.StatusActive, membership.CreatedAt); !errors.Is(err, identity.ErrInvalidRole) {
			t.Fatal("invalid roles accepted")
		}
	}
	if _, err := identity.NewOrganization("org", " ", organization.CreatedAt); err == nil {
		t.Fatal("blank organization name accepted")
	}
	if _, err := identity.NewUser(user.ID, "", user.Subject, user.Email, user.CreatedAt); err == nil {
		t.Fatal("missing issuer accepted")
	}
	if _, err := identity.NewMembership(organization.ID, user.ID, membership.Roles, "unknown", membership.CreatedAt); err == nil {
		t.Fatal("invalid membership status accepted")
	}
	if _, err := identity.NewOrganization("org", "Example", time.Time{}); err == nil {
		t.Fatal("missing creation time accepted")
	}
	roles := []identity.Role{identity.RoleViewer}
	copy, err := identity.NewMembership(organization.ID, user.ID, roles, identity.StatusActive, membership.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	roles[0] = identity.RoleAdmin
	if copy.Roles[0] != identity.RoleViewer {
		t.Fatal("caller mutated membership through its input slice")
	}
}
