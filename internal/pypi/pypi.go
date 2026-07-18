// Package pypi implements the PyPI "simple" repository protocol (PEP 503) plus
// blob download and Twine-style upload, presented over metadata-service and
// storage-service. It is a stateless facade: metadata-service is the system of
// record and storage-service holds the distribution files.
//
// Storage key layout (the cross-service contract, kept byte-identical to the
// legacy PHP service): pypi/{group}/{normName}/{version}/{filename}.
//
// Fix folded in from the port: download links are built from the storage
// service's public URL (storageclient.DownloadURL) instead of a hardcoded
// pypi.<domain> host as the PHP did.
package pypi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/reporangler/reporangler/internal/authclient"
	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/metadataclient"
	"github.com/reporangler/reporangler/internal/middleware"
	"github.com/reporangler/reporangler/internal/storageclient"
	"github.com/reporangler/reporangler/internal/types"
)

// RepoType is this facade's ecosystem, used as the metadata-service repository.
const RepoType = "pypi"

// Config wires the server to its dependencies. Base URLs and the auth client
// are process-wide; per-request metadata/storage clients are built from them so
// the authenticated caller's token can be forwarded.
type Config struct {
	Protocol         string // APP_PROTOCOL, for CORS
	Domain           string // APP_DOMAIN, for CORS
	MetadataBaseURL  string
	StorageBaseURL   string
	StoragePublicURL string // client-facing base for download links
	Auth             *authclient.Client
	HTTP             *http.Client
}

// Server holds the facade configuration.
type Server struct {
	cfg Config
}

// New builds a Server.
func New(cfg Config) *Server {
	if cfg.HTTP == nil {
		cfg.HTTP = http.DefaultClient
	}
	return &Server{cfg: cfg}
}

// Handler builds the routed, CORS-wrapped http.Handler for the service.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", httpx.Healthz("pypi-service"))

	repo := middleware.Repo(s.cfg.Auth)   // auth:repo — optional, public fallback
	token := middleware.Token(s.cfg.Auth) // auth:token — mandatory bearer

	mux.Handle("GET /simple/", repo(http.HandlerFunc(s.handleIndex)))
	mux.Handle("GET /simple/{package}/", repo(http.HandlerFunc(s.handleFileList)))
	mux.Handle("GET /packages/{group}/{name}/{version}/{filename}", repo(http.HandlerFunc(s.handleDownload)))
	mux.Handle("POST /upload", token(http.HandlerFunc(s.handleUpload)))

	// CORS wraps the whole mux and also answers OPTIONS preflight (a legacy bug fix).
	return middleware.CORS(s.cfg.Protocol, s.cfg.Domain)(mux)
}

// --- per-request clients (forward the caller's token) ---

func (s *Server) meta(ctx context.Context) *metadataclient.Client {
	u, _ := middleware.UserFrom(ctx)
	return metadataclient.New(s.cfg.MetadataBaseURL, u.Token, s.cfg.HTTP)
}

func (s *Server) storage(ctx context.Context) *storageclient.Client {
	u, _ := middleware.UserFrom(ctx)
	return storageclient.New(s.cfg.StorageBaseURL, s.cfg.StoragePublicURL, u.Token, s.cfg.HTTP)
}

// --- pure helpers (unit-tested) ---

var nameSep = regexp.MustCompile(`[-_.]+`)

// normalizeName applies PEP 503 name normalization: lower-case and collapse any
// run of runs of -, _ or . into a single -.
func normalizeName(name string) string {
	return strings.ToLower(nameSep.ReplaceAllString(name, "-"))
}

// storageKey builds the cross-service storage key for a distribution file.
// The name segment must already be normalized (see normalizeName).
func storageKey(group, normName, version, filename string) string {
	return fmt.Sprintf("pypi/%s/%s/%s/%s", group, normName, version, filename)
}

// contentTypeFor maps a distribution filename to its Content-Type.
func contentTypeFor(filename string) string {
	f := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(f, ".tar.gz"), strings.HasSuffix(f, ".tgz"):
		return "application/gzip"
	case strings.HasSuffix(f, ".whl"), strings.HasSuffix(f, ".zip"):
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}

// --- handlers ---

// handleIndex renders the PEP 503 root index: one anchor per unique normalized
// package name, sorted.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	pkgs, err := s.meta(r.Context()).GetPackages(r.Context(), RepoType)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "metadata unavailable")
		return
	}

	seen := map[string]struct{}{}
	names := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		n := normalizeName(p.Name)
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n  <head>\n    <title>Simple index</title>\n    <meta name=\"pypi:repository-version\" content=\"1.0\">\n  </head>\n  <body>\n")
	for _, n := range names {
		esc := html.EscapeString(n)
		fmt.Fprintf(&b, "    <a href=\"/simple/%s/\">%s</a>\n", esc, esc)
	}
	b.WriteString("  </body>\n</html>\n")

	writeHTML(w, b.String())
}

// handleFileList renders the PEP 503 per-project page: one anchor per stored
// distribution file, with a #sha256 fragment and optional data-requires-python.
func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) {
	name := normalizeName(r.PathValue("package"))
	ctx := r.Context()

	pkgs, err := s.meta(ctx).GetPackagesByName(ctx, RepoType, name)
	if err != nil {
		if errors.Is(err, metadataclient.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "package not found")
			return
		}
		httpx.Error(w, http.StatusBadGateway, "metadata unavailable")
		return
	}
	if len(pkgs) == 0 {
		httpx.Error(w, http.StatusNotFound, "package not found")
		return
	}

	store := s.storage(ctx)

	var b strings.Builder
	fmt.Fprintf(&b, "<!DOCTYPE html>\n<html>\n  <head>\n    <title>Links for %s</title>\n    <meta name=\"pypi:repository-version\" content=\"1.0\">\n  </head>\n  <body>\n    <h1>Links for %s</h1>\n", html.EscapeString(name), html.EscapeString(name))
	for _, p := range pkgs {
		key := p.StorageKey
		if key == "" {
			continue
		}
		filename := path.Base(key)
		if fn := defString(p.Definition, "filename"); fn != "" {
			filename = fn
		}

		href := store.DownloadURL(key)
		if sha := defString(p.Definition, "sha256_digest"); sha != "" {
			href += "#sha256=" + sha
		}

		var attr string
		if rp := defString(p.Definition, "requires_python"); rp != "" {
			attr = fmt.Sprintf(" data-requires-python=\"%s\"", html.EscapeString(rp))
		}

		fmt.Fprintf(&b, "    <a href=\"%s\"%s>%s</a><br/>\n",
			html.EscapeString(href), attr, html.EscapeString(filename))
	}
	b.WriteString("  </body>\n</html>\n")

	writeHTML(w, b.String())
}

// handleDownload streams a distribution file from storage. Parity endpoint —
// generated index links point at storage directly, but clients may still fetch
// through here.
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	key := storageKey(
		r.PathValue("group"),
		r.PathValue("name"),
		r.PathValue("version"),
		r.PathValue("filename"),
	)
	ctx := r.Context()

	rc, err := s.storage(ctx).Download(ctx, key)
	if err != nil {
		if errors.Is(err, storageclient.ErrNotFound) {
			httpx.Error(w, http.StatusNotFound, "file not found")
			return
		}
		httpx.Error(w, http.StatusBadGateway, "storage unavailable")
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", contentTypeFor(r.PathValue("filename")))
	w.Header().Set("Content-Disposition", "attachment; filename=\""+r.PathValue("filename")+"\"")
	_, _ = io.Copy(w, rc)
}

// handleUpload ingests a Twine-style multipart upload: it stores the file in
// storage and registers the version in metadata.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	u, _ := middleware.UserFrom(r.Context())
	if u.IsPublicUser || u.Token == types.PublicToken {
		httpx.Error(w, http.StatusForbidden, "public user may not upload")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	name := r.FormValue("name")
	version := r.FormValue("version")

	file, header, err := r.FormFile("content")
	if name == "" || version == "" || err != nil {
		httpx.Error(w, http.StatusBadRequest, "name, version and content are required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "could not read uploaded content")
		return
	}
	if len(data) == 0 {
		httpx.Error(w, http.StatusBadRequest, "name, version and content are required")
		return
	}

	sha := r.FormValue("sha256_digest")
	if sha == "" {
		sum := sha256.Sum256(data)
		sha = hex.EncodeToString(sum[:])
	}

	group := r.FormValue("package_group")
	if group == "" {
		group = types.PublicGroup
	}
	normName := normalizeName(name)
	filename := header.Filename
	filetype := r.FormValue("filetype")
	key := storageKey(group, normName, version, filename)

	ctx := r.Context()
	if err := s.storage(ctx).Upload(ctx, key, bytes.NewReader(data)); err != nil {
		httpx.Error(w, http.StatusBadGateway, "storage upload failed")
		return
	}

	def := map[string]any{
		"filename":        filename,
		"filetype":        filetype,
		"summary":         r.FormValue("summary"),
		"author":          r.FormValue("author"),
		"license":         r.FormValue("license"),
		"requires_python": r.FormValue("requires_python"),
		"md5_digest":      r.FormValue("md5_digest"),
		"sha256_digest":   sha,
	}
	if rd := r.PostForm["requires_dist"]; len(rd) > 0 {
		def["requires_dist"] = rd
	}

	pkg, err := s.meta(ctx).AddPackage(ctx, RepoType, group, normName, version, def, key, filetype)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "metadata registration failed")
		return
	}

	httpx.JSON(w, http.StatusCreated, pkg)
}

// --- misc ---

func writeHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, body)
}

func defString(def map[string]any, key string) string {
	if def == nil {
		return ""
	}
	s, _ := def[key].(string)
	return s
}
