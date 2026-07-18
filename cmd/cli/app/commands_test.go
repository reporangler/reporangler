package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// newTestApp returns an App wired to write into buffers, with a config that
// points every endpoint at base and carries the given token.
func newTestApp(t *testing.T, base, token string) (*App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Endpoints = Endpoints{Auth: base, Metadata: base, PHP: base, NPM: base, Storage: base}
	cfg.LoginToken = token
	cfg.UserID = 1
	out, errb := &bytes.Buffer{}, &bytes.Buffer{}
	a := &App{
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Config:     &cfg,
		HTTP:       http.DefaultClient,
		Out:        out,
		Err:        errb,
	}
	return a, out, errb
}

func run(a *App, args ...string) error {
	root := NewRootCmd(a)
	root.SetArgs(args)
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	return root.Execute()
}

func TestHelpDoesNotPanic(t *testing.T) {
	a, _, _ := newTestApp(t, "http://unused", "")
	if err := run(a, "--help"); err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
}

func TestCommandTableComplete(t *testing.T) {
	a, _, _ := newTestApp(t, "http://unused", "")
	root := NewRootCmd(a)
	got := CommandNames(root)
	want := []string{
		"add-access-token",
		"create-package-group",
		"create-repository",
		"create-user",
		"delete-package-group",
		"delete-repository",
		"delete-user",
		"health-check",
		"join-package-group",
		"join-repository",
		"leave-package-group",
		"leave-repository",
		"list-access-token",
		"list-package-group",
		"list-repository",
		"list-user",
		"login",
		"protect-package-group",
		"publish",
		"remove-access-token",
		"unprotect-package-group",
		"update-repository",
		"user-info",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("command table mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestHealthCheckSucceedsWithoutToken(t *testing.T) {
	var sawAuth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = append(sawAuth, r.Header.Get("Authorization"))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	// Empty token on purpose: health-check must not require one.
	a, out, _ := newTestApp(t, srv.URL, "")
	if err := run(a, "health-check"); err != nil {
		t.Fatalf("health-check failed: %v", err)
	}
	if c := strings.Count(out.String(), "OK"); c != 5 {
		t.Errorf("expected 5 OK lines, got %d: %q", c, out.String())
	}
	for _, h := range sawAuth {
		if h != "" {
			t.Errorf("health-check sent an Authorization header: %q", h)
		}
	}
}

func TestHealthCheckReportsFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	a, out, _ := newTestApp(t, srv.URL, "")
	err := run(a, "health-check")
	if err == nil {
		t.Fatal("expected non-nil error (non-zero exit) when services are unhealthy")
	}
	if !strings.Contains(out.String(), "FAIL") {
		t.Errorf("expected FAIL in output, got %q", out.String())
	}
}

func TestLoginStoresTokenAndDoesNotPrintIt(t *testing.T) {
	const secret = "deadbeefdeadbeefdeadbeefdeadbeef"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("reporangler-login-type") != "database" {
			t.Errorf("missing login-type header: %v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":42,"username":"alice","token":"` + secret + `"}`))
	}))
	defer srv.Close()

	a, out, _ := newTestApp(t, srv.URL, "")
	a.Config.LoginToken = "" // start logged out
	if err := run(a, "login", "--username", "alice", "--password", "pw"); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if strings.Contains(out.String(), secret) {
		t.Errorf("login leaked the token to stdout: %q", out.String())
	}
	if !strings.Contains(out.String(), "logged in") {
		t.Errorf("expected a 'logged in' confirmation, got %q", out.String())
	}
	// Token + user id must be persisted to the config file.
	saved, err := LoadConfig(a.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if saved.LoginToken != secret || saved.UserID != 42 {
		t.Errorf("config not persisted: token=%q user=%d", saved.LoginToken, saved.UserID)
	}
}

func TestAuthenticatedRequestSendsBearer(t *testing.T) {
	const token = "tok-abc"
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"count":0,"data":[]}`))
	}))
	defer srv.Close()

	a, _, _ := newTestApp(t, srv.URL, token)
	if err := run(a, "list-user"); err != nil {
		t.Fatalf("list-user failed: %v", err)
	}
	if gotAuth != "Bearer "+token {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer "+token)
	}
}

func TestNon200TwoxxTreatedAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 201 Created with no body — must count as success.
		w.WriteHeader(201)
	}))
	defer srv.Close()

	a, _, _ := newTestApp(t, srv.URL, "tok")
	if err := run(a, "create-repository", "--name", "widgets"); err != nil {
		t.Errorf("2xx status should be success, got error: %v", err)
	}
}

func TestErrorStatusReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":403,"message":"nope"}`, 403)
	}))
	defer srv.Close()

	a, _, _ := newTestApp(t, srv.URL, "tok")
	err := run(a, "delete-user", "--id", "5")
	if err == nil {
		t.Fatal("expected error on 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention status, got %v", err)
	}
}

func TestPublishUnknownRepoErrors(t *testing.T) {
	a, _, _ := newTestApp(t, "http://unused", "tok")
	err := run(a, "publish", "--repo", "pypi", "--package-group", "g", "--url", "http://x")
	if err == nil || !strings.Contains(err.Error(), "unknown --repo") {
		t.Errorf("expected unknown-repo error, got %v", err)
	}
}
