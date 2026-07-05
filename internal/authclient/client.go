// Package authclient calls auth-service to introspect bearer tokens. It
// replaces lib-reporangler's AuthClient::check().
package authclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/reporangler/reporangler/internal/types"
)

// ErrUnauthorized indicates the token was rejected by auth-service.
var ErrUnauthorized = errors.New("unauthorized")

// Client talks to auth-service.
type Client struct {
	baseURL string
	http    *http.Client
}

// New builds a client for the given auth-service base URL.
func New(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{baseURL: baseURL, http: hc}
}

// Check validates an Authorization header value against GET /login/token and
// returns the resolved user.
func (c *Client) Check(ctx context.Context, authHeader string) (types.User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/login/token", nil)
	if err != nil {
		return types.User{}, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return types.User{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return types.User{}, ErrUnauthorized
	}
	var u types.User
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return types.User{}, err
	}
	return u, nil
}
