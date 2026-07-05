package auth

import "testing"

func TestGenerateToken(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(a) != 64 {
		t.Fatalf("want 64 hex chars, got %d (%q)", len(a), a)
	}
	b, _ := GenerateToken()
	if a == b {
		t.Fatal("consecutive tokens should differ")
	}
}

func TestStripBearer(t *testing.T) {
	cases := map[string]string{
		"Bearer abc":   "abc",
		"bearer abc":   "abc",
		"abc":          "abc",
		"  Bearer  x ": "x",
		"":             "",
	}
	for in, want := range cases {
		if got := StripBearer(in); got != want {
			t.Errorf("StripBearer(%q) = %q, want %q", in, got, want)
		}
	}
}
