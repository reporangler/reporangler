// Command storage is the RepoRangler blob store (put/get/delete objects by key).
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
	mux.HandleFunc("GET /", httpx.Healthz("storage-service"))

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("storage-service (scaffold) listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
