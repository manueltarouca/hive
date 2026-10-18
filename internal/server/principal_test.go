package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/remote"
)

// A client on the unix socket is known by its peer's user ID, the same
// principal format remote listeners use (peer:UID, tailscale:login…).
func TestLocalClientPrincipalIsPeerUID(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	c := dial(t, h, "principal-test")
	defer c.ws.CloseNow()
	want := fmt.Sprintf("peer:%d", os.Getuid())
	for _, x := range h.call("client.list", nil)["clients"].([]any) {
		cl := x.(map[string]any)
		if cl["name"] == "principal-test" {
			if cl["principal"] != want {
				t.Fatalf("principal = %v, want %s", cl["principal"], want)
			}
			return
		}
	}
	t.Fatal("client not listed")
}

func TestWhoamiLocal(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	out := h.call("whoami", nil)
	if out["principal"] != fmt.Sprintf("peer:%d", os.Getuid()) || out["via"] != "local" {
		t.Fatalf("whoami = %v", out)
	}
}

// A plain TCP listener can't tell callers apart: refused unless the server
// was told to let anonymous callers in.
func TestPlainListenerNeedsAllowAnonymous(t *testing.T) {
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("hive-test-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	srv, err := New(Config{Socket: sock, StateDir: t.TempDir(),
		Remote: &RemoteConfig{Listen: []remote.Spec{{Kind: remote.Plain, Addr: "127.0.0.1:0"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Serve(); err == nil || !strings.Contains(err.Error(), "--allow-anonymous") {
		t.Fatalf("Serve = %v, want a refusal naming --allow-anonymous", err)
	}
}

func TestPlainListenerServesAnonymous(t *testing.T) {
	h := startCfg(t, t.TempDir(), func(c *Config) {
		c.Remote = &RemoteConfig{Listen: []remote.Spec{{Kind: remote.Plain, Addr: "127.0.0.1:0"}},
			Policy: remote.Policy{AllowAnonymous: true}}
	})
	defer h.stop()
	var addr string
	for _, x := range h.call("server.version", nil)["listeners"].([]any) {
		if l := x.(map[string]any); l["kind"] == "plain" {
			addr = l["addr"].(string)
		}
	}
	if addr == "" {
		t.Fatal("plain listener not listed")
	}
	body := strings.NewReader(`{"method":"whoami"}`)
	resp, err := http.Post("http://"+addr+"/v1/call", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Result remote.Identity `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Result.Principal != "anonymous" || out.Result.Via != "plain" {
		t.Fatalf("whoami over plain TCP = %+v", out.Result)
	}
	// A browser page that rebinds a name to this address is refused.
	req, _ := http.NewRequest("GET", "http://"+addr+"/v1/events", nil)
	req.Host = "evil.example"
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 403 {
		t.Fatalf("Host evil.example: %d, want 403", resp2.StatusCode)
	}
}

// Authorization runs before the attach upgrade: a refused caller never gets
// a WebSocket.
func TestAttachRefusedBeforeUpgrade(t *testing.T) {
	h := startCfg(t, t.TempDir(), func(c *Config) {
		c.Remote = &RemoteConfig{Listen: []remote.Spec{{Kind: remote.Plain, Addr: "127.0.0.1:0"}},
			Policy: remote.Policy{AllowAnonymous: true}}
	})
	defer h.stop()
	var addr string
	for _, x := range h.call("server.version", nil)["listeners"].([]any) {
		if l := x.(map[string]any); l["kind"] == "plain" {
			addr = l["addr"].(string)
		}
	}
	req, _ := http.NewRequest("GET", "http://"+addr+"/v1/attach", nil)
	req.Host = "evil.example" // refused: a rebound name
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("attach from a refused caller: %d, want 403 (no upgrade)", resp.StatusCode)
	}
}
