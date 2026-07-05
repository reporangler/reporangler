-- metadata-service schema. Ported from the legacy Laravel metadata-service
-- migrations. package_group is a plain string on package (NOT a FK) to mirror
-- the legacy model, but repository IS a FK. The composite UNIQUE key is what
-- POST /package upserts against.

CREATE TABLE IF NOT EXISTS package_group (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS repository (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS package (
  id            BIGSERIAL PRIMARY KEY,
  name          TEXT NOT NULL,
  version       TEXT NOT NULL,
  repository_id BIGINT NOT NULL REFERENCES repository(id) ON DELETE CASCADE,
  package_group TEXT NOT NULL,
  definition    JSONB NOT NULL DEFAULT '{}'::jsonb,
  storage_key   TEXT,
  package_type  TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (name, version, repository_id, package_group)
);
CREATE INDEX IF NOT EXISTS idx_package_repository ON package (repository_id);

-- Seed the four ecosystem repositories the facades speak for.
INSERT INTO repository (name) VALUES ('php'), ('npm'), ('pypi'), ('go')
ON CONFLICT (name) DO NOTHING;
