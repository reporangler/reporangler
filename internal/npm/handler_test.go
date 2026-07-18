package npm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/reporangler/reporangler/internal/authclient"
)

func testHandler() *Handler {
	return NewHandler(Config{
		Auth:            authclient.New("http://auth.invalid", nil),
		MetadataBaseURL: "http://metadata.invalid",
		StorageBaseURL:  "http://storage.invalid",
		Protocol:        "https",
	})
}

// TestRoutesRegister ensures the mux registers without a pattern conflict
// (notably GET /{$} alongside the GET /{package...} catch-all).
func TestRoutesRegister(t *testing.T) {
	if h := testHandler().Routes(); h == nil {
		t.Fatal("Routes() returned nil")
	}
}

func TestHealthzRoot(t *testing.T) {
	srv := httptest.NewServer(testHandler().Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["service"] != "npm-service" {
		t.Errorf("healthz service = %v, want npm-service", body["service"])
	}
}

func TestAuditStub(t *testing.T) {
	srv := httptest.NewServer(testHandler().Routes())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/-/npm/v1/security/audits", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Actions     []any  `json:"actions"`
		Advisories  []any  `json:"advisories"`
		MoreInfoURL string `json:"moreInfoUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Actions == nil || body.Advisories == nil {
		t.Errorf("audit arrays should be non-null: %+v", body)
	}
}
