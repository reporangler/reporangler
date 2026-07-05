// Package metadataclient calls metadata-service (the package index). It
// replaces lib-reporangler's MetadataClient. Method set is stubbed for the
// initial scaffold; the wire contract is documented in docs/legacy/README.md.
package metadataclient

import "net/http"

// Client talks to metadata-service, forwarding the caller's bearer token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a metadata-service client.
func New(baseURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{baseURL: baseURL, token: token, http: hc}
}

// TODO(port): GetPackages, GetPackagesByName, AddPackage,
// package-group + repository CRUD — see docs/legacy/README.md
// (§ metadata-service contract).
