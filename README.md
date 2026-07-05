# RepoRangler

Multi-ecosystem private package registry — a Go rewrite of the original PHP
platform. One monorepo, one Go module, one binary per service.

## Architecture

Each service presents an **external protocol** and owns its internals. Three
backends hold state; four facades translate a package-manager protocol into
calls against them.

```
                 ┌──────────────┐
  composer ─────▶│ php-service  │─┐
  npm      ─────▶│ npm-service  │ │   ┌───────────────┐   ┌───────────────┐
  pip      ─────▶│ pypi-service │ ├──▶│ metadata-svc  │   │ storage-svc   │
  go       ─────▶│ go-service   │ │   │ (Postgres)    │   │ (blobs)       │
                 └──────────────┘ │   └───────────────┘   └───────────────┘
                                  └──▶ auth-service (Postgres) ◀── every service
                                       validates bearer tokens here
```

- **auth-service** — issues + validates opaque bearer tokens; users + capabilities.
- **metadata-service** — system of record: packages, package-groups, repositories.
- **storage-service** — blob store (package files) by key.
- **php / npm / pypi / goproxy** — stateless protocol facades over the three backends.
- **cli** — operator tool (login, user/group/repo/token management, publish).

Not in this repo: `reporangler/admin` (React UI, a REST client) and
`reporangler/website` (Jekyll/Pages) live separately — they're browser
frontends, not part of the Go platform.

## Layout

```
cmd/{auth,metadata,storage,php,npm,pypi,goproxy,cli}/   one main per binary
internal/
  types/           shared domain model (User, Capability, PackageGroup, Repository)
  config/ httpx/    env + HTTP helpers
  authclient/ metadataclient/ storageclient/   inter-service HTTP clients
  middleware/       auth guards (Token = mandatory, Repo = optional/public)
  auth/             auth-service internals (store, service, handlers, migrations)
deploy/             Dockerfile, dev.Dockerfile (air), docker-compose.yml, .air.toml
docs/legacy/        functional specs of the original PHP services (the port's source of truth)
```

## Run it (dev, hot-reload via air)

```sh
cd deploy && docker compose up
```

Services publish to host ports 8001–8007; Postgres on 5432. A seed admin user
(`admin` / `admin`) is created on first boot. Smoke test:

```sh
# issue a token
TOKEN=$(curl -s http://localhost:8001/login/api \
  -H 'reporangler-login-type: database' \
  -H 'reporangler-login-username: admin' \
  -H 'reporangler-login-password: admin' | jq -r .token)

# validate it
curl -s http://localhost:8001/login/token -H "Authorization: Bearer $TOKEN" | jq
```

## Build / test (no local Go needed)

```sh
docker run --rm -v "$PWD":/app -w /app golang:1.25-alpine sh -c 'go build ./... && go test ./...'
```

## Status

Scaffold + **auth-service vertical slice** (login + token introspection against
Postgres, with tests). Backends and facades are healthz stubs; contracts to
implement are in `docs/legacy/README.md`. Migration order: `auth → metadata →
storage → goproxy → npm → pypi → php → cli`.
