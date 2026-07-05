package metadata

import (
	"reflect"
	"testing"

	"github.com/reporangler/reporangler/internal/types"
)

func TestIsNumericID(t *testing.T) {
	cases := map[string]bool{
		"0":       true,
		"42":      true,
		"007":     true,
		"":        false,
		"12a":     false,
		"a12":     false,
		"1.2":     false,
		"-1":      false,
		" 1":      false,
		"1 ":      false,
		"php":     false,
		"acme-ui": false,
	}
	for in, want := range cases {
		if got := isNumericID(in); got != want {
			t.Errorf("isNumericID(%q) = %v, want %v", in, got, want)
		}
	}
}

func pkg(name, group string) types.Package {
	return types.Package{Name: name, Version: "1.0.0", PackageGroup: group}
}

func names(pkgs []types.Package) []string {
	out := make([]string, len(pkgs))
	for i, p := range pkgs {
		out[i] = p.Name
	}
	return out
}

func TestFilterPackagesForUser(t *testing.T) {
	all := []types.Package{
		pkg("public-lib", "public"),
		pkg("team-a-lib", "team-a"),
		pkg("team-b-lib", "team-b"),
	}

	tests := []struct {
		name string
		user types.User
		want []string
	}{
		{
			name: "admin sees everything",
			user: types.User{IsAdminUser: true},
			want: []string{"public-lib", "team-a-lib", "team-b-lib"},
		},
		{
			name: "user with one group sees only that group",
			user: types.User{PackageGroups: map[string]string{"team-a": "access"}},
			want: []string{"team-a-lib"},
		},
		{
			name: "user with two groups sees both",
			user: types.User{PackageGroups: map[string]string{"team-a": "access", "team-b": "admin"}},
			want: []string{"team-a-lib", "team-b-lib"},
		},
		{
			name: "user with no groups sees only public",
			user: types.User{PackageGroups: map[string]string{}},
			want: []string{"public-lib"},
		},
		{
			name: "user with nil groups sees only public",
			user: types.User{},
			want: []string{"public-lib"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := names(filterPackagesForUser(tc.user, all))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilterPackagesForUserAlwaysNonNil(t *testing.T) {
	// Empty input must still yield a non-nil slice so the JSON envelope emits [].
	if got := filterPackagesForUser(types.User{IsAdminUser: true}, nil); got == nil {
		t.Error("admin path returned nil slice, want non-nil")
	}
	if got := filterPackagesForUser(types.User{}, nil); got == nil {
		t.Error("filtered path returned nil slice, want non-nil")
	}
}

// TestFilterPackagesForUserViaCapabilities exercises the same ACL through the
// derived PackageGroups map that auth-service actually produces, so the filter
// and types.User.ComputeDerived stay in agreement.
func TestFilterPackagesForUserViaCapabilities(t *testing.T) {
	u := types.User{
		Capability: []types.CapabilityMap{
			{Name: types.CapPackageGroupAccess, Constraint: map[string]any{"package_group": "team-a", "admin": false}},
		},
	}
	u.ComputeDerived()

	all := []types.Package{pkg("public-lib", "public"), pkg("team-a-lib", "team-a")}
	got := names(filterPackagesForUser(u, all))
	want := []string{"team-a-lib"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
