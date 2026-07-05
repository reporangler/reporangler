// Command pypi serves the PyPI simple-index protocol (PEP 503/691) + download
// + upload over metadata + storage.
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
	mux.HandleFunc("GET /", httpx.Healthz("pypi-service"))

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("pypi-service (scaffold) listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
