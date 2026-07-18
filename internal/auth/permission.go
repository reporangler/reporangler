package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/metadataclient"
	"github.com/reporangler/reporangler/internal/types"
)

// --- capability store helpers ---

// grantCapability idempotently adds a (constraint-less) capability to a user.
func (s *Store) grantCapability(ctx context.Context, userID int64, capName string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO capability_map (entity_type, entity_id, capability_id)
		SELECT 'user', $1, c.id FROM capability c
		WHERE c.name = $2
		  AND NOT EXISTS (
		    SELECT 1 FROM capability_map m
		    WHERE m.entity_type = 'user' AND m.entity_id = $1 AND m.capability_id = c.id
		  )`, userID, capName)
	return err
}

// revokeCapability removes all rows of a named capability from a user.
func (s *Store) revokeCapability(ctx context.Context, userID int64, capName string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM capability_map cm USING capability c
		WHERE cm.capability_id = c.id AND c.name = $2
		  AND cm.entity_type = 'user' AND cm.entity_id = $1`, userID, capName)
	return err
}

// countAdmins returns the number of distinct users holding IS_ADMIN_USER.
func (s *Store) countAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT cm.entity_id)
		FROM capability_map cm JOIN capability c ON c.id = cm.capability_id
		WHERE c.name = $1 AND cm.entity_type = 'user'`, types.CapIsAdminUser).Scan(&n)
	return n, err
}

// addPackageGroupAccess inserts a PACKAGE_GROUP_ACCESS row with the given jsonb
// constraint.
func (s *Store) addPackageGroupAccess(ctx context.Context, userID int64, constraint map[string]any) error {
	raw, err := json.Marshal(constraint)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO capability_map (entity_type, entity_id, capability_id, "constraint")
		SELECT 'user', $1, c.id, $2::jsonb FROM capability c WHERE c.name = $3`,
		userID, raw, types.CapPackageGroupAccess)
	return err
}

// removePackageGroupAccess deletes PACKAGE_GROUP_ACCESS rows whose constraint
// contains the match object (jsonb @>). Returns the number of rows removed.
func (s *Store) removePackageGroupAccess(ctx context.Context, userID int64, match map[string]any) (int64, error) {
	raw, err := json.Marshal(match)
	if err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM capability_map cm USING capability c
		WHERE cm.capability_id = c.id AND c.name = $3
		  AND cm.entity_type = 'user' AND cm.entity_id = $1
		  AND cm."constraint" @> $2::jsonb`, userID, raw, types.CapPackageGroupAccess)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// setPackageGroupProtected merges {"protected": <bool>} into the constraint of
// matching PACKAGE_GROUP_ACCESS rows. Returns the number of rows updated.
func (s *Store) setPackageGroupProtected(ctx context.Context, userID int64, match map[string]any, protected bool) (int64, error) {
	raw, err := json.Marshal(match)
	if err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE capability_map cm
		SET "constraint" = cm."constraint" || jsonb_build_object('protected', $4::bool),
		    updated_at = now()
		FROM capability c
		WHERE cm.capability_id = c.id AND c.name = $3
		  AND cm.entity_type = 'user' AND cm.entity_id = $1
		  AND cm."constraint" @> $2::jsonb`, userID, raw, types.CapPackageGroupAccess, protected)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// --- admin permission service ---

// GrantAdmin makes a user an admin. ErrNotFound if the user is missing,
// ErrAlreadyAdmin if they already hold IS_ADMIN_USER.
func (s *Service) GrantAdmin(ctx context.Context, userID int64) (types.User, error) {
	u, err := s.store.userByID(ctx, userID)
	if err != nil {
		return types.User{}, err
	}
	if u.IsAdminUser {
		return types.User{}, ErrAlreadyAdmin
	}
	if err := s.store.grantCapability(ctx, userID, types.CapIsAdminUser); err != nil {
		return types.User{}, err
	}
	return s.store.userByID(ctx, userID)
}

// RevokeAdmin removes IS_ADMIN_USER from a user. ErrNotFound if missing or not
// an admin, ErrLastAdmin if they are the only admin left.
func (s *Service) RevokeAdmin(ctx context.Context, userID int64) (types.User, error) {
	u, err := s.store.userByID(ctx, userID)
	if err != nil {
		return types.User{}, err
	}
	if !u.IsAdminUser {
		return types.User{}, ErrNotFound
	}
	n, err := s.store.countAdmins(ctx)
	if err != nil {
		return types.User{}, err
	}
	if !canRevokeAdmin(n) {
		return types.User{}, ErrLastAdmin
	}
	if err := s.store.revokeCapability(ctx, userID, types.CapIsAdminUser); err != nil {
		return types.User{}, err
	}
	return s.store.userByID(ctx, userID)
}

// --- admin permission handlers ---

// GrantAdminPermission handles PUT /permission/user/admin/{userId}. Admin-only;
// 400 if the target is already an admin.
func (h *Handler) GrantAdminPermission(w http.ResponseWriter, r *http.Request) {
	caller, _ := userFromCtx(r.Context())
	if !caller.IsAdminUser {
		httpx.Error(w, http.StatusForbidden, "Forbidden.")
		return
	}
	userID, ok := idOrName(r.PathValue("userId"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}
	u, err := h.svc.GrantAdmin(r.Context(), userID)
	switch {
	case errors.Is(err, ErrAlreadyAdmin):
		httpx.Error(w, http.StatusBadRequest, "user is already an admin")
		return
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	case err != nil:
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, u)
}

// RevokeAdminPermission handles DELETE /permission/user/admin/{userId}.
// Admin-only; refuses to remove the last admin.
func (h *Handler) RevokeAdminPermission(w http.ResponseWriter, r *http.Request) {
	caller, _ := userFromCtx(r.Context())
	if !caller.IsAdminUser {
		httpx.Error(w, http.StatusForbidden, "Forbidden.")
		return
	}
	userID, ok := idOrName(r.PathValue("userId"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}
	u, err := h.svc.RevokeAdmin(r.Context(), userID)
	switch {
	case errors.Is(err, ErrLastAdmin):
		httpx.Error(w, http.StatusBadRequest, "cannot remove the last admin")
		return
	case errors.Is(err, ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	case err != nil:
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, u)
}

// --- package-group permission handlers ---

// pgPermRequest is the body shared by join/leave/protect/unprotect. Group and
// repository each accept an id XOR a name.
type pgPermRequest struct {
	UserID         int64  `json:"user_id"`
	PackageGroupID *int64 `json:"package_group_id"`
	PackageGroup   string `json:"package_group"`
	RepositoryID   *int64 `json:"repository_id"`
	Repository     string `json:"repository"`
	Admin          bool   `json:"admin"`
}

// resolvedPGPerm holds a decoded, name-normalised package-group permission
// request that has already passed validation and authorisation.
type resolvedPGPerm struct {
	userID int64
	group  string
	repo   string
	admin  bool
}

// decodePGPerm parses the body, validates the id-XOR-name rules, resolves
// group/repo to names via metadata-service, and authorises the caller as a
// global admin or admin of the target package group. On any failure it writes
// the response and returns ok=false.
func (h *Handler) decodePGPerm(w http.ResponseWriter, r *http.Request) (resolvedPGPerm, bool) {
	caller, _ := userFromCtx(r.Context())

	var body pgPermRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return resolvedPGPerm{}, false
	}

	v := map[string]any{}
	if body.UserID == 0 {
		v["user_id"] = "user_id is required"
	}
	if (body.PackageGroupID == nil) == (body.PackageGroup == "") {
		v["package_group"] = "provide exactly one of package_group_id or package_group"
	}
	if (body.RepositoryID == nil) == (body.Repository == "") {
		v["repository"] = "provide exactly one of repository_id or repository"
	}
	if len(v) > 0 {
		validationError(w, "The given data was invalid.", v)
		return resolvedPGPerm{}, false
	}

	// The target user must exist.
	if _, err := h.svc.store.userByID(r.Context(), body.UserID); err != nil {
		if errors.Is(err, ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "user not found")
		} else {
			httpx.Error(w, http.StatusInternalServerError, "internal error")
		}
		return resolvedPGPerm{}, false
	}

	mc := metadataclient.New(h.metadataBaseURL, caller.Token, h.httpClient)
	group, err := resolveGroup(r.Context(), mc, body.PackageGroupID, body.PackageGroup)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unknown package group")
		return resolvedPGPerm{}, false
	}
	repo, err := resolveRepo(r.Context(), mc, body.RepositoryID, body.Repository)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "unknown repository")
		return resolvedPGPerm{}, false
	}

	if !isPackageGroupAdmin(caller, group) {
		httpx.Error(w, http.StatusForbidden, "Forbidden.")
		return resolvedPGPerm{}, false
	}

	return resolvedPGPerm{userID: body.UserID, group: group, repo: repo, admin: body.Admin}, true
}

// resolveGroup returns the package group's canonical name from an id or a name.
func resolveGroup(ctx context.Context, mc *metadataclient.Client, id *int64, name string) (string, error) {
	if id != nil {
		g, err := mc.GetPackageGroupByID(ctx, *id)
		if err != nil {
			return "", err
		}
		return g.Name, nil
	}
	g, err := mc.GetPackageGroupByName(ctx, name)
	if err != nil {
		return "", err
	}
	return g.Name, nil
}

// resolveRepo returns the repository's canonical name from an id or a name.
// metadata-service has no by-id repository lookup, so an id is resolved by
// scanning the repository list.
func resolveRepo(ctx context.Context, mc *metadataclient.Client, id *int64, name string) (string, error) {
	if id != nil {
		repos, err := mc.GetRepositories(ctx)
		if err != nil {
			return "", err
		}
		for _, rp := range repos {
			if rp.ID == *id {
				return rp.Name, nil
			}
		}
		return "", metadataclient.ErrNotFound
	}
	rp, err := mc.GetRepositoryByName(ctx, name)
	if err != nil {
		return "", err
	}
	return rp.Name, nil
}

// matchConstraint is the jsonb sub-document used to find an existing
// PACKAGE_GROUP_ACCESS row for (group, repo).
func (p resolvedPGPerm) matchConstraint() map[string]any {
	return map[string]any{"package_group": p.group, "repository": p.repo}
}

// PackageGroupJoin handles POST /permission/package-group/join: create a
// PACKAGE_GROUP_ACCESS grant for the target user.
func (h *Handler) PackageGroupJoin(w http.ResponseWriter, r *http.Request) {
	p, ok := h.decodePGPerm(w, r)
	if !ok {
		return
	}
	constraint := map[string]any{
		"package_group": p.group,
		"repository":    p.repo,
		"admin":         p.admin,
		"approved":      true,
		"protected":     false,
	}
	if err := h.svc.store.addPackageGroupAccess(r.Context(), p.userID, constraint); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"joined": true, "constraint": constraint})
}

// PackageGroupLeave handles POST /permission/package-group/leave: delete the
// PACKAGE_GROUP_ACCESS grant for (group, repo).
func (h *Handler) PackageGroupLeave(w http.ResponseWriter, r *http.Request) {
	p, ok := h.decodePGPerm(w, r)
	if !ok {
		return
	}
	n, err := h.svc.store.removePackageGroupAccess(r.Context(), p.userID, p.matchConstraint())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		httpx.Error(w, http.StatusNotFound, "no matching package-group access")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"left": true})
}

// PackageGroupProtect handles POST /permission/package-group/protect: set
// protected=true on the matching grant.
func (h *Handler) PackageGroupProtect(w http.ResponseWriter, r *http.Request) {
	h.setProtected(w, r, true)
}

// PackageGroupUnprotect handles POST /permission/package-group/unprotect: set
// protected=false on the matching grant.
func (h *Handler) PackageGroupUnprotect(w http.ResponseWriter, r *http.Request) {
	h.setProtected(w, r, false)
}

func (h *Handler) setProtected(w http.ResponseWriter, r *http.Request, protected bool) {
	p, ok := h.decodePGPerm(w, r)
	if !ok {
		return
	}
	n, err := h.svc.store.setPackageGroupProtected(r.Context(), p.userID, p.matchConstraint(), protected)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if n == 0 {
		httpx.Error(w, http.StatusNotFound, "no matching package-group access")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"protected": protected})
}
