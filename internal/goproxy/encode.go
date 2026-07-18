package goproxy

import (
	"fmt"
	"strings"
)

// The Go module proxy protocol case-encodes module paths so that they survive
// case-insensitive filesystems: every uppercase letter is written as "!"
// followed by its lowercase form (e.g. github.com/Azure -> github.com/!azure).
// Requests arrive encoded; we decode to recover the canonical module path used
// as the metadata name, and encode again when laying out storage keys.

// decodeModulePath converts a proxy-encoded module path back to its canonical
// form: "!" followed by a lowercase letter becomes the uppercase letter. A
// bare uppercase letter (which should never appear in an encoded path) or a
// dangling/invalid escape is an error.
func decodeModulePath(enc string) (string, error) {
	var b strings.Builder
	b.Grow(len(enc))
	for i := 0; i < len(enc); i++ {
		c := enc[i]
		switch {
		case c == '!':
			i++
			if i >= len(enc) {
				return "", fmt.Errorf("invalid module path %q: trailing %q", enc, "!")
			}
			r := enc[i]
			if r < 'a' || r > 'z' {
				return "", fmt.Errorf("invalid module path %q: bad escape !%c", enc, r)
			}
			b.WriteByte(r - 'a' + 'A')
		case c >= 'A' && c <= 'Z':
			return "", fmt.Errorf("invalid module path %q: unexpected uppercase %c", enc, c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

// encodeModulePath encodes a canonical module path for proxy/storage use:
// every uppercase letter becomes "!" + its lowercase form. All other bytes
// (lowercase, digits, '/', '.', '-', '_') are left untouched.
func encodeModulePath(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c >= 'A' && c <= 'Z' {
			b.WriteByte('!')
			b.WriteByte(c - 'A' + 'a')
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
