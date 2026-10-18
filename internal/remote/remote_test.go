package remote

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseSpec(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Spec
		err  bool
	}{
		{"tailnet-serve:///run/hive/serve.sock", Spec{Kind: TailnetServe, Path: "/run/hive/serve.sock"}, false},
		{"tailnet-serve://relative.sock", Spec{}, true},
		{"tailnet://:4483", Spec{Kind: Tailnet, Addr: ":4483"}, false},
		{"tailnet://", Spec{}, true},
		{":4483", Spec{Kind: Plain, Addr: ":4483"}, false},
		{"10.0.0.2:4483", Spec{Kind: Plain, Addr: "10.0.0.2:4483"}, false},
		{"4483", Spec{}, true},
		{"https://x:1", Spec{}, true},
		{":99999", Spec{}, true},
	} {
		got, err := ParseSpec(c.in)
		if (err != nil) != c.err || (!c.err && got != c.want) {
			t.Errorf("ParseSpec(%q) = %+v, %v", c.in, got, err)
		}
	}
}

func TestAllowed(t *testing.T) {
	owner := "alice@github"
	alice := Peer{Login: "alice@github"}
	bob := Peer{Login: "bob@github"}
	ci := Peer{Tags: []string{"tag:ci"}}
	anon := Peer{}
	for _, c := range []struct {
		name  string
		kind  Kind
		pol   Policy
		peer  Peer
		owner string
		want  bool
	}{
		{"owner, serve", TailnetServe, Policy{}, alice, owner, true},
		{"owner, case", Tailnet, Policy{}, Peer{Login: "Alice@GitHub"}, owner, true},
		{"other user", Tailnet, Policy{}, bob, owner, false},
		{"other user, identified", Tailnet, Policy{AllowIdentified: true}, bob, owner, true},
		{"other user, members flag is tsnet's", Tailnet, Policy{AllowMembers: true}, bob, owner, false},
		{"other user, tsnet members", Tailscale, Policy{AllowMembers: true}, bob, owner, true},
		{"identified flag isn't tsnet's", Tailscale, Policy{AllowIdentified: true}, bob, owner, false},
		{"tagged peer is never the owner", Tailnet, Policy{}, ci, owner, false},
		{"tagged peer, identified", Tailnet, Policy{AllowIdentified: true}, ci, owner, true},
		{"tagged self has no owner", Tailscale, Policy{}, alice, "", false},
		{"tagged self, members", Tailscale, Policy{AllowMembers: true}, alice, "", true},
		{"anonymous on serve, even identified", TailnetServe, Policy{AllowIdentified: true, AllowAnonymous: true}, anon, owner, false},
		{"anonymous on tailnet", Tailnet, Policy{AllowAnonymous: true}, anon, owner, false},
		{"plain without the flag", Plain, Policy{}, anon, "", false},
		{"plain with it", Plain, Policy{AllowAnonymous: true}, anon, "", true},
		{"local", Local, Policy{}, anon, "", true},
	} {
		if got := c.pol.Allowed(c.kind, c.peer, c.owner); got != c.want {
			t.Errorf("%s: Allowed = %v, want %v", c.name, got, c.want)
		}
	}
}

// fakeWho answers WhoIs from a table keyed by the caller's port.
type fakeWho struct {
	mu    sync.Mutex
	peers map[string]Peer // remote host:port → peer; missing → error
	owner string
	names []string
	whoIs int
}

func (f *fakeWho) WhoIs(_ context.Context, addr string) (Peer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.whoIs++
	for k, p := range f.peers {
		if k == "*" || k == addr {
			return p, nil
		}
	}
	return Peer{}, errors.New("no such peer")
}
func (f *fakeWho) Owner(context.Context) (string, error)   { return f.owner, nil }
func (f *fakeWho) Names(context.Context) ([]string, error) { return f.names, nil }

// serve runs Authorize in front of a handler that reports the identity.
func serve(t *testing.T, kind Kind, network string, who Who, pol Policy) (addr string, denied *[]string) {
	t.Helper()
	var ln net.Listener
	var err error
	if network == "unix" {
		addr = t.TempDir() + "/s.sock"
		ln, err = net.Listen("unix", addr)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		addr = ""
	}
	if err != nil {
		t.Fatal(err)
	}
	if addr == "" {
		addr = ln.Addr().String()
	}
	var mu sync.Mutex
	var d []string
	h := Authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, IdentityOf(r.Context()).Principal)
	}), func(r *http.Request, id Identity, why string) {
		mu.Lock()
		d = append(d, id.Principal+": "+why)
		mu.Unlock()
	})
	srv := &http.Server{Handler: h, ConnContext: ConnContext}
	go srv.Serve(&Listener{Listener: ln, Kind: kind, Who: who, Policy: pol})
	t.Cleanup(func() { srv.Close() })
	return addr, &d
}

func client(network, addr string) *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		}}}
}

func get(t *testing.T, c *http.Client, host string, hdr map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://"+host+"/v1/events", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(b))
}

// tailscaled's Serve is a pooled reverse proxy: one keep-alive connection
// carries requests from different users. Identity has to be per request.
func TestServeIdentityIsPerRequest(t *testing.T) {
	who := &fakeWho{owner: "alice@github"}
	addr, _ := serve(t, TailnetServe, "unix", who, Policy{})
	c := client("unix", addr)
	if code, body := get(t, c, "hive", map[string]string{"Tailscale-User-Login": "alice@github"}); code != 200 || body != "tailscale:alice@github" {
		t.Fatalf("owner: %d %s", code, body)
	}
	// The same connection (keep-alive), another user.
	if code, _ := get(t, c, "hive", map[string]string{"Tailscale-User-Login": "bob@github"}); code != 403 {
		t.Fatalf("bob on alice's connection: %d, want 403", code)
	}
	if code, _ := get(t, c, "hive", nil); code != 403 {
		t.Fatalf("no header (a tagged device, or not through Serve): %d, want 403", code)
	}
	if code, _ := get(t, c, "hive", map[string]string{"Tailscale-User-Login": "alice@github", "Tailscale-Funnel-Request": "?1"}); code != 403 {
		t.Fatalf("funnel: %d, want 403", code)
	}
}

func TestAnonymousNeverOnServe(t *testing.T) {
	who := &fakeWho{owner: "alice@github"}
	addr, _ := serve(t, TailnetServe, "unix", who, Policy{AllowAnonymous: true, AllowIdentified: true})
	if code, _ := get(t, client("unix", addr), "hive", nil); code != 403 {
		t.Fatalf("anonymous on the Serve socket with every flag: %d, want 403", code)
	}
}

// Identity headers mean nothing anywhere but the Serve socket.
func TestHeadersIgnoredOffServe(t *testing.T) {
	who := &fakeWho{owner: "alice@github", names: []string{"127.0.0.1", "box.example.ts.net"},
		peers: map[string]Peer{"*": {Login: "bob@github"}}}
	addr, _ := serve(t, Tailnet, "tcp", who, Policy{})
	if code, _ := get(t, client("tcp", addr), "box.example.ts.net", map[string]string{"Tailscale-User-Login": "alice@github"}); code != 403 {
		t.Fatalf("bob claiming to be alice on a tailnet listener: %d, want 403", code)
	}
	pAddr, _ := serve(t, Plain, "tcp", nil, Policy{AllowAnonymous: true})
	if code, body := get(t, client("tcp", pAddr), "127.0.0.1", map[string]string{"Tailscale-User-Login": "alice@github"}); code != 200 || body != "anonymous" {
		t.Fatalf("a header on a plain listener: %d %s, want 200 anonymous", code, body)
	}
}

func TestTailnetWhoIsOncePerConnection(t *testing.T) {
	who := &fakeWho{owner: "alice@github", names: []string{"box.example.ts.net"},
		peers: map[string]Peer{"*": {Login: "alice@github"}}}
	addr, _ := serve(t, Tailnet, "tcp", who, Policy{})
	c := client("tcp", addr)
	for range 3 {
		if code, body := get(t, c, "box.example.ts.net", nil); code != 200 || body != "tailscale:alice@github" {
			t.Fatalf("owner: %d %s", code, body)
		}
	}
	if who.whoIs != 1 {
		t.Fatalf("WhoIs ran %d times on one connection, want 1", who.whoIs)
	}
}

func TestTaggedPeerIsNotTheOwner(t *testing.T) {
	who := &fakeWho{owner: "alice@github", names: []string{"box.example.ts.net"},
		peers: map[string]Peer{"*": {Login: "alice@github", Tags: []string{"tag:ci"}}}}
	addr, _ := serve(t, Tailnet, "tcp", who, Policy{})
	if code, _ := get(t, client("tcp", addr), "box.example.ts.net", nil); code != 403 {
		t.Fatalf("a tagged peer (sharing the owner's login): %d, want 403", code)
	}
}

func TestUnknownPeerDenied(t *testing.T) {
	who := &fakeWho{owner: "alice@github", names: []string{"box.example.ts.net"}}
	addr, _ := serve(t, Tailnet, "tcp", who, Policy{AllowIdentified: true})
	if code, _ := get(t, client("tcp", addr), "box.example.ts.net", nil); code != 403 {
		t.Fatalf("WhoIs failed: %d, want 403", code)
	}
}

// After DNS rebinding, a page's requests reach the server's address but
// carry the attacker's Host, and browsers mark them with Sec-Fetch-*.
func TestRebindingRefused(t *testing.T) {
	who := &fakeWho{owner: "alice@github", names: []string{"100.64.0.1", "box.example.ts.net"},
		peers: map[string]Peer{"*": {Login: "alice@github"}}}
	addr, denied := serve(t, Tailnet, "tcp", who, Policy{})
	c := client("tcp", addr)
	if code, _ := get(t, c, "evil.example:4483", nil); code != 403 {
		t.Fatalf("Host evil.example: %d, want 403", code)
	}
	if code, _ := get(t, c, "box.example.ts.net:4483", map[string]string{"Sec-Fetch-Site": "same-origin"}); code != 403 {
		t.Fatalf("Sec-Fetch-Site: %d, want 403", code)
	}
	if code, _ := get(t, c, "100.64.0.1:4483", nil); code != 200 {
		t.Fatalf("the node's own address: %d, want 200", code)
	}
	if code, _ := get(t, c, "BOX.example.ts.net.", nil); code != 200 {
		t.Fatalf("its name, any case, trailing dot: %d, want 200", code)
	}
	pAddr, _ := serve(t, Plain, "tcp", nil, Policy{AllowAnonymous: true})
	pc := client("tcp", pAddr)
	if code, _ := get(t, pc, "evil.example", nil); code != 403 {
		t.Fatalf("plain, Host evil.example: %d, want 403", code)
	}
	if code, body := get(t, pc, pAddr, nil); code != 200 || body != "anonymous" {
		t.Fatalf("plain, an IP: %d %s", code, body)
	}
	if len(*denied) == 0 {
		t.Fatal("denials weren't logged")
	}
}

// Built-in Tailscale's plain HTTP port is a tailnet address too.
func TestRebindingRefusedOnBuiltInTailscale(t *testing.T) {
	who := &fakeWho{owner: "alice@github", names: []string{"100.64.0.5", "hive.example.ts.net"},
		peers: map[string]Peer{"*": {Login: "alice@github"}}}
	addr, _ := serve(t, Tailscale, "tcp", who, Policy{})
	c := client("tcp", addr)
	if code, _ := get(t, c, "evil.example:4483", nil); code != 403 {
		t.Fatalf("Host evil.example: %d, want 403", code)
	}
	if code, _ := get(t, c, "hive.example.ts.net:4483", nil); code != 200 {
		t.Fatalf("its own name: %d, want 200", code)
	}
}

// A connection that didn't come through a vetted listener or the local
// socket is refused, not waved through as local.
func TestUnwrappedListenerRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := Authorize(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "in") }), nil)
	srv := &http.Server{Handler: h, ConnContext: ConnContext}
	go srv.Serve(ln)
	defer srv.Close()
	if code, _ := get(t, client("tcp", ln.Addr().String()), ln.Addr().String(), nil); code != 403 {
		t.Fatalf("an unwrapped TCP listener: %d, want 403", code)
	}
}

func TestServeLoginDecoded(t *testing.T) {
	who := &fakeWho{owner: "joão@github"}
	addr, _ := serve(t, TailnetServe, "unix", who, Policy{})
	// Serve sends non-ASCII logins RFC 2047-encoded.
	if code, body := get(t, client("unix", addr), "hive", map[string]string{"Tailscale-User-Login": "=?utf-8?q?jo=C3=A3o@github?="}); code != 200 || body != "tailscale:joão@github" {
		t.Fatalf("encoded owner login: %d %s", code, body)
	}
}
