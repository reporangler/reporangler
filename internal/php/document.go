// Package php implements the Composer (PHP) repository facade. It presents the
// modern Composer v2 read protocol (packages.json + /p2 metadata) built from
// metadata-service records, resolving dist URLs to storage-service. There is
// deliberately NO Composer/Satis engine and NO VCS scanning: we own the
// internals and only speak the external wire protocol.
package php

import (
	"fmt"
	"sort"
	"strings"

	"github.com/reporangler/reporangler/internal/types"
)

// repoType is the metadata-service repository this facade fronts.
const repoType = "php"

// metadataURLTemplate is the Composer v2 lazy-metadata template advertised in
// packages.json. Composer substitutes %package% with "vendor/package".
const metadataURLTemplate = "/p2/%package%.json"

// buildRootDocument builds the packages.json entrypoint document. It advertises
// the lazy /p2 metadata URL and an empty inline packages map (Composer v2 fetches
// each package lazily). When names is non-empty it also lists them under
// "available-packages" so clients can enumerate without wildcard search.
func buildRootDocument(names []string) map[string]any {
	doc := map[string]any{
		"metadata-url": metadataURLTemplate,
		"packages":     map[string]any{},
	}
	if len(names) > 0 {
		doc["available-packages"] = names
	}
	return doc
}

// buildP2Document builds the Composer v2 /p2/{vendor}/{package}.json metadata
// document for a single package name from its stored versions.
//
// Each version object is the stored composer.json (definition) merged with the
// canonical name, its version string, and a dist block pointing at storage.
// distURL maps a storage key to a client-facing download URL (in production this
// is storageclient.Client.DownloadURL); passing it as a function keeps the
// shaping logic pure and unit-testable.
func buildP2Document(name string, pkgs []types.Package, distURL func(key string) string) map[string]any {
	versions := make([]map[string]any, 0, len(pkgs))
	for _, p := range pkgs {
		obj := make(map[string]any, len(p.Definition)+3)
		for k, v := range p.Definition {
			obj[k] = v
		}
		// The metadata record is authoritative for name and version.
		obj["name"] = name
		obj["version"] = p.Version
		if p.StorageKey != "" {
			obj["dist"] = map[string]any{
				"url":  distURL(p.StorageKey),
				"type": "zip",
			}
		}
		versions = append(versions, obj)
	}
	return map[string]any{
		"packages": map[string]any{
			name: versions,
		},
	}
}

// packageNames returns the sorted, de-duplicated set of package names from a
// flat list of package versions (as returned by metadataclient.GetPackages).
func packageNames(pkgs []types.Package) []string {
	seen := make(map[string]struct{}, len(pkgs))
	names := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		if p.Name == "" {
			continue
		}
		if _, ok := seen[p.Name]; ok {
			continue
		}
		seen[p.Name] = struct{}{}
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}

// resolveGroup picks the package group for an ingested package:
// the x-package-group header, else the vendor (first path segment of the
// package name), else the public group.
func resolveGroup(headerGroup, name string) string {
	if g := strings.TrimSpace(headerGroup); g != "" {
		return g
	}
	if i := strings.Index(name, "/"); i > 0 {
		return name[:i]
	}
	return types.PublicGroup
}

// sanitizeName collapses an arbitrary package/group/version string into a single
// filesystem-safe path component: lower-cased, with any character outside
// [a-z0-9._-] replaced by '-', consecutive dashes collapsed, and leading/trailing
// separators trimmed. A "vendor/package" name becomes "vendor-package". This
// also neutralises path-traversal inputs (e.g. ".." -> "package").
func sanitizeName(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		var c byte
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			c = byte(r)
		default:
			c = '-'
		}
		if c == '-' {
			if prevDash {
				continue
			}
			prevDash = true
		} else {
			prevDash = false
		}
		b.WriteByte(c)
	}
	out := strings.Trim(b.String(), ".-_")
	if out == "" {
		return "package"
	}
	return out
}

// storagePath is the deterministic storage-service key for a package version's
// dist zip: php/{group}/{name-sanitized}/{version}/{name-sanitized}-{version}.zip
//
// The key is a pure function of (group, name, version), so the metadata-only
// publish path (dist already uploaded) and the multipart publish path (we upload
// the bytes) agree on the same location without the client having to supply it.
func storagePath(group, name, version string) string {
	g := sanitizeName(group)
	n := sanitizeName(name)
	v := sanitizeName(version)
	return fmt.Sprintf("%s/%s/%s/%s/%s-%s.zip", repoType, g, n, v, n, v)
}
