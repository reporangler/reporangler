package auth

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/reporangler/reporangler/internal/types"
)

var (
	// ErrNotFound is returned when a user/token lookup finds nothing.
	ErrNotFound = errors.New("not found")
	// ErrUnauthorized is returned for invalid credentials or tokens.
	ErrUnauthorized = errors.New("unauthorized")
)

// Store is the Postgres-backed persistence layer for auth-service.
type Store struct{ pool *pgxpool.Pool }

// NewStore wraps a pgx pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) loadCapabilities(ctx context.Context, userID int64) ([]types.CapabilityMap, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.name, cm."constraint"
		FROM capability_map cm
		JOIN capability c ON c.id = cm.capability_id
		WHERE cm.entity_type = 'user' AND cm.entity_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var caps []types.CapabilityMap
	for rows.Next() {
		var name string
		var raw []byte
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		cap := types.CapabilityMap{Name: name}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &cap.Constraint)
		}
		caps = append(caps, cap)
	}
	return caps, rows.Err()
}

// userByUsername loads a user (with capabilities) plus the bcrypt password hash.
func (s *Store) userByUsername(ctx context.Context, username string) (types.User, string, error) {
	var u types.User
	var hash string
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, email, password FROM "user" WHERE username = $1`, username).
		Scan(&u.ID, &u.Username, &u.Email, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, "", ErrNotFound
	}
	if err != nil {
		return u, "", err
	}
	caps, err := s.loadCapabilities(ctx, u.ID)
	if err != nil {
		return u, "", err
	}
	u.Capability = caps
	u.ComputeDerived()
	return u, hash, nil
}

// insertLoginToken persists an issued bearer token with its expiry.
func (s *Store) insertLoginToken(ctx context.Context, userID int64, token string, expire time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO login_tokens (token, user_id, expire_at) VALUES ($1, $2, $3)`,
		token, userID, expire)
	return err
}

// userByToken validates a bearer token, deleting it and returning
// ErrUnauthorized if it has expired (lazy cleanup, as the legacy service did).
func (s *Store) userByToken(ctx context.Context, token string) (types.User, error) {
	var u types.User
	var expire time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.email, lt.expire_at
		FROM login_tokens lt
		JOIN "user" u ON u.id = lt.user_id
		WHERE lt.token = $1
		ORDER BY lt.expire_at DESC
		LIMIT 1`, token).
		Scan(&u.ID, &u.Username, &u.Email, &expire)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrUnauthorized
	}
	if err != nil {
		return u, err
	}
	if time.Now().After(expire) {
		_, _ = s.pool.Exec(ctx, `DELETE FROM login_tokens WHERE token = $1`, token)
		return types.User{}, ErrUnauthorized
	}
	caps, err := s.loadCapabilities(ctx, u.ID)
	if err != nil {
		return u, err
	}
	u.Capability = caps
	u.ComputeDerived()
	return u, nil
}

// EnsureAdmin idempotently creates the seed admin user (from env at boot) and
// grants it IS_ADMIN_USER + IS_REST_USER, mirroring the legacy InitialAdminUser
// seeder.
func (s *Store) EnsureAdmin(ctx context.Context, username, email, passwordHash string) error {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM "user" WHERE username = $1`, username).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx,
			`INSERT INTO "user" (username, email, password) VALUES ($1, $2, $3) RETURNING id`,
			username, email, passwordHash).Scan(&id)
	}
	if err != nil {
		return err
	}
	for _, capName := range []string{types.CapIsAdminUser, types.CapIsRestUser} {
		if _, err := s.pool.Exec(ctx, `
			INSERT INTO capability_map (entity_type, entity_id, capability_id)
			SELECT 'user', $1, c.id FROM capability c
			WHERE c.name = $2
			  AND NOT EXISTS (
			    SELECT 1 FROM capability_map m
			    WHERE m.entity_type = 'user' AND m.entity_id = $1 AND m.capability_id = c.id
			  )`, id, capName); err != nil {
			return err
		}
	}
	return nil
}
