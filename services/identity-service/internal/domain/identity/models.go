// Package identity owns organization, user and membership invariants. It has no
// dependency on the gateway, an OIDC SDK, a database or a transport framework.
package identity

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

var (
	ErrInvalidIdentity = errors.New("invalid organization, user or membership")
	ErrInvalidRole     = errors.New("unknown or duplicate membership role")
	ErrForbidden       = errors.New("membership does not authorize this organization action")
)

type Status string

const (
	StatusActive    Status = "active"
	StatusInvited   Status = "invited"
	StatusSuspended Status = "suspended"
)

type Role string

const (
	RoleAdmin   Role = "admin"
	RoleAnalyst Role = "analyst"
	RoleViewer  Role = "viewer"
)

type Permission string

const (
	PermissionSampleRead         Permission = "sample:read"
	PermissionMarketRead         Permission = "market:read"
	PermissionResearchRead       Permission = "research:read"
	PermissionResearchWrite      Permission = "research:write"
	PermissionManageOrganization Permission = "organizations:manage"
)

// An organization is a tenant. A user can have separate memberships in multiple
// organizations; roles never live globally on User.
type Organization struct {
	ID        string
	Name      string
	Status    Status
	CreatedAt time.Time
}

// Issuer + Subject is the external identity key. Email is contact metadata, not
// an authentication or uniqueness key; the local ID remains service-owned.
type User struct {
	ID        string
	Issuer    string
	Subject   string
	Email     string
	Status    Status
	CreatedAt time.Time
}

type Membership struct {
	OrganizationID string
	UserID         string
	Roles          []Role
	Status         Status
	CreatedAt      time.Time
}

func validID(value string) bool {
	return value != "" && len(value) <= 256 && strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) == -1
}

func NewOrganization(id, name string, createdAt time.Time) (Organization, error) {
	if !validID(id) || strings.TrimSpace(name) == "" || len(name) > 200 || createdAt.IsZero() {
		return Organization{}, ErrInvalidIdentity
	}
	return Organization{ID: id, Name: strings.TrimSpace(name), Status: StatusActive, CreatedAt: createdAt.UTC()}, nil
}

func NewUser(id, issuer, subject, email string, createdAt time.Time) (User, error) {
	if !validID(id) || !validID(issuer) || !validID(subject) || createdAt.IsZero() {
		return User{}, ErrInvalidIdentity
	}
	return User{ID: id, Issuer: issuer, Subject: subject, Email: email, Status: StatusActive, CreatedAt: createdAt.UTC()}, nil
}

func NewMembership(organizationID, userID string, roles []Role, status Status, createdAt time.Time) (Membership, error) {
	if !validID(organizationID) || !validID(userID) || createdAt.IsZero() ||
		(status != StatusActive && status != StatusInvited && status != StatusSuspended) {
		return Membership{}, ErrInvalidIdentity
	}
	if len(roles) == 0 {
		return Membership{}, ErrInvalidRole
	}
	seen := map[Role]bool{}
	for _, role := range roles {
		if (role != RoleAdmin && role != RoleAnalyst && role != RoleViewer) || seen[role] {
			return Membership{}, ErrInvalidRole
		}
		seen[role] = true
	}
	return Membership{
		OrganizationID: organizationID, UserID: userID, Roles: append([]Role(nil), roles...),
		Status: status, CreatedAt: createdAt.UTC(),
	}, nil
}

func (m Membership) Authorize(user User, organization Organization, permission Permission) error {
	if m.Status != StatusActive || user.Status != StatusActive || organization.Status != StatusActive ||
		!validID(m.OrganizationID) || !validID(m.UserID) || m.OrganizationID != organization.ID || m.UserID != user.ID {
		return ErrForbidden
	}
	for _, role := range m.Roles {
		switch permission {
		case PermissionSampleRead, PermissionMarketRead, PermissionResearchRead:
			if role == RoleAdmin || role == RoleAnalyst || role == RoleViewer {
				return nil
			}
		case PermissionResearchWrite:
			if role == RoleAdmin || role == RoleAnalyst {
				return nil
			}
		case PermissionManageOrganization:
			if role == RoleAdmin {
				return nil
			}
		}
	}
	return ErrForbidden
}
