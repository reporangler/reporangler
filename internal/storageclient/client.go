// Package storageclient calls storage-service (the blob store). It replaces
// lib-reporangler's StorageClient. Method set is stubbed for the initial
// scaffold; the wire contract is documented in docs/legacy/README.md.
//
// Note: unlike the legacy StorageClient, ports of these methods must return
// explicit errors rather than swallowing them to nil/false.
package storageclient

import "net/http"

// Client talks to storage-service, forwarding the caller's bearer token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New builds a storage-service client.
func New(baseURL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{baseURL: baseURL, token: token, http: hc}
}

// TODO(port): Upload(key, r io.Reader), Download(key) (io.ReadCloser, error),
// Exists(key), Delete(key) — see docs/legacy/README.md (§ storage-service
// contract). Stream bodies; do not buffer whole blobs.
