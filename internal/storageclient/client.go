// Package storageclient calls storage-service (the blob store). It replaces
// lib-reporangler's StorageClient. Unlike the legacy client, methods return
// explicit errors (they do not swallow failures to nil/false) and stream
// bodies rather than buffering whole blobs.
package storageclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrNotFound maps a 404 from storage-service.
var ErrNotFound = errors.New("not found")

// Client talks to storage-service.
type Client struct {
	baseURL   string // internal (server-to-server) base URL
	publicURL string // client-facing base URL for download links
	token     string
	http      *http.Client
}

// New builds a client. publicURL may be empty (defaults to baseURL).
func New(baseURL, publicURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	if publicURL == "" {
		publicURL = baseURL
	}
	return &Client{baseURL: baseURL, publicURL: publicURL, token: token, http: hc}
}

func (c *Client) req(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

// objectURL joins the base with /object/<key>, preserving slashes in the key.
func objectURL(base, key string) string {
	return strings.TrimRight(base, "/") + "/object/" + strings.TrimLeft(key, "/")
}

// Upload streams content to the given key (PUT /object/{key}).
func (c *Client) Upload(ctx context.Context, key string, content io.Reader) error {
	req, err := c.req(ctx, http.MethodPut, objectURL(c.baseURL, key), content)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("storage PUT %s: %d %s", key, resp.StatusCode, string(b))
	}
	return nil
}

// Download returns a streaming reader for the object. Caller must Close it.
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	req, err := c.req(ctx, http.MethodGet, objectURL(c.baseURL, key), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("storage GET %s: %d", key, resp.StatusCode)
	}
	return resp.Body, nil
}

// Exists reports whether an object exists (GET /object-exists/{key}).
func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	req, err := c.req(ctx, http.MethodGet, strings.TrimRight(c.baseURL, "/")+"/object-exists/"+strings.TrimLeft(key, "/"), nil)
	if err != nil {
		return false, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("storage exists %s: %d", key, resp.StatusCode)
	}
	var out struct {
		Exists bool `json:"exists"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	return out.Exists, nil
}

// Delete removes an object (DELETE /object/{key}).
func (c *Client) Delete(ctx context.Context, key string) (bool, error) {
	req, err := c.req(ctx, http.MethodDelete, objectURL(c.baseURL, key), nil)
	if err != nil {
		return false, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("storage DELETE %s: %d", key, resp.StatusCode)
	}
	var out struct {
		Deleted bool `json:"deleted"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.Deleted, nil
}

// DownloadURL returns the client-facing URL for an object.
func (c *Client) DownloadURL(key string) string { return objectURL(c.publicURL, key) }

// InternalDownloadURL returns the server-to-server URL for an object.
func (c *Client) InternalDownloadURL(key string) string { return objectURL(c.baseURL, key) }
