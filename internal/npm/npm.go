// Package npm implements the npm-registry facade: package metadata documents,
// tarball download, publish, and the /-/all index. It is stateless and
// delegates to metadata-service (the package index) and storage-service (the
// blob store).
//
// This file holds the pure, dependency-free logic (name sanitization, storage
// key layout, integrity hashing, tarball URL construction, latest-version
// selection) so it can be unit-tested without a live DB or HTTP services.
package npm

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/reporangler/reporangler/internal/types"
)

// repoType is this facade's ecosystem, used as the metadata-service repository.
const repoType = "npm"

// safeName sanitizes an npm package name for use in storage paths and tarball
// filenames: it drops the scope "@" and turns the scope separator "/" into "-".
// e.g. "@acme/foo" -> "acme-foo", "foo" -> "foo".
func safeName(name string) string {
	return strings.NewReplacer("@", "", "/", "-").Replace(name)
}

// scopeOf returns the scope of a scoped package name ("@acme/foo" -> "acme").
func scopeOf(name string) (string, bool) {
	if !strings.HasPrefix(name, "@") {
		return "", false
	}
	i := strings.Index(name, "/")
	if i <= 1 {
		return "", false
	}
	return name[1:i], true
}

// packageGroup resolves the metadata/storage package group for a publish or
// lookup: a scoped package uses its scope; otherwise the x-package-group header
// value, falling back to the public group.
func packageGroup(name, headerGroup string) string {
	if scope, ok := scopeOf(name); ok {
		return scope
	}
	if headerGroup != "" {
		return headerGroup
	}
	return types.PublicGroup
}

// storageKey is the cross-service blob layout for an npm tarball. It uses the
// sanitized safeName for BOTH the directory and the filename so that the
// filename advertised in dist.tarball (see tarballURL) equals the key basename
// even for scoped packages — the legacy PHP built the URL from the raw name,
// producing a filename that never matched the stored key (404 on download).
func storageKey(group, name, version string) string {
	sn := safeName(name)
	return fmt.Sprintf("%s/%s/%s/%s/%s-%s.tgz", repoType, group, sn, version, sn, version)
}

// attachmentName is the _attachments key npm uses for a version's tarball on
// publish: "<name>-<version>.tgz" with the raw (possibly scoped) package name.
func attachmentName(name, version string) string {
	return name + "-" + version + ".tgz"
}

// integrity computes the npm dist hashes for a raw tarball: the legacy
// "shasum" is sha1 as hex, and Subresource Integrity "integrity" is
// "sha512-" + base64(raw sha512 digest).
func integrity(raw []byte) (shasum, subresource string) {
	s1 := sha1.Sum(raw)
	s5 := sha512.Sum512(raw)
	return hex.EncodeToString(s1[:]), "sha512-" + base64.StdEncoding.EncodeToString(s5[:])
}

// tarballURL builds the dist.tarball download URL, served by this facade's own
// GET /{package}/-/{filename} route. The filename is path.Base(storageKey), so
// it always matches the stored blob — the tarball route selects the version by
// comparing basename(storage_key) to the requested filename.
func tarballURL(base, name, key string) string {
	return strings.TrimRight(base, "/") + "/" + name + "/-/" + path.Base(key)
}

// latestVersion picks the highest semver from a set of npm version strings
// (which omit the "v" prefix). Invalid versions are ignored; if none are valid
// semver, the last listed version is returned as a fallback. This replaces the
// legacy naive version_compare for the "latest" dist-tag.
func latestVersion(versions []string) string {
	best := ""
	for _, v := range versions {
		cv := v
		if !strings.HasPrefix(cv, "v") {
			cv = "v" + cv
		}
		if !semver.IsValid(cv) {
			continue
		}
		if best == "" || semver.Compare(cv, best) > 0 {
			best = cv
		}
	}
	if best == "" {
		if len(versions) > 0 {
			return versions[len(versions)-1]
		}
		return ""
	}
	return strings.TrimPrefix(best, "v")
}
