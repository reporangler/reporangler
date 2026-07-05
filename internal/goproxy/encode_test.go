package goproxy

import "testing"

func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := []struct {
		module  string // canonical module path
		encoded string // proxy/storage encoded form
	}{
		{"rsc.io/quote", "rsc.io/quote"},
		{"github.com/Azure/azure-sdk-for-go", "github.com/!azure/azure-sdk-for-go"},
		{"github.com/BurntSushi/toml", "github.com/!burnt!sushi/toml"},
		{"example.com/UPPER", "example.com/!u!p!p!e!r"},
		{"golang.org/x/mod", "golang.org/x/mod"},
		{"github.com/Masterminds/semver/v3", "github.com/!masterminds/semver/v3"},
	}
	for _, c := range cases {
		if got := encodeModulePath(c.module); got != c.encoded {
			t.Errorf("encodeModulePath(%q) = %q, want %q", c.module, got, c.encoded)
		}
		got, err := decodeModulePath(c.encoded)
		if err != nil {
			t.Errorf("decodeModulePath(%q) error: %v", c.encoded, err)
			continue
		}
		if got != c.module {
			t.Errorf("decodeModulePath(%q) = %q, want %q", c.encoded, got, c.module)
		}
		// Round-trip both directions.
		if rt, err := decodeModulePath(encodeModulePath(c.module)); err != nil || rt != c.module {
			t.Errorf("decode(encode(%q)) = %q, err=%v; want %q", c.module, rt, err, c.module)
		}
	}
}

func TestDecodeModulePathErrors(t *testing.T) {
	bad := []string{
		"github.com/Azure/x", // bare uppercase must not appear encoded
		"github.com/!/x",     // '!' not followed by a-z
		"github.com/foo!",    // trailing '!'
		"github.com/!9/x",    // '!' followed by a digit
	}
	for _, in := range bad {
		if got, err := decodeModulePath(in); err == nil {
			t.Errorf("decodeModulePath(%q) = %q, want error", in, got)
		}
	}
}
