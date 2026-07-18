// Command php serves the Composer (PHP) repository protocol over the shared
// metadata + storage services.
//
// It presents the modern Composer v2 read protocol (packages.json + lazy /p2
// metadata) and owns its internals: there is deliberately NO Composer/Satis
// engine and NO VCS scanning. Ingest is a client-push endpoint (POST /publish),
// which replaces the legacy VCS-scan publish by design — Composer has no native
// publish command. See docs/legacy/README.md and internal/php.
package main

import (
	"log"
	"net/http"
	"time"

	"github.com/reporangler/reporangler/internal/authclient"
	"github.com/reporangler/reporangler/internal/config"
	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/middleware"
	"github.com/reporangler/reporangler/internal/php"
)

func main() {
	hc := &http.Client{Timeout: 5 * time.Minute}

	ac := authclient.New(config.MustEnv("AUTH_BASE_URL"), hc)
	h := php.NewHandler(
		config.MustEnv("METADATA_BASE_URL"),
		config.MustEnv("STORAGE_BASE_URL"),
		config.Env("STORAGE_PUBLIC_URL", ""),
		hc,
	)

	repo := middleware.Repo(ac)   // auth:repo — optional, falls back to public
	token := middleware.Token(ac) // auth:token — mandatory bearer

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", httpx.Healthz("php-service"))
	mux.Handle("GET /packages.json", repo(http.HandlerFunc(h.Root)))
	mux.Handle("GET /p2/{name...}", repo(http.HandlerFunc(h.P2)))
	mux.Handle("POST /publish", token(http.HandlerFunc(h.Publish)))

	// CORS wraps the whole mux (and answers OPTIONS preflight — the legacy bug).
	handler := middleware.CORS(config.Env("APP_PROTOCOL", "https"), config.Env("APP_DOMAIN", "reporangler.local"))(mux)

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("php-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}
