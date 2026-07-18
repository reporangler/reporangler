package goproxy

import (
	"reflect"
	"testing"
)

func TestLatestVersion(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"picks highest release", []string{"v1.0.0", "v1.2.0", "v1.1.0"}, "v1.2.0"},
		{"ignores order", []string{"v0.9.0", "v0.10.0", "v0.2.0"}, "v0.10.0"}, // semver, not string
		{"excludes higher prerelease", []string{"v1.0.0", "v2.0.0-beta", "v1.5.0"}, "v1.5.0"},
		{"prerelease excluded even when highest", []string{"v2.0.0-rc.1", "v1.9.0"}, "v1.9.0"},
		{"all prerelease falls back to highest prerelease", []string{"v1.0.0-alpha", "v1.0.0-beta"}, "v1.0.0-beta"},
		{"build metadata release beats prerelease", []string{"v1.0.0+meta", "v1.0.0-rc"}, "v1.0.0+meta"},
		{"single", []string{"v3.4.5"}, "v3.4.5"},
		{"invalid skipped", []string{"latest", "v1.1.0", "garbage"}, "v1.1.0"},
		{"none valid", []string{"latest", "garbage"}, ""},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := latestVersion(c.in); got != c.want {
				t.Errorf("latestVersion(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestSortVersions(t *testing.T) {
	in := []string{"v1.2.0", "v1.10.0", "v1.1.0", "v2.0.0-beta", "v2.0.0"}
	want := []string{"v1.1.0", "v1.2.0", "v1.10.0", "v2.0.0-beta", "v2.0.0"}
	sortVersions(in)
	if !reflect.DeepEqual(in, want) {
		t.Errorf("sortVersions = %v, want %v", in, want)
	}
}
