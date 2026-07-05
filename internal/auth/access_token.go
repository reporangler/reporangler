package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/reporangler/reporangler/internal/httpx"
)

// AccessToken is an external service credential (e.g. a GitHub token) a user
// stores so the facades can act on their behalf. The token value is returned by
// the API because facades must retrieve it to authenticate upstream — so it
// cannot be hashed the way login credentials are.
type AccessToken struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"user_id"`
	Type   string `json:"type"`
	Token  string `json:"token"`
}

// validAccessTokenTypes enumerates the accepted access-token types.
var validAccessTokenTypes = map[string]bool{"github": true}

// --- store ---

func (s *Store) listAccessTokens(ctx context.Context, userID int64) ([]AccessToken, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, type, token FROM access_tokens WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccessToken
	for rows.Next() {
		var t AccessToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.Type, &t.Token); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// upsertAccessToken inserts or replaces the token for (user_id, type), honouring
// the UNIQUE(user_id, type) constraint (one token per type per user).
func (s *Store) upsertAccessToken(ctx context.Context, userID int64, typ, token string) (AccessToken, error) {
	var t AccessToken
	err := s.pool.QueryRow(ctx, `
		INSERT INTO access_tokens (user_id, type, token) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, type) DO UPDATE SET token = EXCLUDED.token, updated_at = now()
		RETURNING id, user_id, type, token`, userID, typ, token).
		Scan(&t.ID, &t.UserID, &t.Type, &t.Token)
	return t, err
}

func (s *Store) deleteAccessToken(ctx context.Context, userID, tokenID int64) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM access_tokens WHERE id = $1 AND user_id = $2`, tokenID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- handlers ---

// ListAccessTokens handles GET /access-token/{userId}. Admin or self.
func (h *Handler) ListAccessTokens(w http.ResponseWriter, r *http.Request) {
	caller, _ := userFromCtx(r.Context())
	userID, ok := idOrName(r.PathValue("userId"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if !caller.IsAdminUser && caller.ID != userID {
		httpx.Error(w, http.StatusForbidden, "Forbidden.")
		return
	}
	tokens, err := h.svc.store.listAccessTokens(r.Context(), userID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, tokens)
}

// CreateAccessToken handles POST /access-token/{userId} {type,token}. Admin or
// self; type must be one of the supported types (github).
func (h *Handler) CreateAccessToken(w http.ResponseWriter, r *http.Request) {
	caller, _ := userFromCtx(r.Context())
	userID, ok := idOrName(r.PathValue("userId"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if !caller.IsAdminUser && caller.ID != userID {
		httpx.Error(w, http.StatusForbidden, "Forbidden.")
		return
	}
	var body struct {
		Type  string `json:"type"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}
	v := map[string]any{}
	if !validAccessTokenTypes[body.Type] {
		v["type"] = "type must be one of: github"
	}
	if body.Token == "" {
		v["token"] = "token is required"
	}
	if len(v) > 0 {
		validationError(w, "The given data was invalid.", v)
		return
	}
	t, err := h.svc.store.upsertAccessToken(r.Context(), userID, body.Type, body.Token)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusCreated, t)
}

// DeleteAccessToken handles DELETE /access-token/{userId}/{tokenId}. Admin/self.
func (h *Handler) DeleteAccessToken(w http.ResponseWriter, r *http.Request) {
	caller, _ := userFromCtx(r.Context())
	userID, ok := idOrName(r.PathValue("userId"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid user id")
		return
	}
	tokenID, ok := idOrName(r.PathValue("tokenId"))
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid token id")
		return
	}
	if !caller.IsAdminUser && caller.ID != userID {
		httpx.Error(w, http.StatusForbidden, "Forbidden.")
		return
	}
	if err := h.svc.store.deleteAccessToken(r.Context(), userID, tokenID); errors.Is(err, ErrNotFound) {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": true, "id": tokenID})
}
