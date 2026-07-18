package storage

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/middleware"
	"github.com/reporangler/reporangler/internal/types"
)

// Handler serves the storage-service HTTP API over a Store.
type Handler struct {
	store *Store
}

// NewHandler builds a Handler backed by store.
func NewHandler(store *Store) *Handler { return &Handler{store: store} }

// objectEntry is one row of the GET /objects listing.
type objectEntry struct {
	Key      string `json:"key"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

// Get streams the raw bytes of an object (GET /object/{key...}, auth:repo).
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	full, err := h.store.Resolve(key)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_key"})
		return
	}

	f, err := os.Open(full)
	if err != nil {
		httpx.JSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		httpx.JSON(w, http.StatusNotFound, map[string]any{"error": "not_found"})
		return
	}

	name := filepath.Base(full)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	// ServeContent streams the file (io.Copy internally), sets Content-Length,
	// and honours Range requests without buffering the whole blob in memory.
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

// Exists reports whether an object exists (GET /object-exists/{key...}).
func (h *Handler) Exists(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	full, err := h.store.Resolve(key)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_key"})
		return
	}
	exists := false
	if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
		exists = true
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"exists": exists, "key": key})
}

// List returns metadata for every object under an optional key prefix
// (GET /objects?prefix=), walking recursively.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")

	// Choose the tightest starting directory for the walk while still filtering
	// by the raw prefix (which may end mid-segment).
	start := h.store.root
	if prefix != "" {
		cand := filepath.Join(h.store.root, filepath.FromSlash(prefix))
		if !withinRoot(h.store.root, cand) {
			httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_key"})
			return
		}
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			start = cand
		} else if parent := filepath.Dir(cand); withinRoot(h.store.root, parent) {
			start = parent
		}
	}

	data := []objectEntry{}
	filepath.WalkDir(start, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(h.store.root, p)
		if err != nil {
			return nil
		}
		key := filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		data = append(data, objectEntry{
			Key:      key,
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	})

	httpx.JSON(w, http.StatusOK, types.ListResponse[objectEntry]{Count: len(data), Data: data})
}

// Put streams a request body to disk (PUT /object/{key...}, auth:token). The
// public user is rejected; empty bodies are refused with 400.
func (h *Handler) Put(w http.ResponseWriter, r *http.Request) {
	if user, ok := middleware.UserFrom(r.Context()); ok && user.IsPublicUser {
		httpx.JSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
		return
	}

	key := r.PathValue("key")
	full, err := h.store.Resolve(key)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_key"})
		return
	}

	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		httpx.JSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}
	// Re-check containment now that parent dirs exist (guards symlink escape via
	// a component created concurrently).
	if _, err := h.store.Resolve(key); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_key"})
		return
	}

	// Stream to a temp file in the same dir, then atomically rename into place.
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}
	tmpName := tmp.Name()

	n, copyErr := io.Copy(tmp, r.Body)
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(tmpName)
		httpx.JSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}
	if n == 0 {
		os.Remove(tmpName)
		httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": "empty_body"})
		return
	}
	if err := os.Rename(tmpName, full); err != nil {
		os.Remove(tmpName)
		httpx.JSON(w, http.StatusInternalServerError, map[string]any{"error": "write_failed"})
		return
	}

	httpx.JSON(w, http.StatusCreated, map[string]any{"ok": true, "key": key, "size": n})
}

// Delete removes an object (DELETE /object/{key...}, auth:token).
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	full, err := h.store.Resolve(key)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_key"})
		return
	}

	deleted := false
	if fi, statErr := os.Stat(full); statErr == nil && !fi.IsDir() {
		if err := os.Remove(full); err != nil {
			httpx.JSON(w, http.StatusInternalServerError, map[string]any{"error": "delete_failed"})
			return
		}
		deleted = true
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		httpx.JSON(w, http.StatusInternalServerError, map[string]any{"error": "delete_failed"})
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]any{"deleted": deleted, "key": key})
}
