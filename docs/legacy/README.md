# Legacy PHP services — functional specs (port source of truth)

Distilled from a full read of the original Laravel services. This captures the
**wire contracts** the Go rewrite must preserve and the **bugs** to fix in the
port. The original repos are archived under the `reporangler` org for reference.

Total original code: ~8,300 hand-written PHP LOC across 9 components.

---

## Auth model (all services)

Opaque bearer tokens (64 hex chars, 6h TTL) stored in Postgres — **not JWT**.
Every service validates a token by calling auth-service `GET /login/token`.

- **`auth:token`** — mandatory bearer; 401 on missing/invalid.
- **`auth:repo`** — optional; falls back to the public user (`token="public"`,
  which auth-service refuses to validate remotely). Anonymous reads.

Authorization is capability-based: a user carries `capability_map` rows
(`IS_ADMIN_USER`, `PACKAGE_GROUP_ACCESS{package_group,admin}`, etc.).

Error envelope everywhere: `{"code":<int>,"message":<str>}` (+ `validation`
for 422, `exception`/`stack` when debug). Healthz is `GET /` →
`{"statusCode":200,"service":<url>}`.

---

## auth-service (Postgres) — the token issuer

Key endpoints:
- `GET /login/api` — headers `reporangler-login-{type,username,password}` → user+token. type ∈ {database, http-basic→database, ldap(unimplemented)}.
- `GET /login/token` — `Authorization: Bearer` → user JSON. **The introspection endpoint every service calls.**
- `GET /user`, `GET /user/{id|name}`, `POST /user`, `PUT/DELETE /user/{id}`
- `GET/POST/DELETE /access-token/{userId}[/{tokenId}]`
- `PUT/DELETE /permission/user/admin/{userId}`
- `POST /permission/package-group/{join,leave,protect,unprotect}`, `GET/POST /permission/package-group/approve` (approve is a stub)

Tables: `user`, `login_tokens`, `access_tokens`, `capability`, `capability_map`
(polymorphic entity_type/entity_id + jsonb `constraint`). See
`internal/auth/migrations/`.

---

## metadata-service (Postgres) — package index / system of record

All routes `auth:token`. `repository`/`name` = `[a-z][a-z0-9\-\.]+`, `id` = `[0-9]+`.
- `GET /package/{repository}` — list; **non-admins filtered to their package_groups** (empty → only `public`).
- `POST /package/{repository}` — upsert by (name,version,package_group). Body: `name,version,package_group,definition(json)[,storage_key][,package_type]`.
- `GET /package/{repository}/by-name/{name}` — all versions (name url-encoded).
- `DELETE /package/{repository}/{id}`
- `GET/POST/PUT/DELETE /package-group[/{id|name}]`
- `GET/POST/PUT/DELETE /repository[/{id|name}]`

Tables: `package_group`, `repository`, `package` (name,version,repository_id FK,
`package_group` **string not FK**, `definition` jsonb, `storage_key`,
`package_type`; UNIQUE(name,version,repository_id,package_group)). Seeds repos
`php,npm,pypi,go`.

---

## storage-service (filesystem) — blob store

Keys may contain `/` (catch-all). Store at `packages/<key>`.
- `GET /object/{key}` (`auth:repo`) — raw bytes, `application/octet-stream`, `Content-Disposition`.
- `GET /object-exists/{key}` → `{exists,key}`
- `GET /objects?prefix=` → `{count,data:[{key,size,modified}]}`
- `PUT /object/{key}` (`auth:token`, reject public user) — body = raw bytes → `{ok,key,size}` (201).
- `DELETE /object/{key}` (`auth:token`) → `{deleted,key}`

Port must: **stream** (io.Copy, no full-file buffering) and **contain paths**
(reject `..`/encoded traversal — the PHP had none).

---

## Facades (stateless) — present protocol, delegate to metadata+storage

Storage key layout is the cross-service contract — keep byte-identical:
- **php** (Composer/Satis): serve `packages.json` + include/`/p2` metadata from metadata records; `dist.url` → storage. **No Composer engine** — present the read protocol, own internals. Ingest is a free design choice (Composer has no publish command).
- **npm**: `GET /{pkg}` metadata doc, `GET /{pkg}/-/{file}.tgz` tarball, `PUT /{pkg}` publish, `GET /-/all`. Storage key `npm/{group}/{safeName}/{ver}/{safeName}-{ver}.tgz`. Hashes: `shasum=sha1(tgz)`, `integrity="sha512-"+base64(raw sha512)`.
- **pypi**: `GET /simple/` + `/simple/{pkg}/` (PEP 503; add PEP 691 JSON), `GET /packages/{group}/{name}/{ver}/{file}`, `POST /upload`. `normalizeName = lower(re.sub([-_.]+,'-'))`. Key `pypi/{group}/{name}/{ver}/{file}`.
- **goproxy** (Laravel, despite name): `/{mod}/@v/list|{v}.info|{v}.mod|{v}.zip`, `/@v/@latest`, `POST /upload`. Case-encode module path (`!x`). **Use golang.org/x/mod/semver** for versions/@latest.

repoType = APP_NAME per service (`php`/`npm`/`pypi`/`go`).

---

## cli — operator tool (→ cobra)

Commands: `health-check`, `login`, `{list,create,delete}-user`, `user-info`,
`{list,create,delete}-package-group`, `{list,create,update,delete}-repository`,
`{add,list,remove}-access-token`, `publish`, `{join,leave}-package-group`,
`{join,leave}-repository`, `{,un}protect-package-group`. Several `request-*` /
`*-repository` protect variants are stubs. Config JSON: `{endpoints,login_token,
user_id}`. Login sends `reporangler-login-*` headers; other calls use
`Authorization: Bearer`.

---

## Bugs to fix in the port (found across the read)

**Security (auth-service):**
- Token + username written to `/tmp/auth-debug.log` every request. Delete.
- `is-package-group-admin` gate is **always true** (`empty()` on an Eloquent Collection). Any user passes.
- Duplicate-user check compares plaintext against bcrypt column — dead; dupes surface as 500 not 422.
- No authz on user read/create/enumerate.
- `access_tokens.token` and cli token stored plaintext.

**Cross-cutting (every facade):**
- CORS preflight `OPTIONS` route declared **outside** the CORS middleware group → preflight returns no CORS headers. Broken everywhere.
- by-id vs by-name collide on one URL pattern (`/user/{x}`, `/package-group/{x}`) — server guesses int/string. Make explicit.
- Whole-blob buffering in storage/zip/tarball — stream instead.
- Naive `version_compare` for `@latest` (go, npm) — real semver.
- Storage path traversal — add containment.
- php-service publish is currently a `MetadataClient` arg-order **TypeError** (broken) — the port fixes it.
- lib-reporangler by-id/by-name client methods build identical URLs; StorageClient swallows all errors to nil/false (do not carry over).

**Dead weight to drop:** every service carries legacy `app/Http/Kernel.php`,
`app/Console/Kernel.php`, `app/Exceptions/Handler.php`, and unused
`config/auth.php` guard drivers — all bypassed by Laravel 12's `bootstrap/app.php`.
