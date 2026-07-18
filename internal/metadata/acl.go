package metadata

import "github.com/reporangler/reporangler/internal/types"

// isNumericID reports whether s is a non-empty run of ASCII digits. It is the
// single decision the id-vs-name handlers branch on: a numeric path value is
// looked up by id, anything else by name. Mirrors the legacy `id = [0-9]+`.
func isNumericID(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// filterPackagesForUser applies the metadata-service ACL to a package list:
//   - admins (user.IsAdminUser) see everything;
//   - other users see only packages whose package_group is one of their
//     PackageGroups keys;
//   - a user with no package groups sees only the "public" group.
//
// It always returns a non-nil slice so the {count,data} envelope serialises
// data as [] rather than null.
func filterPackagesForUser(user types.User, pkgs []types.Package) []types.Package {
	if user.IsAdminUser {
		out := pkgs
		if out == nil {
			out = []types.Package{}
		}
		return out
	}

	allowed := make(map[string]bool, len(user.PackageGroups))
	for group := range user.PackageGroups {
		allowed[group] = true
	}
	if len(allowed) == 0 {
		allowed[types.PublicGroup] = true
	}

	out := make([]types.Package, 0, len(pkgs))
	for _, p := range pkgs {
		if allowed[p.PackageGroup] {
			out = append(out, p)
		}
	}
	return out
}
