package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/types"
)

// Errors surfaced by the user/permission logic. ErrNotFound / ErrUnauthorized
// live in store.go.
var (
	// ErrDuplicate is returned when a user with the same username or email
	// already exists (the legacy port surfaced this as a 500; we return 422).
	ErrDuplicate = errors.New("user already exists")
	// ErrAlreadyAdmin is returned when granting admin to a user who already
	// holds IS_ADMIN_USER.
	ErrAlreadyAdmin = errors.New("user is already an admin")
	// ErrLastAdmin guards against removing the final admin from the system.
	ErrLastAdmin = errors.New("cannot remove the last admin")
	// ErrForbidden is returned when the caller lacks the required capability.
	ErrForbidden = errors.New("forbidden")
)

type ctxKey string

const userCtxKey ctxKey = "auth.user"

// userFromCtx returns the authenticated user placed by RequireToken.
func userFromCtx(ctx context.Context) (types.User, bool) {
	u, ok := ctx.Value(userCtxKey).(types.User)
	return u, ok
}

// RequireToken is auth-service's own bearer guard. Unlike the other services it
// does NOT call authclient (which would loop back to itself) — it validates the
// token with its local Service.CheckToken and stashes the user in the context.
func (h *Handler) RequireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			httpx.Error(w, http.StatusUnauthorized, "Unauthenticated.")
			return
		}
		user, err := h.svc.CheckToken(r.Context(), authHeader)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "Unauthenticated.")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, user)))
	}
}

// idOrName decides how a `/user/{x}` or similar path segment should be resolved:
// an all-digit segment is a numeric id (looked up by id); anything else is a
// name (looked up by username). This makes the legacy "server guesses int vs
// string on one URL" behaviour explicit. Returns (id, true) for a numeric id.
func idOrName(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// canRevokeAdmin reports whether an admin may still be revoked given the current
// total admin count. It refuses to drop the system to zero admins.
func canRevokeAdmin(adminCount int) bool {
	return adminCount > 1
}

// isPackageGroupAdmin reports whether user may administer permissions for the
// named package group: either a global admin, or an admin of that group. This
// replaces the legacy is-package-group-admin gate that was always true
// (empty() on an Eloquent Collection).
func isPackageGroupAdmin(u types.User, group string) bool {
	return u.IsAdminUser || u.PackageGroups[group] == "admin"
}

// validationError writes the 422 envelope with a validation map, matching the
// legacy Laravel {code,message,validation} body.
func validationError(w http.ResponseWriter, message string, validation map[string]any) {
	httpx.JSON(w, http.StatusUnprocessableEntity, map[string]any{
		"code":       http.StatusUnprocessableEntity,
		"message":    message,
		"validation": validation,
	})
}
