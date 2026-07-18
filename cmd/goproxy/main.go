// Command goproxy serves the GOPROXY protocol (/@v/list, .info, .mod, .zip,
// /@latest) + a client-push /upload, over metadata + storage. Versions use
// golang.org/x/mod/semver (fixes the legacy naive version_compare @latest bug).
package main

import (
	"log"
	"net/http"

	"github.com/reporangler/reporangler/internal/authclient"
	"github.com/reporangler/reporangler/internal/config"
	"github.com/reporangler/reporangler/internal/goproxy"
)

func main() {
	ac := authclient.New(config.MustEnv("AUTH_BASE_URL"), nil)

	srv := goproxy.New(goproxy.Config{
		RepoType:      "go",
		MetadataBase:  config.MustEnv("METADATA_BASE_URL"),
		StorageBase:   config.MustEnv("STORAGE_BASE_URL"),
		StoragePublic: config.Env("STORAGE_PUBLIC_URL", ""),
	}, ac)

	handler := srv.Handler(
		config.Env("APP_PROTOCOL", "http"),
		config.Env("APP_DOMAIN", "reporangler.localhost"),
	)

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("go-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}
