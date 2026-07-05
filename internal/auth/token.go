package auth

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// GenerateToken returns a 64-char hex string from 32 random bytes,
// matching the legacy PHP LoginToken (bin2hex(random_bytes(32))).
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// StripBearer removes a leading, case-insensitive "Bearer " prefix and trims
// surrounding whitespace, matching AuthClient::check() in lib-reporangler.
func StripBearer(header string) string {
	header = strings.TrimSpace(header)
	if len(header) >= 7 && strings.EqualFold(header[:7], "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return header
}
