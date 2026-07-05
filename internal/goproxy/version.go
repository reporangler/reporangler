package goproxy

import (
	"sort"

	"golang.org/x/mod/semver"
)

// sortVersions orders versions ascending by real semver (golang.org/x/mod/
// semver), replacing the legacy naive string / version_compare sort. Versions
// that are not valid semver are sorted lexicographically and placed before the
// valid ones, so the highest valid release ends up last.
func sortVersions(vs []string) {
	sort.SliceStable(vs, func(i, j int) bool {
		a, b := vs[i], vs[j]
		av, bv := semver.IsValid(a), semver.IsValid(b)
		if av && bv {
			return semver.Compare(a, b) < 0
		}
		if av != bv {
			// invalid sorts before valid.
			return bv
		}
		return a < b
	})
}

// latestVersion selects the version returned by @latest, following the Go
// toolchain rule: prefer the highest valid, non-prerelease (release) version;
// only if there is no release version at all fall back to the highest
// prerelease. Returns "" when no valid semver version is present.
func latestVersion(vs []string) string {
	bestRelease := ""
	bestAny := ""
	for _, v := range vs {
		if !semver.IsValid(v) {
			continue
		}
		if bestAny == "" || semver.Compare(v, bestAny) > 0 {
			bestAny = v
		}
		if semver.Prerelease(v) == "" {
			if bestRelease == "" || semver.Compare(v, bestRelease) > 0 {
				bestRelease = v
			}
		}
	}
	if bestRelease != "" {
		return bestRelease
	}
	return bestAny
}
