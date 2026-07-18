// Package metadataclient is the HTTP client for metadata-service, replacing
// lib-reporangler's MetadataClient. It forwards the caller's bearer token.
package metadataclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/reporangler/reporangler/internal/types"
)

// ErrNotFound maps a 404 from metadata-service.
var ErrNotFound = errors.New("not found")

// Client talks to metadata-service.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a client. token is forwarded as the bearer on every request.
func New(baseURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{baseURL: baseURL, token: token, http: hc}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("metadata %s %s: %d %s", method, path, resp.StatusCode, string(b))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// --- Packages ---

// GetPackages lists all packages in a repository (repoType = php/npm/pypi/go).
func (c *Client) GetPackages(ctx context.Context, repoType string) ([]types.Package, error) {
	var lr types.ListResponse[types.Package]
	err := c.do(ctx, http.MethodGet, "/package/"+repoType, nil, &lr)
	return lr.Data, err
}

// GetPackagesByName lists every version of one package by name.
func (c *Client) GetPackagesByName(ctx context.Context, repoType, name string) ([]types.Package, error) {
	var lr types.ListResponse[types.Package]
	err := c.do(ctx, http.MethodGet, "/package/"+repoType+"/by-name/"+url.PathEscape(name), nil, &lr)
	return lr.Data, err
}

// AddPackage upserts a package version.
func (c *Client) AddPackage(ctx context.Context, repoType, group, name, version string, definition map[string]any, storageKey, packageType string) (types.Package, error) {
	body := map[string]any{"name": name, "version": version, "package_group": group, "definition": definition}
	if storageKey != "" {
		body["storage_key"] = storageKey
	}
	if packageType != "" {
		body["package_type"] = packageType
	}
	var p types.Package
	err := c.do(ctx, http.MethodPost, "/package/"+repoType, body, &p)
	return p, err
}

// DeletePackage removes a package by id within a repository.
func (c *Client) DeletePackage(ctx context.Context, repoType string, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/package/%s/%d", repoType, id), nil, nil)
}

// --- Package groups ---

func (c *Client) GetPackageGroups(ctx context.Context) ([]types.PackageGroup, error) {
	var lr types.ListResponse[types.PackageGroup]
	err := c.do(ctx, http.MethodGet, "/package-group/", nil, &lr)
	return lr.Data, err
}

func (c *Client) GetPackageGroupByName(ctx context.Context, name string) (types.PackageGroup, error) {
	var g types.PackageGroup
	err := c.do(ctx, http.MethodGet, "/package-group/"+name, nil, &g)
	return g, err
}

func (c *Client) GetPackageGroupByID(ctx context.Context, id int64) (types.PackageGroup, error) {
	var g types.PackageGroup
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/package-group/%d", id), nil, &g)
	return g, err
}

func (c *Client) CreatePackageGroup(ctx context.Context, name string) (types.PackageGroup, error) {
	var g types.PackageGroup
	err := c.do(ctx, http.MethodPost, "/package-group/", map[string]any{"name": name}, &g)
	return g, err
}

func (c *Client) DeletePackageGroup(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/package-group/%d", id), nil, nil)
}

// --- Repositories ---

func (c *Client) GetRepositories(ctx context.Context) ([]types.Repository, error) {
	var lr types.ListResponse[types.Repository]
	err := c.do(ctx, http.MethodGet, "/repository/", nil, &lr)
	return lr.Data, err
}

func (c *Client) GetRepositoryByName(ctx context.Context, name string) (types.Repository, error) {
	var r types.Repository
	err := c.do(ctx, http.MethodGet, "/repository/"+name, nil, &r)
	return r, err
}

func (c *Client) CreateRepository(ctx context.Context, name string) (types.Repository, error) {
	var r types.Repository
	err := c.do(ctx, http.MethodPost, "/repository/", map[string]any{"name": name}, &r)
	return r, err
}

func (c *Client) DeleteRepository(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/repository/%d", id), nil, nil)
}
