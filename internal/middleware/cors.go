package middleware

import (
	"net/http"
	"regexp"
)

// CORS reflects an Origin matching https?://<sub>.<domain>, else falls back to
// <protocol>://<domain>. Unlike the legacy PHP services, it answers OPTIONS
// preflight *with* the CORS headers (the PHP preflight route sat outside the
// CORS group and returned none — a bug fixed here).
func CORS(protocol, domain string) func(http.Handler) http.Handler {
	re := regexp.MustCompile(`^https?://[a-z0-9-]+\.` + regexp.QuoteMeta(domain) + `$`)
	fallback := protocol + "://" + domain
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allow := fallback
			if o := r.Header.Get("Origin"); re.MatchString(o) {
				allow = o
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", allow)
			h.Set("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Headers",
				"Authorization, Content-Type, Accept, reporangler-login-type, reporangler-login-username, reporangler-login-password")
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
