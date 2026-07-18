package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/types"
)

// --- store ---

// listUsers returns every user with capabilities loaded.
func (s *Store) listUsers(ctx context.Context) ([]types.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, username, email FROM "user" ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var users []types.User
	for rows.Next() {
		var u types.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email); err != nil {
			rows.Close()
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// Load capabilities after draining the row set so we don't hold the pooled
	// connection while issuing the per-user capability queries.
	for i := range users {
		caps, err := s.loadCapabilities(ctx, users[i].ID)
		if err != nil {
			return nil, err
		}
		users[i].Capability = caps
		users[i].ComputeDerived()
	}
	return users, nil
}

// userByID loads a single user (with capabilities) by numeric id.
func (s *Store) userByID(ctx context.Context, id int64) (types.User, error) {
	var u types.User
	err := s.pool.QueryRow(ctx, `SELECT id, username, email FROM "user" WHERE id = $1`, id).
		Scan(&u.ID, &u.Username, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	caps, err := s.loadCapabilities(ctx, u.ID)
	if err != nil {
		return u, err
	}
	u.Capability = caps
	u.ComputeDerived()
	return u, nil
}

// createUser inserts a user, granting IS_REST_USER so the account can use the
// REST API. The duplicate check is by username OR email (returning ErrDuplicate)
// — fixing the legacy bug that compared the plaintext password against the
// bcrypt column and surfaced dupes as a 500.
func (s *Store) createUser(ctx context.Context, username, email, passwordHash string) (types.User, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM "user" WHERE username = $1 OR email = $2)`,
		username, email).Scan(&exists); err != nil {
		return types.User{}, err
	}
	if exists {
		return types.User{}, ErrDuplicate
	}
	var id int64
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO "user" (username, email, password) VALUES ($1, $2, $3) RETURNING id`,
		username, email, passwordHash).Scan(&id); err != nil {
		return types.User{}, err
	}
	if err := s.grantCapability(ctx, id, types.CapIsRestUser); err != nil {
		return types.User{}, err
	}
	return s.userByID(ctx, id)
}

// updateUser applies optional (non-nil) fields; nil fields are left unchanged.
func (s *Store) updateUser(ctx context.Context, id int64, username, email, passwordHash *string) (types.User, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE "user" SET
			username   = COALESCE($2, username),
			email      = COALESCE($3, email),
			password   = COALESCE($4, password),
			updated_at = now()
		WHERE id = $1`, id, username, email, passwordHash)
	if err != nil {
		return types.User{}, err
	}
	if tag.RowsAffected() == 0 {
		return types.User{}, ErrNotFound
	}
	return s.userByID(ctx, id)
}

// deleteUser removes a user (capability_map + tokens cascade via FKs).
func (s *Store) deleteUser(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- service ---

// ListUsers returns all users.
func (s *Service) ListUsers(ctx context.Context) ([]types.User, error) {
	return s.store.listUsers(ctx)
}

// GetUser resolves an all-numeric segment by id, otherwise by username.
func (s *Service) GetUser(ctx context.Context, idOrNameStr string) (types.User, error) {
	if id, ok := idOrName(idOrNameStr); ok {
		return s.store.userByID(ctx, id)
	}
	u, _, err := s.store.userByUsername(ctx, idOrNameStr)
	return u, err
}

// CreateUser bcrypt-hashes the password and inserts the user.
func (s *Service) CreateUser(ctx context.Context, username, email, password string) (types.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return types.User{}, err
	}
	return s.store.createUser(ctx, username, email, string(hash))
}

// UpdateUser applies optional fields; a non-nil password is bcrypt-hashed.
func (s *Service) UpdateUser(ctx context.Context, id int64, username, email, password *string) (types.User, error) {
	var hash *string
	if password != nil {
		h, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
		if err != nil {
			return types.User{}, err
		}
		hs := string(h)
		hash = &hs
	}
	return s.store.updateUser(ctx, id, username, email, hash)
}

// DeleteUser removes a user by id.
func (s *Service) DeleteUser(ctx context.Context, id int64) error {
	return s.store.deleteUser(ctx, id)
}

// --- handlers ---

// ListUsers handles GET /user. Admin-only (the legacy service had no authz on
// user enumeration). Preserves the legacy 404-when-empty behaviour.
func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	caller, _ := userFromCtx(r.Context())
	if !caller.IsAdminUser {
		httpx.Error(w, http.StatusForbidden, "Forbidden.")
		return
	}
	users, err := h.svc.ListUsers(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(users) == 0 {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	httpx.JSON(w, http.StatusOK, users)
}

// GetUser handles GET /user/{idOrName}: numeric → by id, else by username.
// Available to any authenticated user.
func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	u, err := h.svc.GetUser(r.Context(), r.PathValue("idOrName"))
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, u)
}

// CreateUser handles POST /user {username,email,password(min8)}. Admin-only.
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	caller, _ := userFromCtx(r.Context())
	if !caller.IsAdminUser {
		httpx.Error(w, http.StatusForbidden, "Forbidden.")
		return
	}
	var body struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	v := map[string]any{}
	if body.Username == "" {
		v["username"] = "username is required"
	}
	if body.Email == "" {
		v["email"] = "email is required"
	}
	if len(body.Password) < 8 {
		v["password"] = "password must be at least 8 characters"
	}
	if len(v) > 0 {
		validationError(w, "The given data was invalid.", v)
		return
	}
	u, err := h.svc.CreateUser(r.Context(), body.Username, body.Email, body.Password)
	if errors.Is(err, ErrDuplicate) {
		validationError(w, "The given data was invalid.", map[string]any{"username": "username or email already in use"})
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusCreated, u)
}

// UpdateUser handles PUT /user/{id}. Admin or self; all fields optional.
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	caller, _ := userFromCtx(r.Context())
	id, ok := idOrName(r.PathValue("id"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if !caller.IsAdminUser && caller.ID != id {
		httpx.Error(w, http.StatusForbidden, "Forbidden.")
		return
	}
	var body struct {
		Username *string `json:"username"`
		Email    *string `json:"email"`
		Password *string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Password != nil && len(*body.Password) < 8 {
		validationError(w, "The given data was invalid.", map[string]any{"password": "password must be at least 8 characters"})
		return
	}
	u, err := h.svc.UpdateUser(r.Context(), id, body.Username, body.Email, body.Password)
	if errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, u)
}

// DeleteUser handles DELETE /user/{id}. Admin-only.
func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	caller, _ := userFromCtx(r.Context())
	if !caller.IsAdminUser {
		httpx.Error(w, http.StatusForbidden, "Forbidden.")
		return
	}
	id, ok := idOrName(r.PathValue("id"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := h.svc.DeleteUser(r.Context(), id); errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": true, "id": id})
}
