// Command metadata is the RepoRangler package-index service: the Postgres-backed
// system of record for packages, package-groups and repositories. Every route
// requires a bearer token (auth:token) except the healthz probe; mutating
// package-group/repository routes additionally require an admin user. Ported
// from the legacy Laravel metadata-service.
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/reporangler/reporangler/internal/authclient"
	"github.com/reporangler/reporangler/internal/config"
	"github.com/reporangler/reporangler/internal/metadata"
	"github.com/reporangler/reporangler/internal/middleware"
	"github.com/reporangler/reporangler/internal/pg"
)

func main() {
	ctx := context.Background()

	pool, err := pg.Connect(ctx, config.MustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err := pg.Migrate(ctx, pool, metadata.MigrationFS); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	h := metadata.NewHandler(metadata.NewStore(pool))
	ac := authclient.New(config.MustEnv("AUTH_BASE_URL"), nil)
	tok := middleware.Token(ac) // mandatory bearer (mutations)
	repo := middleware.Repo(ac) // optional, public fallback (reads)

	mux := metadata.Routes(h, tok, repo)
	handler := middleware.CORS(config.Env("APP_PROTOCOL", "https"), config.Env("APP_DOMAIN", "reporangler.localhost"))(mux)

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("metadata-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}
