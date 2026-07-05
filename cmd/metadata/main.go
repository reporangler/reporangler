// Command metadata is the RepoRangler package-index service (Postgres-backed
// system of record for packages, package-groups, repositories).
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
	mux.HandleFunc("GET /", httpx.Healthz("metadata-service"))

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("metadata-service (scaffold) listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
