package http

import (
	"context"
	"net/http"
	"strings"

	appIdentity "marketmind/services/api-gateway/internal/application/identity"
	domainIdentity "marketmind/services/api-gateway/internal/domain/identity"
)

type userContextKey string

const (
	UserClaimsKey       userContextKey = "user_claims"
	HeaderTenantID      = "X-Tenant-ID"
	HeaderUserID        = "X-User-ID"
	HeaderUserRoles     = "X-User-Roles"
)

// AuthenticateMiddleware verifies Bearer tokens and extracts tenant/user context.
func AuthenticateMiddleware(identityUseCase *appIdentity.UseCase) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":"missing or invalid authorization header"}`, http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := identityUseCase.Authenticate(r.Context(), token)
			if err != nil {
				if err == domainIdentity.ErrExpiredToken {
					http.Error(w, `{"error":"token expired"}`, http.StatusUnauthorized)
					return
				}
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			// Tenant isolation guard: verify against target tenant header if supplied
			targetTenant := r.Header.Get(HeaderTenantID)
			if targetTenant != "" && targetTenant != claims.TenantID {
				http.Error(w, `{"error":"tenant isolation violation"}`, http.StatusForbidden)
				return
			}

			// Inject user claims into request context
			ctx := context.WithValue(r.Context(), UserClaimsKey, claims)

			// Propagate identity headers for downstream microservices
			r.Header.Set(HeaderTenantID, claims.TenantID)
			r.Header.Set(HeaderUserID, claims.Subject)

			var roles []string
			for _, role := range claims.Roles {
				roles = append(roles, string(role))
			}
			r.Header.Set(HeaderUserRoles, strings.Join(roles, ","))

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole enforces role-based access control.
func RequireRole(role domainIdentity.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(UserClaimsKey).(*domainIdentity.UserClaims)
			if !ok || claims == nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			if !claims.HasRole(role) {
				http.Error(w, `{"error":"forbidden: insufficient role"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}