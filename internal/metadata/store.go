package metadata

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reporangler/reporangler/internal/types"
)

var (
	// ErrNotFound is returned when a lookup finds no matching row.
	ErrNotFound = errors.New("not found")
	// ErrDuplicate is returned on a unique-constraint violation (23505).
	ErrDuplicate = errors.New("duplicate")
)

// Store is the Postgres-backed persistence layer for metadata-service.
type Store struct{ pool *pgxpool.Pool }

// NewStore wraps a pgx pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// isUniqueViolation maps a Postgres 23505 error to ErrDuplicate.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// --- Repositories ---

// ListRepositories returns every repository, ordered by name.
func (s *Store) ListRepositories(ctx context.Context) ([]types.Repository, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM repository ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []types.Repository{}
	for rows.Next() {
		var r types.Repository
		if err := rows.Scan(&r.ID, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RepositoryByName looks a repository up by its unique name.
func (s *Store) RepositoryByName(ctx context.Context, name string) (types.Repository, error) {
	var r types.Repository
	err := s.pool.QueryRow(ctx, `SELECT id, name FROM repository WHERE name = $1`, name).Scan(&r.ID, &r.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// RepositoryByID looks a repository up by id.
func (s *Store) RepositoryByID(ctx context.Context, id int64) (types.Repository, error) {
	var r types.Repository
	err := s.pool.QueryRow(ctx, `SELECT id, name FROM repository WHERE id = $1`, id).Scan(&r.ID, &r.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// CreateRepository inserts a repository, returning ErrDuplicate on a name clash.
func (s *Store) CreateRepository(ctx context.Context, name string) (types.Repository, error) {
	var r types.Repository
	err := s.pool.QueryRow(ctx, `INSERT INTO repository (name) VALUES ($1) RETURNING id, name`, name).Scan(&r.ID, &r.Name)
	if isUniqueViolation(err) {
		return r, ErrDuplicate
	}
	return r, err
}

// UpdateRepository renames a repository by id.
func (s *Store) UpdateRepository(ctx context.Context, id int64, name string) (types.Repository, error) {
	var r types.Repository
	err := s.pool.QueryRow(ctx,
		`UPDATE repository SET name = $2, updated_at = now() WHERE id = $1 RETURNING id, name`,
		id, name).Scan(&r.ID, &r.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if isUniqueViolation(err) {
		return r, ErrDuplicate
	}
	return r, err
}

// DeleteRepository removes a repository by id, returning ErrNotFound if absent.
func (s *Store) DeleteRepository(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM repository WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Package groups ---

// ListPackageGroups returns every package group, ordered by name.
func (s *Store) ListPackageGroups(ctx context.Context) ([]types.PackageGroup, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM package_group ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []types.PackageGroup{}
	for rows.Next() {
		var g types.PackageGroup
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// PackageGroupByName looks a package group up by its unique name.
func (s *Store) PackageGroupByName(ctx context.Context, name string) (types.PackageGroup, error) {
	var g types.PackageGroup
	err := s.pool.QueryRow(ctx, `SELECT id, name FROM package_group WHERE name = $1`, name).Scan(&g.ID, &g.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

// PackageGroupByID looks a package group up by id.
func (s *Store) PackageGroupByID(ctx context.Context, id int64) (types.PackageGroup, error) {
	var g types.PackageGroup
	err := s.pool.QueryRow(ctx, `SELECT id, name FROM package_group WHERE id = $1`, id).Scan(&g.ID, &g.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

// CreatePackageGroup inserts a group, returning ErrDuplicate on a name clash.
func (s *Store) CreatePackageGroup(ctx context.Context, name string) (types.PackageGroup, error) {
	var g types.PackageGroup
	err := s.pool.QueryRow(ctx, `INSERT INTO package_group (name) VALUES ($1) RETURNING id, name`, name).Scan(&g.ID, &g.Name)
	if isUniqueViolation(err) {
		return g, ErrDuplicate
	}
	return g, err
}

// UpdatePackageGroup renames a group by id.
func (s *Store) UpdatePackageGroup(ctx context.Context, id int64, name string) (types.PackageGroup, error) {
	var g types.PackageGroup
	err := s.pool.QueryRow(ctx,
		`UPDATE package_group SET name = $2, updated_at = now() WHERE id = $1 RETURNING id, name`,
		id, name).Scan(&g.ID, &g.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return g, ErrNotFound
	}
	if isUniqueViolation(err) {
		return g, ErrDuplicate
	}
	return g, err
}

// DeletePackageGroup removes a group by id, returning ErrNotFound if absent.
func (s *Store) DeletePackageGroup(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM package_group WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Packages ---

const packageCols = `id, name, version, package_group, definition, storage_key, package_type`

func scanPackage(row pgx.Row) (types.Package, error) {
	var p types.Package
	var def []byte
	var storageKey, packageType *string
	if err := row.Scan(&p.ID, &p.Name, &p.Version, &p.PackageGroup, &def, &storageKey, &packageType); err != nil {
		return p, err
	}
	if len(def) > 0 {
		_ = json.Unmarshal(def, &p.Definition)
	}
	if storageKey != nil {
		p.StorageKey = *storageKey
	}
	if packageType != nil {
		p.PackageType = *packageType
	}
	return p, nil
}

// ListPackages returns every package in a repository. ACL filtering is applied
// by the caller (filterPackagesForUser) so it stays pure and testable.
func (s *Store) ListPackages(ctx context.Context, repoID int64) ([]types.Package, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+packageCols+` FROM package WHERE repository_id = $1 ORDER BY name, version`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []types.Package{}
	for rows.Next() {
		p, err := scanPackage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PackagesByName returns every version of one package (by name) in a repository.
func (s *Store) PackagesByName(ctx context.Context, repoID int64, name string) ([]types.Package, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+packageCols+` FROM package WHERE repository_id = $1 AND name = $2 ORDER BY version`, repoID, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []types.Package{}
	for rows.Next() {
		p, err := scanPackage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertPackage inserts or updates a package version, keyed on the
// (name, version, repository_id, package_group) UNIQUE constraint. Publishers
// use this to re-publish; no admin gate.
func (s *Store) UpsertPackage(ctx context.Context, repoID int64, group, name, version string, definition map[string]any, storageKey, packageType string) (types.Package, error) {
	if definition == nil {
		definition = map[string]any{}
	}
	def, err := json.Marshal(definition)
	if err != nil {
		return types.Package{}, err
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO package (name, version, repository_id, package_group, definition, storage_key, package_type)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)
		ON CONFLICT (name, version, repository_id, package_group)
		DO UPDATE SET definition   = EXCLUDED.definition,
		              storage_key  = EXCLUDED.storage_key,
		              package_type = EXCLUDED.package_type,
		              updated_at   = now()
		RETURNING `+packageCols,
		name, version, repoID, group, def, nullif(storageKey), nullif(packageType))
	return scanPackage(row)
}

// DeletePackage removes a package by id within a repository, returning the ids
// actually deleted (empty when nothing matched).
func (s *Store) DeletePackage(ctx context.Context, repoID, id int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`DELETE FROM package WHERE repository_id = $1 AND id = $2 RETURNING id`, repoID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deleted := []int64{}
	for rows.Next() {
		var did int64
		if err := rows.Scan(&did); err != nil {
			return nil, err
		}
		deleted = append(deleted, did)
	}
	return deleted, rows.Err()
}

// nullif returns nil for an empty string so it lands as SQL NULL, else the value.
func nullif(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
