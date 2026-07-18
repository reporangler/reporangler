// Package storage implements the RepoRangler blob store: a filesystem-backed
// object store keyed by arbitrary slash-containing keys, rooted at STORAGE_PATH.
//
// It ports the legacy Laravel storage-service and folds in two fixes noted in
// docs/legacy/README.md: paths are contained within the storage root (the PHP
// had no traversal defence) and every transfer is streamed (io.Copy /
// http.ServeContent) rather than buffered whole in memory.
package storage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrInvalidKey is returned when a key is empty, absolute, or attempts to
// traverse outside the storage root.
var ErrInvalidKey = errors.New("invalid key")

// Store is a filesystem blob store rooted at an absolute, symlink-resolved
// directory. All keys resolve to paths guaranteed to stay within root.
type Store struct {
	root string // absolute, symlinks resolved
}

// NewStore creates (if needed) and canonicalises the root directory, returning
// a Store that serves keys beneath it.
func NewStore(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// Resolve symlinks in the root itself so containment checks compare against
	// the real path (the store root may legitimately be a symlink).
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return &Store{root: abs}, nil
}

// Root returns the absolute storage root.
func (s *Store) Root() string { return s.root }

// cleanKey validates a key and returns its path segments, rejecting empty
// keys, absolute paths, NUL bytes, and any traversal ("..") segment. It is
// pure (no filesystem access) so the containment rules can be unit-tested.
func cleanKey(key string) ([]string, error) {
	if key == "" {
		return nil, ErrInvalidKey
	}
	if strings.ContainsRune(key, 0) {
		return nil, ErrInvalidKey
	}
	// Reject absolute keys (unix "/foo", or an OS-absolute/Windows-drive path).
	if strings.HasPrefix(key, "/") || filepath.IsAbs(key) {
		return nil, ErrInvalidKey
	}
	segs := strings.Split(filepath.ToSlash(key), "/")
	clean := make([]string, 0, len(segs))
	for _, seg := range segs {
		switch seg {
		case "", ".":
			// collapse empty and current-dir segments
			continue
		case "..":
			return nil, ErrInvalidKey
		default:
			clean = append(clean, seg)
		}
	}
	if len(clean) == 0 {
		return nil, ErrInvalidKey
	}
	return clean, nil
}

// withinRoot reports whether p lies at or beneath root.
func withinRoot(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Resolve turns a request key into an absolute on-disk path, guaranteeing the
// result stays within the store root. It rejects traversal, absolute paths and
// symlink escapes with ErrInvalidKey.
func (s *Store) Resolve(key string) (string, error) {
	clean, err := cleanKey(key)
	if err != nil {
		return "", err
	}
	full := filepath.Join(append([]string{s.root}, clean...)...)
	// Defence in depth: even after segment cleaning, verify containment.
	if !withinRoot(s.root, full) {
		return "", ErrInvalidKey
	}
	// Reject symlink escapes: follow symlinks on the deepest existing portion
	// of the path and confirm it still resolves within the root.
	if ok, err := s.symlinkContained(full); err != nil {
		return "", err
	} else if !ok {
		return "", ErrInvalidKey
	}
	return full, nil
}

// symlinkContained walks up from full to the deepest existing ancestor,
// resolves its symlinks, and confirms the real path stays within root. Paths
// that do not yet exist (e.g. a fresh PUT target) are contained as long as
// their existing ancestors are.
func (s *Store) symlinkContained(full string) (bool, error) {
	p := full
	for {
		real, err := filepath.EvalSymlinks(p)
		if err == nil {
			return withinRoot(s.root, real), nil
		}
		if !os.IsNotExist(err) {
			return false, err
		}
		parent := filepath.Dir(p)
		if parent == p {
			// Reached the filesystem root without finding an existing path.
			return true, nil
		}
		p = parent
	}
}
