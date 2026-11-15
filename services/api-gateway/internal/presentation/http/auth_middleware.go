package http

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	appIdentity "marketmind/services/api-gateway/internal/application/identity"
	domainIdentity "marketmind/services/api-gateway/internal/domain/identity"
)

type userContextKey string

const (
	UserClaimsKey         userContextKey = "user_claims"
	HeaderTenantID                       = "X-Tenant-ID"
	HeaderUserID                         = "X-User-ID"
	HeaderUserRoles                      = "X-User-Roles"
	HeaderUserPermissions                = "X-User-Permissions"
)

// AuthenticateMiddleware verifies Bearer tokens and extracts tenant/user context.
func AuthenticateMiddleware(identityUseCase *appIdentity.UseCase) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorizations := r.Header.Values("Authorization")
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(authorizations) != 1 || len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				unauthorized(w, "missing or invalid authorization header")
				return
			}

			claims, err := identityUseCase.Authenticate(r.Context(), parts[1])
			if err != nil {
				if errors.Is(err, domainIdentity.ErrExpiredToken) {
					unauthorized(w, "token expired")
					return
				}
				unauthorized(w, "unauthorized")
				return
			}

			// Tenant isolation guard: verify against target tenant header if supplied
			targetTenant := r.Header.Get(HeaderTenantID)
			if len(r.Header.Values(HeaderTenantID)) > 1 || identityUseCase.EnforceTenantIsolation(claims, targetTenant) != nil {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "tenant isolation violation"})
				return
			}

			// Inject user claims into request context
			ctx := context.WithValue(r.Context(), UserClaimsKey, claims)
			request := r.Clone(ctx)
			propagateIdentity(request.Header, claims)
			next.ServeHTTP(w, request)
		})
	}
}

func unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="marketmind", error="invalid_token"`)
	writeJSON(w, http.StatusUnauthorized, map[string]string{"error": message})
}

func propagateIdentity(headers http.Header, claims *domainIdentity.UserClaims) {
	for _, header := range []string{HeaderTenantID, HeaderUserID, HeaderUserRoles, HeaderUserPermissions, "X-Organization-ID", "X-User-Email"} {
		headers.Del(header)
	}
	if claims == nil {
		return
	}
	headers.Set(HeaderTenantID, claims.TenantID)
	headers.Set(HeaderUserID, claims.Subject)
	roles := make([]string, 0, len(claims.Roles))
	for _, role := range claims.Roles {
		roles = append(roles, string(role))
	}
	headers.Set(HeaderUserRoles, strings.Join(roles, ","))
	headers.Set(HeaderUserPermissions, strings.Join(claims.EffectivePermissions(), ","))
}

// RequireRole enforces role-based access control.
func RequireRole(role domainIdentity.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(UserClaimsKey).(*domainIdentity.UserClaims)
			if !ok || claims == nil {
				unauthorized(w, "unauthorized")
				return
			}

			if !claims.HasRole(role) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden: insufficient role"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePermission grants only known, explicitly scoped actions or the
// corresponding tenant role. It must run inside AuthenticateMiddleware.
func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(UserClaimsKey).(*domainIdentity.UserClaims)
			if !ok || claims == nil {
				unauthorized(w, "unauthorized")
				return
			}
			if !claims.HasPermission(permission) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden: insufficient permission"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requireMethodPermissions(permissions map[string]string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		permission, exists := permissions[r.Method]
		if !exists {
			methods := make([]string, 0, len(permissions))
			for method := range permissions {
				methods = append(methods, method)
			}
			sort.Strings(methods)
			w.Header().Set("Allow", strings.Join(methods, ", "))
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		RequirePermission(permission)(next).ServeHTTP(w, r)
	})
}
