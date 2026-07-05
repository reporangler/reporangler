// Package metadata implements the RepoRangler package-index service: the
// Postgres-backed system of record for packages, package-groups and
// repositories. All routes require a bearer token (auth:token); mutating
// package-group/repository routes additionally require an admin user.
package metadata

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/middleware"
	"github.com/reporangler/reporangler/internal/types"
)

// Handler adapts Store to HTTP.
type Handler struct{ store *Store }

// NewHandler wraps a Store.
func NewHandler(store *Store) *Handler { return &Handler{store: store} }

// requireAdmin writes a 403 {code,message} and returns false unless the user is
// an admin. Mirrors the legacy is-admin gate (fixed to actually deny).
func requireAdmin(w http.ResponseWriter, user types.User) bool {
	if !user.IsAdminUser {
		httpx.Error(w, http.StatusForbidden, "This action is unauthorized.")
		return false
	}
	return true
}

func parseID(s string) (int64, bool) {
	if !isNumericID(s) {
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil
}

// --- Packages ---

// ListPackages handles GET /package/{repository}: the full package list for a
// repository, ACL-filtered to what the caller may see.
func (h *Handler) ListPackages(w http.ResponseWriter, r *http.Request) {
	repo, err := h.store.RepositoryByName(r.Context(), r.PathValue("repository"))
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "repository not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	pkgs, err := h.store.ListPackages(r.Context(), repo.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	user, _ := middleware.UserFrom(r.Context())
	filtered := filterPackagesForUser(user, pkgs)
	httpx.JSON(w, http.StatusOK, types.ListResponse[types.Package]{Count: len(filtered), Data: filtered})
}

// UpsertPackage handles POST /package/{repository}: create-or-update a package
// version keyed on (name, version, package_group). No admin gate — publishers
// (any authenticated user with repo access) use this.
func (h *Handler) UpsertPackage(w http.ResponseWriter, r *http.Request) {
	repo, err := h.store.RepositoryByName(r.Context(), r.PathValue("repository"))
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "repository not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	var body struct {
		Name         string         `json:"name"`
		Version      string         `json:"version"`
		PackageGroup string         `json:"package_group"`
		Definition   map[string]any `json:"definition"`
		StorageKey   string         `json:"storage_key"`
		PackageType  string         `json:"package_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "invalid request body")
		return
	}
	if body.Name == "" || body.Version == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, "name and version are required")
		return
	}
	pkg, err := h.store.UpsertPackage(r.Context(), repo.ID, body.PackageGroup, body.Name, body.Version, body.Definition, body.StorageKey, body.PackageType)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, pkg)
}

// PackagesByName handles GET /package/{repository}/by-name/{name}: every version
// of one package. The name segment is URL-decoded for us by r.PathValue.
func (h *Handler) PackagesByName(w http.ResponseWriter, r *http.Request) {
	repo, err := h.store.RepositoryByName(r.Context(), r.PathValue("repository"))
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "repository not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	pkgs, err := h.store.PackagesByName(r.Context(), repo.ID, r.PathValue("name"))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, types.ListResponse[types.Package]{Count: len(pkgs), Data: pkgs})
}

// DeletePackage handles DELETE /package/{repository}/{id}.
func (h *Handler) DeletePackage(w http.ResponseWriter, r *http.Request) {
	repo, err := h.store.RepositoryByName(r.Context(), r.PathValue("repository"))
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "repository not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "package not found")
		return
	}
	deleted, err := h.store.DeletePackage(r.Context(), repo.ID, id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// --- Package groups ---

// ListPackageGroups handles GET /package-group/.
func (h *Handler) ListPackageGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.store.ListPackageGroups(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, types.ListResponse[types.PackageGroup]{Count: len(groups), Data: groups})
}

// CreatePackageGroup handles POST /package-group/ (admin). 422 on duplicate.
func (h *Handler) CreatePackageGroup(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFrom(r.Context())
	if !requireAdmin(w, user) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, "name is required")
		return
	}
	group, err := h.store.CreatePackageGroup(r.Context(), body.Name)
	if errors.Is(err, ErrDuplicate) {
		httpx.Error(w, http.StatusUnprocessableEntity, "package group already exists")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, group)
}

// UpdatePackageGroup handles PUT /package-group/ (admin). id comes in the body.
func (h *Handler) UpdatePackageGroup(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFrom(r.Context())
	if !requireAdmin(w, user) {
		return
	}
	var body struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 || body.Name == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, "id and name are required")
		return
	}
	group, err := h.store.UpdatePackageGroup(r.Context(), body.ID, body.Name)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "package group not found")
		return
	}
	if errors.Is(err, ErrDuplicate) {
		httpx.Error(w, http.StatusUnprocessableEntity, "package group already exists")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, group)
}

// GetPackageGroup handles GET /package-group/{idOrName}: one handler that
// branches on whether the path value is all digits (→ by id) or not (→ by name).
func (h *Handler) GetPackageGroup(w http.ResponseWriter, r *http.Request) {
	idOrName := r.PathValue("idOrName")
	var (
		group types.PackageGroup
		err   error
	)
	if id, ok := parseID(idOrName); ok {
		group, err = h.store.PackageGroupByID(r.Context(), id)
	} else {
		group, err = h.store.PackageGroupByName(r.Context(), idOrName)
	}
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "package group not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, group)
}

// DeletePackageGroup handles DELETE /package-group/{id} (admin).
func (h *Handler) DeletePackageGroup(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFrom(r.Context())
	if !requireAdmin(w, user) {
		return
	}
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "package group not found")
		return
	}
	err := h.store.DeletePackageGroup(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "package group not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": []int64{id}})
}

// --- Repositories ---

// ListRepositories handles GET /repository/.
func (h *Handler) ListRepositories(w http.ResponseWriter, r *http.Request) {
	repos, err := h.store.ListRepositories(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, types.ListResponse[types.Repository]{Count: len(repos), Data: repos})
}

// CreateRepository handles POST /repository/ (admin). 422 on duplicate.
func (h *Handler) CreateRepository(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFrom(r.Context())
	if !requireAdmin(w, user) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, "name is required")
		return
	}
	repo, err := h.store.CreateRepository(r.Context(), body.Name)
	if errors.Is(err, ErrDuplicate) {
		httpx.Error(w, http.StatusUnprocessableEntity, "repository already exists")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, repo)
}

// GetRepository handles GET /repository/{idOrName}: one handler branching on
// numeric id vs name.
func (h *Handler) GetRepository(w http.ResponseWriter, r *http.Request) {
	idOrName := r.PathValue("idOrName")
	var (
		repo types.Repository
		err  error
	)
	if id, ok := parseID(idOrName); ok {
		repo, err = h.store.RepositoryByID(r.Context(), id)
	} else {
		repo, err = h.store.RepositoryByName(r.Context(), idOrName)
	}
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "repository not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, repo)
}

// UpdateRepository handles PUT /repository/{id} (admin). id comes in the path,
// the new name in the body.
func (h *Handler) UpdateRepository(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFrom(r.Context())
	if !requireAdmin(w, user) {
		return
	}
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "repository not found")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, "name is required")
		return
	}
	repo, err := h.store.UpdateRepository(r.Context(), id, body.Name)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "repository not found")
		return
	}
	if errors.Is(err, ErrDuplicate) {
		httpx.Error(w, http.StatusUnprocessableEntity, "repository already exists")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, repo)
}

// DeleteRepository handles DELETE /repository/{id} (admin).
func (h *Handler) DeleteRepository(w http.ResponseWriter, r *http.Request) {
	user, _ := middleware.UserFrom(r.Context())
	if !requireAdmin(w, user) {
		return
	}
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "repository not found")
		return
	}
	err := h.store.DeleteRepository(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "repository not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": []int64{id}})
}
