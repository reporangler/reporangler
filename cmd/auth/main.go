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
	"github.com/reporangler/reporangler/internal/middleware"
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
		log.Print("ensured seed admin user")
	}

	tokenLife := 6
	if v := os.Getenv("TOKEN_LIFE_HOURS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tokenLife = n
		}
	}

	h := auth.NewHandler(
		auth.NewService(store, time.Duration(tokenLife)*time.Hour),
		config.Env("METADATA_BASE_URL", ""),
	)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", httpx.Healthz(config.Env("AUTH_BASE_URL", "auth-service")))

	// Unauthenticated: login issues tokens; token introspection reads the
	// Authorization header itself (it is the endpoint every other service calls).
	mux.HandleFunc("GET /login/api", h.Login)
	mux.HandleFunc("GET /login/token", h.CheckToken)

	// Everything below is auth:token. auth-service validates the bearer with its
	// OWN Service.CheckToken (via h.RequireToken) rather than calling authclient,
	// which would loop back to itself.
	mux.HandleFunc("GET /user", h.RequireToken(h.ListUsers))
	mux.HandleFunc("GET /user/{idOrName}", h.RequireToken(h.GetUser))
	mux.HandleFunc("POST /user", h.RequireToken(h.CreateUser))
	mux.HandleFunc("PUT /user/{id}", h.RequireToken(h.UpdateUser))
	mux.HandleFunc("DELETE /user/{id}", h.RequireToken(h.DeleteUser))

	mux.HandleFunc("GET /access-token/{userId}", h.RequireToken(h.ListAccessTokens))
	mux.HandleFunc("POST /access-token/{userId}", h.RequireToken(h.CreateAccessToken))
	mux.HandleFunc("DELETE /access-token/{userId}/{tokenId}", h.RequireToken(h.DeleteAccessToken))

	mux.HandleFunc("PUT /permission/user/admin/{userId}", h.RequireToken(h.GrantAdminPermission))
	mux.HandleFunc("DELETE /permission/user/admin/{userId}", h.RequireToken(h.RevokeAdminPermission))

	mux.HandleFunc("POST /permission/package-group/join", h.RequireToken(h.PackageGroupJoin))
	mux.HandleFunc("POST /permission/package-group/leave", h.RequireToken(h.PackageGroupLeave))
	mux.HandleFunc("POST /permission/package-group/protect", h.RequireToken(h.PackageGroupProtect))
	mux.HandleFunc("POST /permission/package-group/unprotect", h.RequireToken(h.PackageGroupUnprotect))

	// Wrap the whole mux in CORS so preflight OPTIONS gets the CORS headers
	// (the legacy PHP preflight route sat outside the CORS group — a bug fixed
	// here); CORS answers OPTIONS itself.
	cors := middleware.CORS(config.Env("APP_PROTOCOL", "https"), config.Env("APP_DOMAIN", "reporangler.localhost"))

	addr := ":" + config.Env("PORT", "8080")
	log.Printf("auth-service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, cors(mux)))
}
