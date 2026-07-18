package metadata

import "embed"

// MigrationFS embeds the metadata-service SQL migrations. It is passed to
// pg.Migrate, which runs every migrations/*.sql file in filename order. The
// migrations are idempotent (IF NOT EXISTS / ON CONFLICT) so boot is safe.
//
//go:embed migrations/*.sql
var MigrationFS embed.FS
