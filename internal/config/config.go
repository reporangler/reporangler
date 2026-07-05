// Package config provides small env-var helpers shared by every service.
//
// The legacy PHP services fail fast at boot when a required env var is unset
// (config/app.php threw). MustEnv preserves that behaviour.
package config

import "os"

// Env returns the value of key, or def if unset/empty.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// MustEnv returns the value of key or panics if it is unset/empty.
func MustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("missing required env var: " + key)
	}
	return v
}
