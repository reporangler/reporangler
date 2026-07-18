package metadata

import (
	"net/http"

	"github.com/reporangler/reporangler/internal/httpx"
)

// Routes builds the metadata-service mux. Reads use the optional guard (repo:
// public fallback) so facades can serve public packages to anonymous clients —
// the ListPackages ACL then scopes results to what the (possibly public) user
// may see. Mutations use the mandatory guard (tok) plus per-handler admin
// gates. GET / (healthz) is open. The caller wraps the mux with CORS.
func Routes(h *Handler, tok, repo func(http.Handler) http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", httpx.Healthz("metadata-service"))

	protected := func(pattern string, hf http.HandlerFunc) { mux.Handle(pattern, tok(hf)) }
	open := func(pattern string, hf http.HandlerFunc) { mux.Handle(pattern, repo(hf)) }

	// Packages — reads optional (ACL-filtered), writes mandatory.
	open("GET /package/{repository}", h.ListPackages)
	protected("POST /package/{repository}", h.UpsertPackage)
	open("GET /package/{repository}/by-name/{name}", h.PackagesByName)
	protected("DELETE /package/{repository}/{id}", h.DeletePackage)

	// Package groups — reads optional, mutations admin-gated.
	open("GET /package-group/", h.ListPackageGroups)
	protected("POST /package-group/", h.CreatePackageGroup)
	protected("PUT /package-group/", h.UpdatePackageGroup)
	open("GET /package-group/{idOrName}", h.GetPackageGroup)
	protected("DELETE /package-group/{id}", h.DeletePackageGroup)

	// Repositories — reads optional, mutations admin-gated.
	open("GET /repository/", h.ListRepositories)
	protected("POST /repository/", h.CreateRepository)
	protected("PUT /repository/{id}", h.UpdateRepository)
	open("GET /repository/{idOrName}", h.GetRepository)
	protected("DELETE /repository/{id}", h.DeleteRepository)

	return mux
}
