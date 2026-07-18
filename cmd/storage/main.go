// Command storage is the RepoRangler blob store: a filesystem-backed object
// store keyed by arbitrary slash-containing keys. Ported from the legacy
// Laravel storage-service, streaming every transfer and containing all paths
// within the storage root.
package main

import (
	"log"
	"net/http"

	"github.com/reporangler/reporangler/internal/authclient"
	"github.com/reporangler/reporangler/internal/config"
	"github.com/reporangler/reporangler/internal/httpx"
	"github.com/reporangler/reporangler/internal/middleware"
	"github.com/reporangler/reporangler/internal/storage"
)

func main() {
	store, err := storage.NewStore(config.Env("STORAGE_PATH", "/data/packages"))
	if err != nil {
		log.Fatalf("storage root: %v", err)
	}
	log.Printf("storage root: %s", store.Root())

	ac := authclient.New(config.MustEnv("AUTH_BASE_URL"), nil)
	token := middleware.Token(ac)
	repo := middleware.Repo(ac)

	h := storage.NewHandler(store)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", httpx.Healthz("storage-service"))
	mux.Handle("GET /object/{key...}", repo(http.HandlerFunc(h.Get)))
	mux.HandleFunc("GET /object-exists/{key...}", h.Exists)
	mux.HandleFunc("GET /objects", h.List)
	mux.Handle("PUT /object/{key...}", token(http.HandlerFunc(h.Put)))
	mux.Handle("DELETE /object/{key...}", token(http.HandlerFunc(h.Delete)))

	handler := middleware.CORS(config.Env("APP_PROTOCOL", "https"), config.Env("APP_DOMAIN", "reporangler.localhost"))(mux)

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("storage-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}
