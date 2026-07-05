// Package httpx holds tiny HTTP response helpers shared across services.
//
// The error envelope {"code":<int>,"message":<string>} mirrors the shape the
// legacy Laravel services returned from bootstrap/app.php, so existing clients
// (and lib-reporangler's error parsing) keep working.
package httpx

import (
	"encoding/json"
	"net/http"
)

// JSON writes v as a JSON response with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes the standard {code,message} error envelope.
func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]any{"code": status, "message": msg})
}

// Healthz returns a handler that reports {"statusCode":200,"service":<name>},
// matching the legacy DefaultController@healthz body.
func Healthz(service string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]any{"statusCode": 200, "service": service})
	}
}
