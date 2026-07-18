package npm

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/reporangler/reporangler/internal/authclient"
	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/metadataclient"
	"github.com/reporangler/reporangler/internal/middleware"
	"github.com/reporangler/reporangler/internal/storageclient"
	"github.com/reporangler/reporangler/internal/types"
)

// Config holds the dependencies and settings a Handler needs.
type Config struct {
	Auth             *authclient.Client
	MetadataBaseURL  string
	StorageBaseURL   string
	StoragePublicURL string
	HTTPClient       *http.Client
	Protocol         string // APP_PROTOCOL, used to build dist.tarball URLs
}

// Handler serves the npm-registry protocol.
type Handler struct {
	cfg Config
}

// NewHandler builds a Handler.
func NewHandler(cfg Config) *Handler {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	return &Handler{cfg: cfg}
}

// Routes registers every npm route on a fresh mux and returns it. The caller
// wraps the result in middleware.CORS. Auth guards are applied per route:
// reads are auth:repo (anonymous falls back to the public user); publish is
// auth:token (mandatory bearer).
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	repo := middleware.Repo(h.cfg.Auth)
	token := middleware.Token(h.cfg.Auth)

	// Healthz on the exact root only ("/"), so it does not shadow the
	// {package...} catch-all below.
	mux.HandleFunc("GET /{$}", httpx.Healthz("npm-service"))

	mux.Handle("GET /-/all", repo(http.HandlerFunc(h.All)))
	mux.HandleFunc("POST /-/npm/v1/security/audits", h.Audit)

	// A multi-segment wildcard must be the final segment, so tarball requests
	// (".../-/<file>.tgz") cannot have their own pattern; Get dispatches them.
	mux.Handle("GET /{package...}", repo(http.HandlerFunc(h.Get)))
	mux.Handle("PUT /{package...}", token(http.HandlerFunc(h.Publish)))

	return mux
}

// clients builds per-request metadata and storage clients that forward the
// authenticated user's token, per the parity rules.
func (h *Handler) clients(u types.User) (*metadataclient.Client, *storageclient.Client) {
	m := metadataclient.New(h.cfg.MetadataBaseURL, u.Token, h.cfg.HTTPClient)
	s := storageclient.New(h.cfg.StorageBaseURL, h.cfg.StoragePublicURL, u.Token, h.cfg.HTTPClient)
	return m, s
}

// baseURL derives this facade's public base URL from the incoming request, so
// dist.tarball links point back at the host the client actually used.
func (h *Handler) baseURL(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = h.cfg.Protocol
	}
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + r.Host
}

// All implements GET /-/all: a deduplicated {name,description} index of every
// package in the npm repository.
func (h *Handler) All(w http.ResponseWriter, r *http.Request) {
	u, _ := middleware.UserFrom(r.Context())
	md, _ := h.clients(u)

	pkgs, err := md.GetPackages(r.Context(), repoType)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "failed to list packages")
		return
	}

	type nameDesc struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	seen := map[string]bool{}
	out := []nameDesc{}
	for _, p := range pkgs {
		if seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		desc, _ := p.Definition["description"].(string)
		out = append(out, nameDesc{Name: p.Name, Description: desc})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// Get implements GET /{package...}. Because a catch-all wildcard must be last,
// this one route serves both the metadata document and tarball downloads: a
// path of the form "<name>/-/<file>.tgz" is a tarball request, anything else
// is a metadata lookup.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	pkg := r.PathValue("package")
	if i := strings.LastIndex(pkg, "/-/"); i >= 0 {
		filename := pkg[i+len("/-/"):]
		if strings.HasSuffix(filename, ".tgz") {
			h.tarball(w, r, pkg[:i], filename)
			return
		}
	}
	h.metadata(w, r, pkg)
}

// metadata builds the npm packument: {name,"dist-tags":{latest},versions}. Each
// version's definition gets a dist.tarball whose filename equals the storage
// key basename (the parity fix), so the tarball route can find the blob.
func (h *Handler) metadata(w http.ResponseWriter, r *http.Request, name string) {
	u, _ := middleware.UserFrom(r.Context())
	md, _ := h.clients(u)

	pkgs, err := md.GetPackagesByName(r.Context(), repoType, name)
	if err != nil && !errors.Is(err, metadataclient.ErrNotFound) {
		httpx.Error(w, http.StatusBadGateway, "failed to fetch package")
		return
	}
	if len(pkgs) == 0 {
		httpx.Error(w, http.StatusNotFound, "package not found")
		return
	}

	base := h.baseURL(r)
	versions := map[string]any{}
	versionList := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		def := p.Definition
		if def == nil {
			def = map[string]any{}
		}
		// Ensure dist.tarball advertises a filename == basename(storage_key),
		// rebuilding it from the sanitized key so scoped packages resolve.
		if p.StorageKey != "" {
			dist, _ := def["dist"].(map[string]any)
			if dist == nil {
				dist = map[string]any{}
			}
			dist["tarball"] = tarballURL(base, name, p.StorageKey)
			def["dist"] = dist
		}
		versions[p.Version] = def
		versionList = append(versionList, p.Version)
	}

	doc := map[string]any{
		"name":      name,
		"dist-tags": map[string]any{"latest": latestVersion(versionList)},
		"versions":  versions,
	}
	httpx.JSON(w, http.StatusOK, doc)
}

// tarball streams the blob for the version whose stored key basename matches the
// requested filename. It never buffers the whole file (io.Copy from storage).
func (h *Handler) tarball(w http.ResponseWriter, r *http.Request, name, filename string) {
	u, _ := middleware.UserFrom(r.Context())
	md, st := h.clients(u)

	pkgs, err := md.GetPackagesByName(r.Context(), repoType, name)
	if err != nil && !errors.Is(err, metadataclient.ErrNotFound) {
		httpx.Error(w, http.StatusBadGateway, "failed to fetch package")
		return
	}
	key := ""
	for _, p := range pkgs {
		if p.StorageKey != "" && path.Base(p.StorageKey) == filename {
			key = p.StorageKey
			break
		}
	}
	if key == "" {
		httpx.Error(w, http.StatusNotFound, "tarball not found")
		return
	}

	body, err := st.Download(r.Context(), key)
	if err != nil {
		if errors.Is(err, storageclient.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "tarball not found")
			return
		}
		httpx.Error(w, http.StatusBadGateway, "failed to download tarball")
		return
	}
	defer body.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

// publishRequest is the npm publish body: package doc plus base64 tarballs.
type publishRequest struct {
	Name        string                    `json:"name"`
	Versions    map[string]map[string]any `json:"versions"`
	Attachments map[string]struct {
		ContentType string `json:"content_type"`
		Data        string `json:"data"`
		Length      int    `json:"length"`
	} `json:"_attachments"`
}

// Publish implements PUT /{package...}: the npm publish flow. It rejects the
// public user, decodes each version's attachment, computes dist hashes, uploads
// the blob, injects dist into the version doc, and records it in metadata.
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	u, _ := middleware.UserFrom(r.Context())
	if u.IsPublicUser || u.Token == types.PublicToken {
		httpx.Error(w, http.StatusForbidden, "publishing requires an authenticated user")
		return
	}
	md, st := h.clients(u)

	var req publishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid publish body")
		return
	}
	name := req.Name
	if name == "" {
		name = r.PathValue("package")
	}
	if name == "" || len(req.Versions) == 0 {
		httpx.Error(w, http.StatusUnprocessableEntity, "missing package name or versions")
		return
	}

	group := packageGroup(name, r.Header.Get("x-package-group"))
	base := h.baseURL(r)

	for version, def := range req.Versions {
		att, ok := req.Attachments[attachmentName(name, version)]
		if !ok {
			httpx.Error(w, http.StatusUnprocessableEntity, "missing attachment for "+name+"@"+version)
			return
		}
		raw, err := base64.StdEncoding.DecodeString(att.Data)
		if err != nil {
			httpx.Error(w, http.StatusUnprocessableEntity, "invalid base64 attachment for "+name+"@"+version)
			return
		}

		shasum, subresource := integrity(raw)
		key := storageKey(group, name, version)

		if err := st.Upload(r.Context(), key, bytes.NewReader(raw)); err != nil {
			httpx.Error(w, http.StatusBadGateway, "failed to store tarball")
			return
		}

		if def == nil {
			def = map[string]any{}
		}
		dist, _ := def["dist"].(map[string]any)
		if dist == nil {
			dist = map[string]any{}
		}
		dist["shasum"] = shasum
		dist["integrity"] = subresource
		dist["tarball"] = tarballURL(base, name, key)
		def["dist"] = dist

		if _, err := md.AddPackage(r.Context(), repoType, group, name, version, def, key, "tgz"); err != nil {
			httpx.Error(w, http.StatusBadGateway, "failed to record package metadata")
			return
		}
	}

	httpx.JSON(w, http.StatusCreated, map[string]any{"ok": true, "id": name})
}

// Audit implements POST /-/npm/v1/security/audits as a no-op stub (the legacy
// service returned an empty advisory set).
func (h *Handler) Audit(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{
		"actions":     []any{},
		"advisories":  []any{},
		"moreInfoUrl": "",
	})
}
