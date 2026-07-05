// Command npm serves the npm-registry protocol (package metadata, tarball
// download, publish) over metadata + storage.
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
	mux.HandleFunc("GET /", httpx.Healthz("npm-service"))

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("npm-service (scaffold) listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
