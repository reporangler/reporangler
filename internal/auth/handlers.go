package auth

import (
	"net/http"

	"github.com/reporangler/reporangler/internal/httpx"
)

// Handler adapts Service to HTTP. metadataBaseURL + httpClient let the
// package-group permission handlers resolve group/repository names via
// metadata-service, forwarding the caller's bearer token.
type Handler struct {
	svc             *Service
	metadataBaseURL string
	httpClient      *http.Client
}

// NewHandler wraps a Service. metadataBaseURL is metadata-service's base URL,
// used to resolve package-group / repository name↔id for permission grants.
func NewHandler(svc *Service, metadataBaseURL string) *Handler {
	return &Handler{svc: svc, metadataBaseURL: metadataBaseURL, httpClient: http.DefaultClient}
}

// Login handles GET /login/api. Credentials arrive as custom headers, matching
// the legacy contract that lib-reporangler's AuthClient::login speaks.
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	loginType := r.Header.Get("reporangler-login-type")
	username := r.Header.Get("reporangler-login-username")
	password := r.Header.Get("reporangler-login-password")
	if username == "" || password == "" {
		httpx.Error(w, http.StatusBadRequest, "missing login headers")
		return
	}
	user, err := h.svc.Login(r.Context(), loginType, username, password)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	httpx.JSON(w, http.StatusOK, user)
}

// CheckToken handles GET /login/token. This is the endpoint every other
// service calls (via authclient) to introspect a bearer token.
func (h *Handler) CheckToken(w http.ResponseWriter, r *http.Request) {
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
	httpx.JSON(w, http.StatusOK, user)
}
