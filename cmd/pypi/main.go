// Command pypi serves the PyPI "simple" repository protocol (PEP 503) plus
// distribution download and Twine-style upload, over metadata + storage.
package main

import (
	"log"
	"net/http"

	"github.com/reporangler/reporangler/internal/authclient"
	"github.com/reporangler/reporangler/internal/config"
	"github.com/reporangler/reporangler/internal/pypi"
)

func main() {
	srv := pypi.New(pypi.Config{
		Protocol:         config.Env("APP_PROTOCOL", "https"),
		Domain:           config.Env("APP_DOMAIN", "reporangler.localhost"),
		MetadataBaseURL:  config.MustEnv("METADATA_BASE_URL"),
		StorageBaseURL:   config.MustEnv("STORAGE_BASE_URL"),
		StoragePublicURL: config.Env("STORAGE_PUBLIC_URL", ""),
		Auth:             authclient.New(config.MustEnv("AUTH_BASE_URL"), http.DefaultClient),
		HTTP:             http.DefaultClient,
	})

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("pypi-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}
