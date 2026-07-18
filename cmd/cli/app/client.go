package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// App carries everything the commands need: the resolved config, an HTTP
// client, and the streams to write to. Keeping this explicit (rather than
// package globals) makes the command tree trivially testable.
type App struct {
	ConfigPath string
	Config     *Config
	HTTP       *http.Client
	Out        io.Writer
	Err        io.Writer
}

// ensureConfig loads the config from disk once, if it has not been supplied
// (tests inject one directly).
func (a *App) ensureConfig() error {
	if a.Config != nil {
		return nil
	}
	if a.ConfigPath == "" {
		p, err := ConfigPath()
		if err != nil {
			return err
		}
		a.ConfigPath = p
	}
	cfg, err := LoadConfig(a.ConfigPath)
	if err != nil {
		return err
	}
	a.Config = &cfg
	return nil
}

func (a *App) httpClient() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// joinURL concatenates a service base URL and a path, tolerating trailing/
// leading slashes.
func joinURL(base, path string) string {
	base = strings.TrimRight(base, "/")
	if path == "" {
		return base + "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// request performs an HTTP request. When body is non-nil it is JSON-encoded.
// When withAuth is true and a login token is present, the bearer header is set;
// health-check passes withAuth=false so it never needs a token. Any 2xx status
// is treated as success. On non-2xx a descriptive error is returned. When out
// is non-nil and the response has a body, it is JSON-decoded into out.
func (a *App) request(ctx context.Context, method, url string, body any, extra map[string]string, withAuth bool, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if withAuth && a.Config != nil && a.Config.LoginToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.Config.LoginToken)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}

	resp, err := a.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: %s: %s", method, url, resp.Status, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(out); err != nil && err != io.EOF {
			return fmt.Errorf("decode response from %s: %w", url, err)
		}
	}
	return nil
}

// printJSON pretty-prints a value to the app's output stream.
func (a *App) printJSON(v any) error {
	enc := json.NewEncoder(a.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
