// Package middleware provides the two auth guards every facade service uses,
// replacing the legacy custom Authenticate middleware:
//
//   - Token (auth:token): mandatory bearer token; 401 on failure.
//   - Repo  (auth:repo):  optional; falls back to the public user.
//
// The resolved user is stored in the request context.
package middleware

import (
	"context"
	"net/http"

	"github.com/reporangler/reporangler/internal/authclient"
	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/types"
)

type ctxKey string

const userKey ctxKey = "reporangler.user"

// UserFrom returns the authenticated user stored by Token/Repo.
func UserFrom(ctx context.Context) (types.User, bool) {
	u, ok := ctx.Value(userKey).(types.User)
	return u, ok
}

func withUser(r *http.Request, u types.User) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), userKey, u))
}

// Token enforces a valid bearer token.
func Token(ac *authclient.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if h == "" {
				httpx.Error(w, http.StatusUnauthorized, "Unauthenticated.")
				return
			}
			u, err := ac.Check(r.Context(), h)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "Unauthenticated.")
				return
			}
			next.ServeHTTP(w, withUser(r, u))
		})
	}
}

// Repo allows anonymous access, resolving to the public user when no valid
// token is presented.
func Repo(ac *authclient.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := types.PublicUser()
			if h := r.Header.Get("Authorization"); h != "" {
				if got, err := ac.Check(r.Context(), h); err == nil {
					u = got
				}
			}
			next.ServeHTTP(w, withUser(r, u))
		})
	}
}
