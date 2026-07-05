-- auth-service schema. Ported from the legacy Laravel migrations, with two
-- fixes folded in: login_tokens gets an index on token (the PHP schema had
-- none), and capability_map gets an entity lookup index.

CREATE TABLE IF NOT EXISTS "user" (
  id         BIGSERIAL PRIMARY KEY,
  username   TEXT NOT NULL,
  email      TEXT NOT NULL,
  password   TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (username, email)
);

CREATE TABLE IF NOT EXISTS login_tokens (
  id         BIGSERIAL PRIMARY KEY,
  token      TEXT NOT NULL,
  user_id    BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  expire_at  TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_login_tokens_token ON login_tokens (token);

CREATE TABLE IF NOT EXISTS access_tokens (
  id         BIGSERIAL PRIMARY KEY,
  user_id    BIGINT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
  type       TEXT NOT NULL,
  token      TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, type)
);

CREATE TABLE IF NOT EXISTS capability (
  id         BIGSERIAL PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS capability_map (
  id            BIGSERIAL PRIMARY KEY,
  entity_type   TEXT NOT NULL,
  entity_id     BIGINT NOT NULL,
  capability_id BIGINT NOT NULL REFERENCES capability(id),
  "constraint"  JSONB,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_capability_map_entity ON capability_map (entity_type, entity_id);

INSERT INTO capability (name) VALUES
  ('IS_ADMIN_USER'),
  ('IS_REST_USER'),
  ('IS_REPO_USER'),
  ('REPOSITORY_ACCESS'),
  ('PACKAGE_GROUP_ACCESS')
ON CONFLICT (name) DO NOTHING;
