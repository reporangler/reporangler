// Package types holds the shared domain model that every service and the CLI
// import. It replaces lib-reporangler's Entity classes (User, Capability,
// CapabilityMap, PackageGroup, Repository) with plain Go structs.
package types

// Capability names — mirror lib-reporangler's RepoRangler\Entity\Capability.
const (
	CapIsPublicUser       = "IS_PUBLIC_USER"
	CapIsAdminUser        = "IS_ADMIN_USER"
	CapIsRestUser         = "IS_REST_USER"
	CapIsRepoUser         = "IS_REPO_USER"
	CapRepositoryAccess   = "REPOSITORY_ACCESS"
	CapPackageGroupAccess = "PACKAGE_GROUP_ACCESS"
)

// Well-known public identity.
const (
	PublicUsername = "public-user"
	PublicToken    = "public"
	PublicGroup    = "public"
)

// CapabilityMap is a capability grant with an optional JSON constraint.
type CapabilityMap struct {
	Name       string         `json:"name"`
	Constraint map[string]any `json:"constraint,omitempty"`
}

// User is the authenticated principal. The is_* flags and PackageGroups are
// derived from Capability by ComputeDerived and serialized for API consumers,
// matching the legacy User model's appended attributes.
type User struct {
	ID            int64             `json:"id"`
	Username      string            `json:"username"`
	Email         string            `json:"email"`
	Token         string            `json:"token,omitempty"`
	Capability    []CapabilityMap   `json:"capability"`
	IsAdminUser   bool              `json:"is_admin_user"`
	IsRestUser    bool              `json:"is_rest_user"`
	IsRepoUser    bool              `json:"is_repo_user"`
	IsPublicUser  bool              `json:"is_public_user"`
	PackageGroups map[string]string `json:"package_groups"`
}

// HasCapability reports whether the user holds a capability by name.
func (u *User) HasCapability(name string) bool {
	for _, c := range u.Capability {
		if c.Name == name {
			return true
		}
	}
	return false
}

// ComputeDerived fills the is_* flags and the PackageGroups map from Capability.
//
// Note: this fixes a legacy lib-reporangler bug where PublicUser seeded the
// PACKAGE_GROUP_ACCESS constraint as {name: ...} but User read {package_group:
// ...}. Here producers and reader agree on the "package_group" key.
func (u *User) ComputeDerived() {
	u.IsAdminUser = u.HasCapability(CapIsAdminUser)
	u.IsRestUser = u.HasCapability(CapIsRestUser)
	u.IsRepoUser = u.HasCapability(CapIsRepoUser)
	u.IsPublicUser = u.HasCapability(CapIsPublicUser)
	u.PackageGroups = map[string]string{}
	for _, c := range u.Capability {
		if c.Name != CapPackageGroupAccess {
			continue
		}
		grp, _ := c.Constraint["package_group"].(string)
		if grp == "" {
			continue
		}
		access := "access"
		if admin, _ := c.Constraint["admin"].(bool); admin {
			access = "admin"
		}
		u.PackageGroups[grp] = access
	}
}

// PublicUser returns the anonymous identity used by the optional (auth:repo)
// guard when no valid token is presented.
func PublicUser() User {
	u := User{
		Username: PublicUsername,
		Token:    PublicToken,
		Capability: []CapabilityMap{
			{Name: CapIsPublicUser},
			{Name: CapRepositoryAccess, Constraint: map[string]any{"name": "php"}},
			{Name: CapPackageGroupAccess, Constraint: map[string]any{"package_group": PublicGroup, "admin": false}},
		},
	}
	u.ComputeDerived()
	return u
}

// PackageGroup mirrors lib-reporangler's Entity\PackageGroup.
type PackageGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Repository mirrors lib-reporangler's Entity\Repository.
type Repository struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Package is a single package version stored in metadata-service. definition
// is the ecosystem-specific manifest (composer.json, npm version doc, etc.).
type Package struct {
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	Version      string         `json:"version"`
	PackageGroup string         `json:"package_group"`
	Definition   map[string]any `json:"definition"`
	StorageKey   string         `json:"storage_key,omitempty"`
	PackageType  string         `json:"package_type,omitempty"`
}

// ListResponse is the {count,data} envelope metadata-service returns for lists.
type ListResponse[T any] struct {
	Count int `json:"count"`
	Data  []T `json:"data"`
}
