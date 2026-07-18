# Parity build — shared rules for per-service work

You are building ONE service in a monorepo that several agents are building in
parallel in the **same working tree** (branch `feat/parity`). Obey the lane
rules exactly or you break other agents.

## Lane rules (STRICT)
- Create/edit files ONLY under your assigned lane: `cmd/<svc>/` and `internal/<svc>/` (+ `internal/<svc>/migrations/`). The auth service's lane is `cmd/auth/` + `internal/auth/`.
- DO NOT edit `go.mod`, `go.sum`, `tools.go`, or any `internal/` package other than your own. The shared packages below are DONE.
- DO NOT run `go mod tidy`, `go get`, or `go mod download` — every dep you need is already present (pgx, x/crypto/bcrypt, spf13/cobra, golang.org/x/mod).
- DO NOT run `go build ./...` / `go test ./...` / `go vet ./...` — that compiles other agents' half-written code. Build/test ONLY your own package paths (see below).

## Shared foundation (import these; already built + stable)
- `internal/types`: `User` (methods `HasCapability`, `ComputeDerived`; `PublicUser()` ctor; fields incl. `IsAdminUser`, `PackageGroups map[string]string`, `Token`, `Capability`), `Package{ID,Name,Version,PackageGroup,Definition map[string]any,StorageKey,PackageType}`, `PackageGroup{ID,Name}`, `Repository{ID,Name}`, `ListResponse[T]{Count,Data}`, consts `CapIsAdminUser`/`CapPackageGroupAccess`/…, `PublicUsername`/`PublicToken`/`PublicGroup`.
- `internal/config`: `Env(key,def) string`, `MustEnv(key) string`.
- `internal/httpx`: `JSON(w,status,v)`, `Error(w,status,msg)` → `{"code","message"}`, `Healthz(name) http.HandlerFunc` → `{"statusCode":200,"service":name}`.
- `internal/pg`: `Connect(ctx,url) (*pgxpool.Pool,error)` (retries ~30s); `Migrate(ctx,pool,embed.FS)` runs `migrations/*.sql` in order (write them idempotent: IF NOT EXISTS / ON CONFLICT). Embed with `//go:embed migrations/*.sql`.
- `internal/middleware`: `Token(ac) func(http.Handler)http.Handler` (mandatory bearer → 401), `Repo(ac)` (optional → falls back to `types.PublicUser()`), `UserFrom(ctx) (types.User,bool)`, `CORS(protocol,domain) func(http.Handler)http.Handler` (wrap the whole mux; it also answers OPTIONS preflight).
- `internal/authclient`: `New(baseURL string, hc *http.Client) *Client`; `.Check(ctx, authHeader) (types.User, error)`.
- `internal/metadataclient`: `New(baseURL, token string, hc *http.Client)`; `.GetPackages(ctx,repoType)`, `.GetPackagesByName(ctx,repoType,name)`, `.AddPackage(ctx,repoType,group,name,version,definition,storageKey,packageType)`, `.GetPackageGroups/CreatePackageGroup/GetPackageGroupByName/ByID/DeletePackageGroup`, `.GetRepositories/GetRepositoryByName/CreateRepository/DeleteRepository`.
- `internal/storageclient`: `New(baseURL, publicURL, token string, hc *http.Client)`; `.Upload(ctx,key,io.Reader)`, `.Download(ctx,key) (io.ReadCloser,error)`, `.Exists`, `.Delete`, `.DownloadURL(key)`, `.InternalDownloadURL(key)`.

## Routing
Stdlib `net/http.ServeMux` (Go 1.22+): `mux.HandleFunc("GET /path", h)`, params via `r.PathValue("x")`, catch-all `{rest...}` for keys/module paths that contain `/`. Wrap the mux: `middleware.CORS(proto,domain)(authMw(mux))` or apply auth per-route. Health route `GET /` via `httpx.Healthz(...)`. Add `OPTIONS /{p...}` only if not already covered by wrapping CORS around the mux (CORS middleware handles OPTIONS).

## Config env (read what you need)
`PORT` (8080), `APP_PROTOCOL`, `APP_DOMAIN`, `AUTH_BASE_URL`, `METADATA_BASE_URL`, `STORAGE_BASE_URL`, `STORAGE_PUBLIC_URL`, `DATABASE_URL` (backends). Facade `repoType` = its ecosystem (`php`/`npm`/`pypi`/`go`). Forward the authenticated user's `.Token` (from `middleware.UserFrom`) when constructing metadata/storage clients per request.

## Parity + fixes
Match the legacy PHP behavior in `docs/legacy/README.md`, and fold in the fixes noted there for your service: CORS preflight (use `middleware.CORS`), stream blobs (no whole-file buffering), storage path containment, real semver for goproxy, npm scoped-tarball URL == storage-key basename.

## Build/test (Docker; ONLY your lane)
```
docker run --rm -v /Volumes/sdcard256gb/projects/reporangler/reporangler:/app -w /app -v rr-gocache:/go golang:1.25-alpine \
  sh -c 'go build ./cmd/<svc>/... ./internal/<svc>/... && go vet ./cmd/<svc>/... ./internal/<svc>/... && go test ./internal/<svc>/...'
```
Iterate until green. Add unit tests for pure logic (name normalization, semver, key layout, hashing) that don't need a live DB/service. Return: files created, endpoints implemented, build/test result, parity gaps.
