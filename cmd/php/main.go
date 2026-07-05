// Command php serves the Composer/Satis repository protocol (packages.json +
// /p2 metadata) over metadata + storage. No Composer/Satis engine — we present
// the external read protocol and own the internals.
//
// Scaffold: healthz only. See docs/legacy/README.md for the target contract.
package main

import (
	"log"
	"net/http"

	"github.com/reporangler/reporangler/internal/config"
	"github.com/reporangler/reporangler/internal/httpx"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", httpx.Healthz("php-service"))

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("php-service (scaffold) listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
