package npm

import (
	"encoding/base64"
	"path"
	"testing"
)

func TestSafeName(t *testing.T) {
	cases := map[string]string{
		"foo":        "foo",
		"@acme/foo":  "acme-foo",
		"@a/b":       "a-b",
		"bar-baz":    "bar-baz",
		"@scope/a.b": "scope-a.b",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScopeAndGroup(t *testing.T) {
	if s, ok := scopeOf("@acme/foo"); !ok || s != "acme" {
		t.Errorf("scopeOf(@acme/foo) = %q,%v", s, ok)
	}
	if _, ok := scopeOf("foo"); ok {
		t.Error("scopeOf(foo) should not be scoped")
	}
	if _, ok := scopeOf("@/foo"); ok {
		t.Error("scopeOf(@/foo) should not be scoped (empty scope)")
	}

	if g := packageGroup("@acme/foo", "ignored"); g != "acme" {
		t.Errorf("scoped group = %q, want acme", g)
	}
	if g := packageGroup("foo", "myteam"); g != "myteam" {
		t.Errorf("header group = %q, want myteam", g)
	}
	if g := packageGroup("foo", ""); g != "public" {
		t.Errorf("fallback group = %q, want public", g)
	}
}

func TestIntegrity(t *testing.T) {
	// Known vectors for the input "hello".
	const wantShasum = "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d"
	const wantIntegrity = "sha512-m3HSJL1i83hdltRq0+o9czGb+8KJDKra4t/3JRlnPKcjI8PZm6XBHXx6zG4UuMXaDEZjR1wuXDre9G9zvN7AQw=="

	shasum, subresource := integrity([]byte("hello"))
	if shasum != wantShasum {
		t.Errorf("shasum = %q, want %q", shasum, wantShasum)
	}
	if subresource != wantIntegrity {
		t.Errorf("integrity = %q, want %q", subresource, wantIntegrity)
	}
	// integrity must be "sha512-" + base64 of exactly 64 raw digest bytes.
	if len(subresource) <= len("sha512-") || subresource[:7] != "sha512-" {
		t.Fatalf("integrity missing sha512- prefix: %q", subresource)
	}
	digest, err := base64.StdEncoding.DecodeString(subresource[7:])
	if err != nil {
		t.Fatalf("integrity payload not valid base64: %v", err)
	}
	if len(digest) != 64 {
		t.Errorf("sha512 digest length = %d, want 64", len(digest))
	}
}

func TestTarballURLBasenameMatchesStorageKey(t *testing.T) {
	cases := []struct {
		name, group, version string
	}{
		{"foo", "public", "1.0.0"},
		{"@acme/foo", "acme", "2.3.4"},
		{"@scope/a.b", "scope", "0.0.1-beta.1"},
	}
	base := "https://npm.reporangler.localhost"
	for _, c := range cases {
		key := storageKey(c.group, c.name, c.version)
		url := tarballURL(base, c.name, key)

		// The parity fix: the advertised tarball filename must equal the
		// storage key basename, for scoped packages too.
		if got, want := path.Base(url), path.Base(key); got != want {
			t.Errorf("%s@%s: tarball basename %q != key basename %q", c.name, c.version, got, want)
		}
		// Sanity: key basename uses the sanitized name and version.
		wantBase := safeName(c.name) + "-" + c.version + ".tgz"
		if path.Base(key) != wantBase {
			t.Errorf("%s@%s: key basename %q, want %q", c.name, c.version, path.Base(key), wantBase)
		}
	}
}

func TestStorageKeyLayout(t *testing.T) {
	got := storageKey("acme", "@acme/foo", "1.2.3")
	want := "npm/acme/acme-foo/1.2.3/acme-foo-1.2.3.tgz"
	if got != want {
		t.Errorf("storageKey = %q, want %q", got, want)
	}
}

func TestAttachmentName(t *testing.T) {
	if got := attachmentName("@acme/foo", "1.0.0"); got != "@acme/foo-1.0.0.tgz" {
		t.Errorf("attachmentName scoped = %q", got)
	}
	if got := attachmentName("foo", "1.0.0"); got != "foo-1.0.0.tgz" {
		t.Errorf("attachmentName = %q", got)
	}
}

func TestLatestVersion(t *testing.T) {
	if got := latestVersion([]string{"1.0.0", "1.2.0", "1.1.5"}); got != "1.2.0" {
		t.Errorf("latest = %q, want 1.2.0", got)
	}
	if got := latestVersion([]string{"2.0.0-rc.1", "1.9.9"}); got != "2.0.0-rc.1" {
		t.Errorf("latest with prerelease = %q, want 2.0.0-rc.1", got)
	}
	if got := latestVersion([]string{"not-semver"}); got != "not-semver" {
		t.Errorf("latest fallback = %q, want not-semver", got)
	}
	if got := latestVersion(nil); got != "" {
		t.Errorf("latest empty = %q, want empty", got)
	}
}
