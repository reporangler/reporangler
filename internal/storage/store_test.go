package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCleanKeyRejects verifies the pure containment rules reject traversal,
// absolute paths, and empty/degenerate keys.
func TestCleanKeyRejects(t *testing.T) {
	bad := []string{
		"",                // empty
		"..",              // bare traversal
		"../etc/passwd",   // leading traversal
		"a/../../b",       // mid-path escape
		"foo/..",          // trailing traversal
		"/etc/passwd",     // absolute (unix)
		"/",               // absolute root
		"a/b/../../../c",  // deep escape
		".",               // current dir only
		"./",              // current dir only, trailing slash
		"foo/\x00/bar",    // NUL byte
		string([]byte{0}), // NUL only
	}
	for _, k := range bad {
		if _, err := cleanKey(k); err == nil {
			t.Errorf("cleanKey(%q) = nil error, want ErrInvalidKey", k)
		}
	}
}

// TestCleanKeyAccepts verifies safe keys resolve to the expected segments,
// collapsing "." and empty segments.
func TestCleanKeyAccepts(t *testing.T) {
	cases := map[string][]string{
		"a.tgz":                        {"a.tgz"},
		"npm/group/pkg/1.0.0/pkg.tgz":  {"npm", "group", "pkg", "1.0.0", "pkg.tgz"},
		"a//b/./c":                     {"a", "b", "c"},
		"./a/b":                        {"a", "b"},
		"pypi/g/name/1.0/name-1.0.whl": {"pypi", "g", "name", "1.0", "name-1.0.whl"},
	}
	for k, want := range cases {
		got, err := cleanKey(k)
		if err != nil {
			t.Errorf("cleanKey(%q) error = %v, want nil", k, err)
			continue
		}
		if strings.Join(got, "/") != strings.Join(want, "/") {
			t.Errorf("cleanKey(%q) = %v, want %v", k, got, want)
		}
	}
}

// TestResolveContainsWithinRoot confirms safe keys resolve inside the root and
// unsafe keys are rejected, using a real temp root.
func TestResolveContainsWithinRoot(t *testing.T) {
	root := t.TempDir()
	s, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Safe keys resolve within the (symlink-resolved) root.
	safe := []string{"a.tgz", "npm/g/p/1.0.0/p.tgz", "deep/nested/dir/file.bin"}
	for _, k := range safe {
		full, err := s.Resolve(k)
		if err != nil {
			t.Errorf("Resolve(%q) error = %v, want nil", k, err)
			continue
		}
		if !withinRoot(s.Root(), full) {
			t.Errorf("Resolve(%q) = %q escapes root %q", k, full, s.Root())
		}
		if !strings.HasPrefix(full, s.Root()+string(filepath.Separator)) {
			t.Errorf("Resolve(%q) = %q not under root %q", k, full, s.Root())
		}
	}

	// Unsafe keys (including percent-decoded traversal, which the mux hands us
	// already decoded) are rejected.
	unsafe := []string{"../escape", "a/../../b", "/abs/path", "..", "foo/.."}
	for _, k := range unsafe {
		if _, err := s.Resolve(k); err == nil {
			t.Errorf("Resolve(%q) = nil error, want ErrInvalidKey", k)
		}
	}
}

// TestResolveRejectsSymlinkEscape confirms a symlink inside the root that
// points outside is not a usable escape hatch.
func TestResolveRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	s, err := NewStore(root)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Create a symlink inside root pointing at an external directory.
	link := filepath.Join(s.Root(), "evil")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	// A key that would traverse through the symlink to an external file must be
	// rejected as an escape.
	if _, err := s.Resolve("evil/secret.txt"); err == nil {
		t.Errorf("Resolve through escaping symlink = nil error, want ErrInvalidKey")
	}
}
