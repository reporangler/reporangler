package php

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/metadataclient"
	"github.com/reporangler/reporangler/internal/middleware"
	"github.com/reporangler/reporangler/internal/storageclient"
	"github.com/reporangler/reporangler/internal/types"
)

// maxMultipartMemory bounds in-memory buffering of multipart parts; the dist
// file itself is streamed to storage, not read into memory.
const maxMultipartMemory = 8 << 20 // 8 MiB

// Handler serves the Composer read protocol and the client-push ingest endpoint.
// It holds the downstream base URLs and builds per-request metadata/storage
// clients that forward the authenticated user's token.
type Handler struct {
	metaURL    string
	storageURL string
	storagePub string
	hc         *http.Client
}

// NewHandler builds a php facade handler. hc is the shared HTTP client used for
// all downstream calls (nil → http.DefaultClient inside the clients).
func NewHandler(metaURL, storageURL, storagePub string, hc *http.Client) *Handler {
	return &Handler{metaURL: metaURL, storageURL: storageURL, storagePub: storagePub, hc: hc}
}

// clients builds metadata + storage clients that forward u's bearer token.
func (h *Handler) clients(u types.User) (*metadataclient.Client, *storageclient.Client) {
	mc := metadataclient.New(h.metaURL, u.Token, h.hc)
	sc := storageclient.New(h.storageURL, h.storagePub, u.Token, h.hc)
	return mc, sc
}

// Root handles GET /packages.json (auth:repo). It returns the Composer v2
// entrypoint advertising the lazy /p2 metadata URL plus, when reachable, the
// full list of available package names.
func (h *Handler) Root(w http.ResponseWriter, r *http.Request) {
	u, _ := middleware.UserFrom(r.Context())
	mc, _ := h.clients(u)

	var names []string
	if pkgs, err := mc.GetPackages(r.Context(), repoType); err != nil {
		// Degrade gracefully: the lazy metadata-url still works even if we
		// cannot enumerate right now. Omit available-packages on error.
		log.Printf("php: packages.json enumerate failed: %v", err)
	} else {
		names = packageNames(pkgs)
	}
	httpx.JSON(w, http.StatusOK, buildRootDocument(names))
}

// P2 handles GET /p2/{vendor}/{package}.json (auth:repo). It returns the
// Composer v2 metadata document for every stored version of one package.
func (h *Handler) P2(w http.ResponseWriter, r *http.Request) {
	rest := r.PathValue("name") // "vendor/package.json"
	name := strings.TrimSuffix(rest, ".json")
	if name == "" || name == rest {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}

	u, _ := middleware.UserFrom(r.Context())
	mc, sc := h.clients(u)

	pkgs, err := mc.GetPackagesByName(r.Context(), repoType, name)
	if err != nil {
		if errors.Is(err, metadataclient.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "not found")
			return
		}
		log.Printf("php: p2 lookup %q failed: %v", name, err)
		httpx.Error(w, http.StatusBadGateway, "metadata lookup failed")
		return
	}
	if len(pkgs) == 0 {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	httpx.JSON(w, http.StatusOK, buildP2Document(name, pkgs, sc.DownloadURL))
}

// publishRequest is the JSON (metadata-only) publish body. storage_key is
// optional; when omitted the deterministic storagePath is used.
type publishRequest struct {
	Name         string         `json:"name"`
	Version      string         `json:"version"`
	Definition   map[string]any `json:"definition"`
	PackageGroup string         `json:"package_group"`
	StorageKey   string         `json:"storage_key"`
}

// Publish handles POST /publish (auth:token, public rejected).
//
// Composer has no native publish command, so ingest is a client-push endpoint.
// This intentionally REPLACES the legacy VCS-scan publish (the old php-service
// cloned/scanned VCS repos to build a Satis index): we do not reimplement any
// VCS scanning. A client pushes a package either as:
//   - JSON {name,version,definition,package_group?} — metadata only; the dist
//     zip is assumed already present in storage at the deterministic key, or
//   - multipart/form-data with a "dist" zip file we stream to storage and then
//     register.
//
// Both paths register via metadataclient.AddPackage with package_type "zip" and
// the deterministic storage key, so the /p2 document resolves dist.url.
func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	u, _ := middleware.UserFrom(r.Context())
	if u.IsPublicUser || u.Token == "" || u.Token == types.PublicToken {
		httpx.Error(w, http.StatusForbidden, "public user cannot publish")
		return
	}

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		h.publishMultipart(w, r, u)
		return
	}
	h.publishJSON(w, r, u)
}

// publishJSON registers a package whose dist is already in storage.
func (h *Handler) publishJSON(w http.ResponseWriter, r *http.Request, u types.User) {
	var req publishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Name == "" || req.Version == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, "name and version are required")
		return
	}
	group := resolveGroup(r.Header.Get("x-package-group"), req.Name)
	if req.PackageGroup != "" {
		group = req.PackageGroup
	}
	def := req.Definition
	if def == nil {
		def = map[string]any{"name": req.Name, "version": req.Version}
	}
	key := req.StorageKey
	if key == "" {
		key = storagePath(group, req.Name, req.Version)
	}

	mc, _ := h.clients(u)
	pkg, err := mc.AddPackage(r.Context(), repoType, group, req.Name, req.Version, def, key, "zip")
	if err != nil {
		log.Printf("php: publish register %q@%q failed: %v", req.Name, req.Version, err)
		httpx.Error(w, http.StatusBadGateway, "failed to register package")
		return
	}
	httpx.JSON(w, http.StatusCreated, pkg)
}

// publishMultipart streams an uploaded dist zip to storage, then registers it.
func (h *Handler) publishMultipart(w http.ResponseWriter, r *http.Request, u types.User) {
	if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	name := r.FormValue("name")
	version := r.FormValue("version")
	if name == "" || version == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, "name and version are required")
		return
	}
	group := resolveGroup(r.Header.Get("x-package-group"), name)
	if g := r.FormValue("package_group"); g != "" {
		group = g
	}
	def := map[string]any{"name": name, "version": version}
	if raw := r.FormValue("definition"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &def); err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid definition JSON")
			return
		}
	}

	file, _, err := r.FormFile("dist")
	if err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "missing dist file")
		return
	}
	defer file.Close()

	key := storagePath(group, name, version)
	mc, sc := h.clients(u)
	// Stream the upload straight through to storage — no whole-file buffering.
	if err := sc.Upload(r.Context(), key, file); err != nil {
		log.Printf("php: publish upload %q failed: %v", key, err)
		httpx.Error(w, http.StatusBadGateway, "failed to upload dist")
		return
	}
	pkg, err := mc.AddPackage(r.Context(), repoType, group, name, version, def, key, "zip")
	if err != nil {
		log.Printf("php: publish register %q@%q failed: %v", name, version, err)
		httpx.Error(w, http.StatusBadGateway, "failed to register package")
		return
	}
	httpx.JSON(w, http.StatusCreated, pkg)
}
