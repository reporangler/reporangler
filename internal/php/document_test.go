package php

import (
	"reflect"
	"testing"

	"github.com/reporangler/reporangler/internal/types"
)

// stubDistURL mimics storageclient.Client.DownloadURL for tests.
func stubDistURL(key string) string { return "https://dl.example/object/" + key }

func TestBuildP2Document(t *testing.T) {
	pkgs := []types.Package{
		{
			Name:       "acme/widget",
			Version:    "1.0.0",
			StorageKey: "php/acme/acme-widget/1.0.0/acme-widget-1.0.0.zip",
			Definition: map[string]any{
				"name":        "acme/widget",
				"description": "A widget",
				"require":     map[string]any{"php": ">=8.1"},
			},
		},
		{
			Name:       "acme/widget",
			Version:    "1.1.0",
			StorageKey: "php/acme/acme-widget/1.1.0/acme-widget-1.1.0.zip",
			Definition: map[string]any{"name": "acme/widget"},
		},
	}

	doc := buildP2Document("acme/widget", pkgs, stubDistURL)

	packages, ok := doc["packages"].(map[string]any)
	if !ok {
		t.Fatalf("packages is not a map: %T", doc["packages"])
	}
	versions, ok := packages["acme/widget"].([]map[string]any)
	if !ok {
		t.Fatalf("package entry is not a []map: %T", packages["acme/widget"])
	}
	if len(versions) != 2 {
		t.Fatalf("want 2 versions, got %d", len(versions))
	}

	v0 := versions[0]
	if v0["version"] != "1.0.0" {
		t.Errorf("version = %v, want 1.0.0", v0["version"])
	}
	if v0["name"] != "acme/widget" {
		t.Errorf("name = %v, want acme/widget", v0["name"])
	}
	// definition fields carry through
	if v0["description"] != "A widget" {
		t.Errorf("description not carried through: %v", v0["description"])
	}
	// dist block
	dist, ok := v0["dist"].(map[string]any)
	if !ok {
		t.Fatalf("dist is not a map: %T", v0["dist"])
	}
	wantURL := "https://dl.example/object/php/acme/acme-widget/1.0.0/acme-widget-1.0.0.zip"
	if dist["url"] != wantURL {
		t.Errorf("dist.url = %v, want %v", dist["url"], wantURL)
	}
	if dist["type"] != "zip" {
		t.Errorf("dist.type = %v, want zip", dist["type"])
	}
}

func TestBuildP2DocumentNoStorageKeyOmitsDist(t *testing.T) {
	pkgs := []types.Package{{Name: "acme/widget", Version: "1.0.0", Definition: map[string]any{}}}
	doc := buildP2Document("acme/widget", pkgs, stubDistURL)
	versions := doc["packages"].(map[string]any)["acme/widget"].([]map[string]any)
	if _, has := versions[0]["dist"]; has {
		t.Errorf("dist should be omitted when storage key is empty")
	}
}

func TestBuildRootDocument(t *testing.T) {
	// with names -> available-packages present
	doc := buildRootDocument([]string{"acme/widget", "acme/gadget"})
	if doc["metadata-url"] != metadataURLTemplate {
		t.Errorf("metadata-url = %v", doc["metadata-url"])
	}
	if pk, ok := doc["packages"].(map[string]any); !ok || len(pk) != 0 {
		t.Errorf("packages should be an empty map, got %v", doc["packages"])
	}
	if ap, ok := doc["available-packages"].([]string); !ok || len(ap) != 2 {
		t.Errorf("available-packages = %v", doc["available-packages"])
	}

	// no names -> available-packages omitted
	doc2 := buildRootDocument(nil)
	if _, has := doc2["available-packages"]; has {
		t.Errorf("available-packages should be omitted when empty")
	}
}

func TestPackageNames(t *testing.T) {
	pkgs := []types.Package{
		{Name: "acme/widget", Version: "1.0.0"},
		{Name: "acme/widget", Version: "1.1.0"},
		{Name: "acme/gadget", Version: "2.0.0"},
		{Name: ""},
	}
	got := packageNames(pkgs)
	want := []string{"acme/gadget", "acme/widget"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("packageNames = %v, want %v", got, want)
	}
}

func TestResolveGroup(t *testing.T) {
	cases := []struct {
		header, name, want string
	}{
		{"team-a", "acme/widget", "team-a"}, // header wins
		{"", "acme/widget", "acme"},         // vendor segment
		{"", "widget", types.PublicGroup},   // no vendor -> public
		{"  ", "acme/widget", "acme"},       // blank header ignored
		{"", "/widget", types.PublicGroup},  // empty vendor -> public
	}
	for _, c := range cases {
		if got := resolveGroup(c.header, c.name); got != c.want {
			t.Errorf("resolveGroup(%q,%q) = %q, want %q", c.header, c.name, got, c.want)
		}
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"acme/widget": "acme-widget",
		"Acme/Widget": "acme-widget",
		"a//b":        "a-b",
		"vendor.name": "vendor.name",
		"weird name!": "weird-name",
		"..":          "package", // traversal neutralised
		"/":           "package",
		"--x--":       "x",
		"@scope/pkg":  "scope-pkg",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStoragePath(t *testing.T) {
	got := storagePath("acme", "acme/widget", "1.0.0")
	want := "php/acme/acme-widget/1.0.0/acme-widget-1.0.0.zip"
	if got != want {
		t.Errorf("storagePath = %q, want %q", got, want)
	}

	// group / name / version are all sanitized into single safe components.
	got2 := storagePath("Team A", "Acme/Widget", "2.0.0-beta")
	want2 := "php/team-a/acme-widget/2.0.0-beta/acme-widget-2.0.0-beta.zip"
	if got2 != want2 {
		t.Errorf("storagePath sanitised = %q, want %q", got2, want2)
	}
}
