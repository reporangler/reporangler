// Command goproxy serves the GOPROXY protocol (/@v/list, .info, .mod, .zip,
// /@latest) over metadata + storage. Versions use golang.org/x/mod/semver
// (the port fixes the legacy naive version_compare @latest bug).
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
	mux.HandleFunc("GET /", httpx.Healthz("go-service"))

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("go-service (scaffold) listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
