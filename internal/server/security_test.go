package server

import (
	"context"
	"errors"
	"maps"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/manueltarouca/hive/internal/client"
)

func rawHTTP(t *testing.T, h *harness) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", h.sock)
	}}}
}

// S1: no route may be driven from a web page, even once hive listens on a
// network: browsers send an Origin, and a "simple" CORS post can't set a
// JSON content type.
func TestBrowserRequestsAreRefused(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	hc := rawHTTP(t, h)
	cases := []struct {
		name, method, path, ctype, origin string
		want                              int
	}{
		{"call with origin", "POST", "/v1/call", "application/json", "https://evil.example", 403},
		{"call as text/plain", "POST", "/v1/call", "text/plain", "", 415},
		{"events with origin", "GET", "/v1/events", "", "https://evil.example", 403},
		{"call ok", "POST", "/v1/call", "application/json", "", 200},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, "http://hive"+c.path, strings.NewReader(`{"method":"server.version"}`))
		if c.ctype != "" {
			req.Header.Set("Content-Type", c.ctype)
		}
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		resp, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.want)
		}
	}
}

// S3: what a hook reports ends up in a command line at the next restore.
func TestAgentReportIsValidated(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	bid := strs(created["block_ids"])[0]
	bad := []map[string]any{
		{"session_id": "--dangerously-skip-permissions"},
		{"session_id": "abc def"},
		{"session_id": strings.Repeat("a", 200)},
		{"cwd": "relative/path"},
	}
	for _, b := range bad {
		p := map[string]any{"block_id": bid, "harness": "claude", "state": "idle"}
		maps.Copy(p, b)
		var e *client.Error
		if err := h.c.Call(context.Background(), "agent.report", p, nil); !errors.As(err, &e) || e.Code != "bad_request" {
			t.Errorf("%v: err = %v", b, err)
		}
	}
	// Long messages are kept, but cut.
	out := h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "blocked",
		"session_id": "6f0c-11ee.b", "message": strings.Repeat("x", 5000)})
	if len(out["message"].(string)) > 500 {
		t.Fatalf("message not capped: %d", len(out["message"].(string)))
	}
}

// S5: the socket is never reachable by others, not even briefly.
func TestSocketIsOwnerOnly(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	fi, err := os.Stat(h.sock)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("socket mode %v", fi.Mode().Perm())
	}
}
