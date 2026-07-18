// Command npm serves the npm-registry protocol (package metadata, tarball
// download, publish, /-/all index) as a stateless facade over metadata-service
// and storage-service. Ported from the legacy Laravel npm-service.
package main

import (
	"log"
	"net/http"

	"github.com/reporangler/reporangler/internal/authclient"
	"github.com/reporangler/reporangler/internal/config"
	"github.com/reporangler/reporangler/internal/middleware"
	"github.com/reporangler/reporangler/internal/npm"
)

func main() {
	protocol := config.Env("APP_PROTOCOL", "https")
	domain := config.Env("APP_DOMAIN", "reporangler.localhost")

	ac := authclient.New(config.MustEnv("AUTH_BASE_URL"), nil)

	h := npm.NewHandler(npm.Config{
		Auth:             ac,
		MetadataBaseURL:  config.MustEnv("METADATA_BASE_URL"),
		StorageBaseURL:   config.MustEnv("STORAGE_BASE_URL"),
		StoragePublicURL: config.Env("STORAGE_PUBLIC_URL", ""),
		Protocol:         protocol,
	})

	// Wrap the whole mux in CORS so OPTIONS preflight also carries CORS headers
	// (the legacy PHP declared its preflight route outside the CORS group).
	handler := middleware.CORS(protocol, domain)(h.Routes())

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("npm-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}
