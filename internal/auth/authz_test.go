package auth

import (
	"testing"

	"github.com/reporangler/reporangler/internal/types"
)

func TestIdOrName(t *testing.T) {
	cases := []struct {
		in     string
		wantID int64
		wantOK bool
	}{
		{"123", 123, true},
		{"0", 0, true},
		{"9007199254740993", 9007199254740993, true},
		{"alice", 0, false},
		{"12a", 0, false},
		{"a12", 0, false},
		{"", 0, false},
		{"-5", 0, false}, // leading sign is a name, not a numeric id
		{"1.0", 0, false},
	}
	for _, c := range cases {
		gotID, gotOK := idOrName(c.in)
		if gotOK != c.wantOK || (gotOK && gotID != c.wantID) {
			t.Errorf("idOrName(%q) = (%d, %v), want (%d, %v)", c.in, gotID, gotOK, c.wantID, c.wantOK)
		}
	}
}

func TestCanRevokeAdmin(t *testing.T) {
	cases := map[int]bool{
		0: false, // impossible in practice, but must not permit
		1: false, // the last admin — refuse
		2: true,
		9: true,
	}
	for count, want := range cases {
		if got := canRevokeAdmin(count); got != want {
			t.Errorf("canRevokeAdmin(%d) = %v, want %v", count, got, want)
		}
	}
}

func TestIsPackageGroupAdmin(t *testing.T) {
	// Global admin may administer any group.
	admin := types.User{IsAdminUser: true, PackageGroups: map[string]string{}}
	if !isPackageGroupAdmin(admin, "anything") {
		t.Error("global admin should administer any package group")
	}

	// Group admin may administer only their own group with admin access.
	groupAdmin := types.User{PackageGroups: map[string]string{"web": "admin", "infra": "access"}}
	if !isPackageGroupAdmin(groupAdmin, "web") {
		t.Error("group admin should administer their admin group")
	}
	if isPackageGroupAdmin(groupAdmin, "infra") {
		t.Error("access-only membership must not grant admin")
	}
	if isPackageGroupAdmin(groupAdmin, "unknown") {
		t.Error("non-member must not administer a group")
	}

	// Plain user administers nothing.
	plain := types.User{PackageGroups: map[string]string{}}
	if isPackageGroupAdmin(plain, "web") {
		t.Error("plain user should administer nothing")
	}
}
