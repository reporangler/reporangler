// Package goproxy implements the RepoRangler Go module proxy facade. It serves
// the GOPROXY download protocol (@v/list, {v}.info, {v}.mod, {v}.zip, @latest)
// and an authenticated /upload endpoint, delegating package metadata to
// metadata-service and blobs to storage-service.
//
// It ports the legacy Laravel "goproxy" service and folds in the fixes noted
// in docs/legacy/README.md: module-path case-encoding, real semver via
// golang.org/x/mod/semver for @latest/list (no naive version_compare), CORS
// preflight via middleware.CORS, and streamed zip downloads (no buffering).
package goproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/reporangler/reporangler/internal/authclient"
	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/metadataclient"
	"github.com/reporangler/reporangler/internal/middleware"
	"github.com/reporangler/reporangler/internal/storageclient"
	"github.com/reporangler/reporangler/internal/types"
)

// metaAPI is the subset of metadataclient used by the facade (kept small so it
// can be faked in tests).
type metaAPI interface {
	GetPackagesByName(ctx context.Context, repoType, name string) ([]types.Package, error)
	AddPackage(ctx context.Context, repoType, group, name, version string, definition map[string]any, storageKey, packageType string) (types.Package, error)
}

// storeAPI is the subset of storageclient used by the facade.
type storeAPI interface {
	Upload(ctx context.Context, key string, content io.Reader) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
}

// Config holds the facade's runtime configuration.
type Config struct {
	RepoType      string // metadata repository / ecosystem, "go"
	MetadataBase  string
	StorageBase   string
	StoragePublic string
	HTTP          *http.Client // shared client for backend calls (nil => default)
}

// Server serves the GOPROXY protocol.
type Server struct {
	cfg  Config
	auth *authclient.Client
	// newClients builds per-request metadata+storage clients carrying the
	// authenticated user's token. Overridable in tests.
	newClients func(token string) (metaAPI, storeAPI)
}

// New builds a Server. Per request it constructs metadata/storage clients that
// forward the caller's bearer token.
func New(cfg Config, ac *authclient.Client) *Server {
	if cfg.RepoType == "" {
		cfg.RepoType = "go"
	}
	s := &Server{cfg: cfg, auth: ac}
	s.newClients = func(token string) (metaAPI, storeAPI) {
		return metadataclient.New(cfg.MetadataBase, token, cfg.HTTP),
			storageclient.New(cfg.StorageBase, cfg.StoragePublic, token, cfg.HTTP)
	}
	return s
}

// Handler wires the routes and wraps the mux with CORS (which also answers
// OPTIONS preflight). Reads use the optional auth:repo guard; /upload uses the
// mandatory auth:token guard.
func (s *Server) Handler(protocol, domain string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", httpx.Healthz("go-service"))

	repo := middleware.Repo(s.auth)
	token := middleware.Token(s.auth)

	// Catch-all read route: module paths contain '/', so {module...} captures
	// the module path plus the trailing @v/... or @latest, parsed by hand
	// (ServeMux forbids fixed segments after a "..." wildcard).
	mux.Handle("GET /{module...}", repo(http.HandlerFunc(s.handleRead)))
	mux.Handle("POST /upload", token(http.HandlerFunc(s.handleUpload)))

	return middleware.CORS(protocol, domain)(mux)
}

// clients builds per-request metadata/storage clients forwarding the caller's
// token (from the auth middleware).
func (s *Server) clients(ctx context.Context) (metaAPI, storeAPI) {
	token := ""
	if u, ok := middleware.UserFrom(ctx); ok {
		token = u.Token
	}
	return s.newClients(token)
}

// handleRead dispatches the GOPROXY download endpoints off the catch-all path.
func (s *Server) handleRead(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("module")

	if enc, ok := strings.CutSuffix(raw, "/@latest"); ok {
		s.handleLatest(w, r, enc)
		return
	}

	enc, rest, ok := strings.Cut(raw, "/@v/")
	if !ok {
		s.notFound(w)
		return
	}
	switch {
	case rest == "list":
		s.handleList(w, r, enc)
	case strings.HasSuffix(rest, ".info"):
		s.handleInfo(w, r, enc, strings.TrimSuffix(rest, ".info"))
	case strings.HasSuffix(rest, ".mod"):
		s.handleMod(w, r, enc, strings.TrimSuffix(rest, ".mod"))
	case strings.HasSuffix(rest, ".zip"):
		s.handleZip(w, r, enc, strings.TrimSuffix(rest, ".zip"))
	default:
		s.notFound(w)
	}
}

// packages fetches every stored version of the encoded module.
func (s *Server) packages(ctx context.Context, enc string) (string, []types.Package, error) {
	module, err := decodeModulePath(enc)
	if err != nil {
		return "", nil, err
	}
	md, _ := s.clients(ctx)
	pkgs, err := md.GetPackagesByName(ctx, s.cfg.RepoType, module)
	if err != nil {
		if errors.Is(err, metadataclient.ErrNotFound) {
			return module, nil, nil
		}
		return module, nil, err
	}
	return module, pkgs, nil
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, enc string) {
	_, pkgs, err := s.packages(r.Context(), enc)
	if err != nil {
		s.serverError(w, err)
		return
	}
	versions := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		versions = append(versions, p.Version)
	}
	sortVersions(versions)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, v := range versions {
		fmt.Fprintln(w, v)
	}
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request, enc, version string) {
	_, pkgs, err := s.packages(r.Context(), enc)
	if err != nil {
		s.serverError(w, err)
		return
	}
	for _, p := range pkgs {
		if p.Version == version {
			httpx.JSON(w, http.StatusOK, versionInfo(p))
			return
		}
	}
	s.notFound(w)
}

func (s *Server) handleMod(w http.ResponseWriter, r *http.Request, enc, version string) {
	module, pkgs, err := s.packages(r.Context(), enc)
	if err != nil {
		s.serverError(w, err)
		return
	}
	pkg, ok := findVersion(pkgs, version)
	if !ok {
		s.notFound(w)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if key := defStr(pkg.Definition, "gomod_key"); key != "" {
		_, st := s.clients(r.Context())
		if rc, err := st.Download(r.Context(), key); err == nil {
			defer rc.Close()
			_, _ = io.Copy(w, rc)
			return
		}
	}
	// Fallback: synthesize a minimal go.mod so the toolchain can proceed.
	fmt.Fprintf(w, "module %s\n", module)
}

func (s *Server) handleZip(w http.ResponseWriter, r *http.Request, enc, version string) {
	_, pkgs, err := s.packages(r.Context(), enc)
	if err != nil {
		s.serverError(w, err)
		return
	}
	pkg, ok := findVersion(pkgs, version)
	if !ok {
		s.notFound(w)
		return
	}
	key := defStr(pkg.Definition, "zip_key")
	if key == "" {
		key = defStr(pkg.Definition, "storage_key")
	}
	if key == "" {
		key = pkg.StorageKey
	}
	if key == "" {
		s.notFound(w)
		return
	}
	_, st := s.clients(r.Context())
	rc, err := st.Download(r.Context(), key)
	if err != nil {
		if errors.Is(err, storageclient.ErrNotFound) {
			s.notFound(w)
			return
		}
		s.serverError(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/zip")
	// Streamed: copy straight from storage to the client (no full-file buffer).
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleLatest(w http.ResponseWriter, r *http.Request, enc string) {
	_, pkgs, err := s.packages(r.Context(), enc)
	if err != nil {
		s.serverError(w, err)
		return
	}
	if len(pkgs) == 0 {
		s.notFound(w)
		return
	}
	versions := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		versions = append(versions, p.Version)
	}
	latest := latestVersion(versions)
	if latest == "" {
		// No valid semver at all: fall back to the sorted-highest string.
		sortVersions(versions)
		latest = versions[len(versions)-1]
	}
	pkg, ok := findVersion(pkgs, latest)
	if !ok {
		s.notFound(w)
		return
	}
	httpx.JSON(w, http.StatusOK, versionInfo(pkg))
}

// handleUpload ingests a module version. auth:token guards the route; the
// public user is additionally rejected here (parity with storage's publish
// guard). Multipart form: module, version (valid semver starting with v), the
// zip file, and an optional gomod file. An optional package_group selects the
// metadata group (default: public).
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFrom(r.Context())
	if !ok || user.IsPublicUser || user.Token == types.PublicToken {
		httpx.Error(w, http.StatusForbidden, "public user may not upload")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	module := strings.TrimSpace(r.FormValue("module"))
	version := strings.TrimSpace(r.FormValue("version"))
	if module == "" || version == "" {
		httpx.Error(w, http.StatusUnprocessableEntity, "module and version are required")
		return
	}
	if !strings.HasPrefix(version, "v") || !semver.IsValid(version) {
		httpx.Error(w, http.StatusUnprocessableEntity, "version must be a valid semver starting with 'v'")
		return
	}

	group := strings.TrimSpace(r.FormValue("package_group"))
	if group == "" {
		group = types.PublicGroup
	}

	zf, _, err := r.FormFile("zip")
	if err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, "zip file is required")
		return
	}
	defer zf.Close()

	encModule := encodeModulePath(module)
	base := path.Base(encModule)
	prefix := fmt.Sprintf("%s/%s/%s/%s", s.cfg.RepoType, group, encModule, version)
	zipKey := fmt.Sprintf("%s/%s-%s.zip", prefix, base, version)

	md, st := s.clients(r.Context())

	if err := st.Upload(r.Context(), zipKey, zf); err != nil {
		s.serverError(w, err)
		return
	}

	definition := map[string]any{
		"version": version,
		"time":    time.Now().UTC().Format(time.RFC3339),
		"zip_key": zipKey,
	}

	if gf, _, err := r.FormFile("gomod"); err == nil {
		defer gf.Close()
		gomodKey := prefix + "/go.mod"
		if err := st.Upload(r.Context(), gomodKey, gf); err != nil {
			s.serverError(w, err)
			return
		}
		definition["gomod_key"] = gomodKey
	}

	pkg, err := md.AddPackage(r.Context(), s.cfg.RepoType, group, module, version, definition, zipKey, "zip")
	if err != nil {
		s.serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, pkg)
}

// versionInfo builds the {"Version","Time"} document returned by .info and
// @latest. Time comes from definition.time, else now.
func versionInfo(p types.Package) map[string]string {
	t := defStr(p.Definition, "time")
	if t == "" {
		t = time.Now().UTC().Format(time.RFC3339)
	}
	return map[string]string{"Version": p.Version, "Time": t}
}

func findVersion(pkgs []types.Package, version string) (types.Package, bool) {
	for _, p := range pkgs {
		if p.Version == version {
			return p, true
		}
	}
	return types.Package{}, false
}

func defStr(def map[string]any, key string) string {
	if def == nil {
		return ""
	}
	if v, ok := def[key].(string); ok {
		return v
	}
	return ""
}

// notFound writes the GOPROXY not-found body ({"error":"not found"}).
func (s *Server) notFound(w http.ResponseWriter) {
	httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	log.Printf("goproxy: %v", err)
	httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}
