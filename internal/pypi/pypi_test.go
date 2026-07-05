package pypi

import "testing"

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"Django":               "django",
		"pytest":               "pytest",
		"Flask-SQLAlchemy":     "flask-sqlalchemy",
		"typing_extensions":    "typing-extensions",
		"zope.interface":       "zope-interface",
		"Foo..__--Bar":         "foo-bar",
		"A_-.B":                "a-b",
		"already-normalized":   "already-normalized",
		"Mixed_Case.Name-Here": "mixed-case-name-here",
		"UPPER":                "upper",
	}
	for in, want := range cases {
		if got := normalizeName(in); got != want {
			t.Errorf("normalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStorageKey(t *testing.T) {
	got := storageKey("public", "flask-sqlalchemy", "3.1.1", "Flask_SQLAlchemy-3.1.1-py3-none-any.whl")
	want := "pypi/public/flask-sqlalchemy/3.1.1/Flask_SQLAlchemy-3.1.1-py3-none-any.whl"
	if got != want {
		t.Errorf("storageKey = %q, want %q", got, want)
	}

	// Layout is always pypi/{group}/{name}/{version}/{filename} (5 slash-joined segments).
	got = storageKey("team-a", "my-pkg", "0.0.1", "my_pkg-0.0.1.tar.gz")
	want = "pypi/team-a/my-pkg/0.0.1/my_pkg-0.0.1.tar.gz"
	if got != want {
		t.Errorf("storageKey = %q, want %q", got, want)
	}
}

func TestStorageKeyUsesNormalizedName(t *testing.T) {
	// The name segment must be run through normalizeName by callers; verify the
	// two compose to the expected key.
	name := "Flask_SQLAlchemy"
	got := storageKey("public", normalizeName(name), "3.1.1", "dist.whl")
	want := "pypi/public/flask-sqlalchemy/3.1.1/dist.whl"
	if got != want {
		t.Errorf("storageKey(normalizeName(%q)) = %q, want %q", name, got, want)
	}
}

func TestContentTypeFor(t *testing.T) {
	cases := map[string]string{
		"pkg-1.0.tar.gz":       "application/gzip",
		"pkg-1.0.TAR.GZ":       "application/gzip",
		"pkg-1.0.tgz":          "application/gzip",
		"pkg-1.0-py3-none.whl": "application/zip",
		"pkg-1.0.zip":          "application/zip",
		"pkg-1.0.WHL":          "application/zip",
		"README":               "application/octet-stream",
		"pkg-1.0.tar.bz2":      "application/octet-stream",
	}
	for in, want := range cases {
		if got := contentTypeFor(in); got != want {
			t.Errorf("contentTypeFor(%q) = %q, want %q", in, got, want)
		}
	}
}
