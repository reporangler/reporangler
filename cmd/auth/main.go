// Command auth is the RepoRangler authentication service: it issues and
// validates opaque bearer login tokens and is the introspection endpoint every
// other service calls. Ported from the legacy Laravel auth-service.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/reporangler/reporangler/internal/auth"
	"github.com/reporangler/reporangler/internal/config"
	"github.com/reporangler/reporangler/internal/httpx"
)

func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, config.MustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	// Wait for Postgres to accept connections (compose start ordering).
	for i := 0; i < 30; i++ {
		if err = pool.Ping(ctx); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Fatalf("db ping: %v", err)
	}

	if err := auth.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	store := auth.NewStore(pool)

	// Seed the admin user from env, mirroring the legacy InitialAdminUser seed.
	if username := os.Getenv("ADMIN_USERNAME"); username != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(config.MustEnv("ADMIN_PASSWORD")), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("hash admin password: %v", err)
		}
		email := config.Env("ADMIN_EMAIL", username+"@reporangler.local")
		if err := store.EnsureAdmin(ctx, username, email, string(hash)); err != nil {
			log.Fatalf("seed admin: %v", err)
		}
		log.Printf("ensured admin user %q", username)
	}

	tokenLife := 6
	if v := os.Getenv("TOKEN_LIFE_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tokenLife = n
		}
	}

	h := auth.NewHandler(auth.NewService(store, time.Duration(tokenLife)*time.Hour))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", httpx.Healthz(config.Env("AUTH_BASE_URL", "auth-service")))
	mux.HandleFunc("GET /login/api", h.Login)
	mux.HandleFunc("GET /login/token", h.CheckToken)

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("auth-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
