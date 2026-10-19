package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/manueltarouca/hive/internal/vt"
)

type wsClient struct {
	t  *testing.T
	ws *websocket.Conn
	id string
}

func dial(t *testing.T, h *harness, name string) *wsClient {
	t.Helper()
	hc := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", h.sock)
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws://hive/v1/attach", &websocket.DialOptions{HTTPClient: hc})
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(64 << 20)
	c := &wsClient{t: t, ws: ws}
	c.send(map[string]any{"type": "hello", "name": name, "kind": "test"})
	m := c.nextJSON()
	if m["type"] != "welcome" {
		t.Fatalf("welcome = %v", m)
	}
	c.id = m["client_id"].(string)
	return c
}

func (c *wsClient) send(v any) {
	b, _ := json.Marshal(v)
	if err := c.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		c.t.Fatal(err)
	}
}

func (c *wsClient) next() (websocket.MessageType, []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, b, err := c.ws.Read(ctx)
	if err != nil {
		c.t.Fatal(err)
	}
	return typ, b
}

func (c *wsClient) nextJSON() map[string]any {
	for {
		typ, b := c.next()
		if typ == websocket.MessageText {
			var m map[string]any
			json.Unmarshal(b, &m)
			return m
		}
	}
}

func TestAttachMirrorsTerminal(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}, "cols": 80, "rows": 24})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.call("hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{"data": "early\r"}})
	h.waitScreen(sid, bid, "early")

	c := dial(t, h, "app")
	defer c.ws.CloseNow()
	c.send(map[string]any{"type": "subscribe", "block_id": bid})
	sub := c.nextJSON()
	if sub["type"] != "subscribed" {
		t.Fatalf("subscribed = %v", sub)
	}
	seq := uint64(sub["seq"].(float64))

	typ, b := c.next()
	kind, fb, fseq, snap, err := ParseFrame(b)
	if typ != websocket.MessageBinary || err != nil || kind != frameSnapshot || fb != bid || fseq != seq {
		t.Fatalf("snapshot frame: %v %v %v %v", kind, fb, fseq, err)
	}
	mirror, err := vt.Restore(snap, vt.Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer mirror.Close()

	// Input over the socket; output frames continue exactly at seq.
	c.send(map[string]any{"type": "input", "block_id": bid, "data": "late"})
	c.send(map[string]any{"type": "key", "block_id": bid, "key": "enter"})
	next := seq
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		typ, b := c.next()
		if typ != websocket.MessageBinary {
			continue
		}
		kind, _, fseq, data, _ := ParseFrame(b)
		if kind != frameOutput {
			continue
		}
		if fseq != next {
			t.Fatalf("gap: %d != %d", fseq, next)
		}
		next += uint64(len(data))
		mirror.Feed(data)
		got, _ := mirror.Format(vt.FormatOpts{Trim: true})
		if strings.Count(got, "late") >= 2 {
			want := h.screen(sid, bid)
			if got != want {
				t.Fatalf("mirror differs:\n%q\nserver:\n%q", got, want)
			}
			return
		}
	}
	t.Fatal("never saw the echo")
}

func TestAttachSizeAuthorityAndCalls(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	bid := strs(created["block_ids"])[0]

	a, b := dial(t, h, "mac"), dial(t, h, "phone")
	defer a.ws.CloseNow()
	defer b.ws.CloseNow()
	list := h.call("client.list", nil)["clients"].([]any)
	if len(list) != 2 {
		t.Fatalf("clients = %v", list)
	}

	a.send(map[string]any{"type": "resize", "block_id": bid, "cols": 100, "rows": 30})
	// Wait for the mac's resize to land: each connection is handled in order.
	a.send(map[string]any{"type": "call", "id": 1, "method": "server.version"})
	a.nextJSON()
	b.send(map[string]any{"type": "resize", "block_id": bid, "cols": 50, "rows": 20})
	if m := b.nextJSON(); m["type"] != "error" || !strings.Contains(m["error"].(map[string]any)["code"].(string), "conflict") {
		t.Fatalf("phone resize = %v", m)
	}
	size := h.call("hive.terminal.size", map[string]any{"block_id": bid})
	if size["cols"].(float64) != 100 || size["size_owner"] != a.id {
		t.Fatalf("size = %v", size)
	}
	b.send(map[string]any{"type": "resize", "block_id": bid, "cols": 50, "rows": 20, "take": true})
	b.send(map[string]any{"type": "call", "id": 9, "method": "hive.terminal.size", "params": map[string]any{"block_id": bid}})
	res := b.nextJSON()
	if res["type"] != "result" || res["id"].(float64) != 9 || res["result"].(map[string]any)["cols"].(float64) != 50 {
		t.Fatalf("call = %v", res)
	}

	// When the owner leaves, authority is released.
	b.ws.Close(websocket.StatusNormalClosure, "")
	time.Sleep(200 * time.Millisecond)
	a.send(map[string]any{"type": "resize", "block_id": bid, "cols": 90, "rows": 30})
	a.send(map[string]any{"type": "call", "id": 1, "method": "hive.terminal.size", "params": map[string]any{"block_id": bid}})
	if res := a.nextJSON(); res["result"].(map[string]any)["cols"].(float64) != 90 {
		t.Fatalf("after owner left = %v", res)
	}
}

// Giving a size back: the holder sets the size it found and lets go, so
// the device in use picks the pane up again on its own.
func TestAttachResizeRelease(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	bid := strs(created["block_ids"])[0]
	a, b := dial(t, h, "mac"), dial(t, h, "phone")
	defer a.ws.CloseNow()
	defer b.ws.CloseNow()
	a.send(map[string]any{"type": "resize", "block_id": bid, "cols": 100, "rows": 30})
	a.send(map[string]any{"type": "call", "id": 1, "method": "server.version"})
	a.nextJSON()
	// Someone else's pane can't be released.
	b.send(map[string]any{"type": "resize", "block_id": bid, "cols": 50, "rows": 20, "release": true})
	if m := b.nextJSON(); m["type"] != "error" {
		t.Fatalf("a release of the mac's pane = %v", m)
	}
	b.send(map[string]any{"type": "resize", "block_id": bid, "cols": 50, "rows": 20, "take": true})
	b.send(map[string]any{"type": "resize", "block_id": bid, "cols": 100, "rows": 30, "release": true})
	b.send(map[string]any{"type": "call", "id": 2, "method": "hive.terminal.size", "params": map[string]any{"block_id": bid}})
	res := b.nextJSON()["result"].(map[string]any)
	if res["cols"].(float64) != 100 || res["size_owner"] != "" {
		t.Fatalf("after release = %v, want 100 columns and no owner", res)
	}
	// Nobody holds it: the mac sizes it without taking.
	a.send(map[string]any{"type": "resize", "block_id": bid, "cols": 120, "rows": 30})
	a.send(map[string]any{"type": "call", "id": 3, "method": "hive.terminal.size", "params": map[string]any{"block_id": bid}})
	if res := a.nextJSON()["result"].(map[string]any); res["cols"].(float64) != 120 || res["size_owner"] != a.id {
		t.Fatalf("the mac after the release = %v", res)
	}
}

func TestAttachSlowClientGetsResync(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	bid := strs(created["block_ids"])[0]
	c := dial(t, h, "slow")
	defer c.ws.CloseNow()
	c.send(map[string]any{"type": "subscribe", "block_id": bid})
	c.nextJSON()
	// Flood output without reading the socket.
	big := strings.Repeat("x", 4000) + "\r"
	for range 3000 {
		h.call("hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{"data": big}})
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		typ, b := c.next()
		if typ == websocket.MessageText && strings.Contains(string(b), `"resync"`) {
			return
		}
	}
	t.Fatal("no resync for a client that fell behind")
}

// A client that falls behind on events gets events_resync, and then events
// again: the stream goes on and the client re-reads state, as the protocol
// says. The stream used to end there, and the app, which doesn't subscribe
// again, heard nothing more (titles, agent status) until it reconnected.
func TestEventsGoOnAfterResync(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"sh"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	c := dial(t, h, "slow")
	c.send(map[string]any{"type": "events", "session_id": sid, "all": true})
	for m := c.nextJSON(); m["type"] != "events_subscribed"; m = c.nextJSON() {
	}
	// A burst while the client reads nothing: more than it can queue.
	h.call("hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{
		"data": "i=0; while [ $i -lt 20000 ]; do printf '\\033]2;t%d\\007' $i; i=$((i+1)); done; echo burst-done\n"}})
	time.Sleep(3 * time.Second)
	for deadline := time.Now().Add(20 * time.Second); ; {
		if m := c.nextJSON(); m["type"] == "events_resync" {
			t.Log("got events_resync")
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no events_resync after the burst")
		}
	}
	h.call("hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{"data": "printf '\\033]2;after-resync\\007'\n"}})
	for deadline := time.Now().Add(20 * time.Second); ; {
		_, b := c.next()
		if strings.Contains(string(b), "after-resync") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no events after events_resync")
		}
	}
}

// A client that stops answering (a laptop asleep, a network gone) is
// dropped: pings go unanswered, and it leaves client.list.
func TestSilentAttachClientDropped(t *testing.T) {
	h := startCfg(t, t.TempDir(), quickPings)
	defer h.stop()
	c := dial(t, h, "goes-silent")
	defer c.ws.CloseNow()
	listed := func() bool {
		for _, x := range h.call("client.list", nil)["clients"].([]any) {
			if x.(map[string]any)["name"] == "goes-silent" {
				return true
			}
		}
		return false
	}
	if !listed() {
		t.Fatal("not listed after hello")
	}
	// It never reads again, so it never answers a ping.
	deadline := time.Now().Add(5 * time.Second)
	for listed() {
		if time.Now().After(deadline) {
			t.Fatal("a client that stopped answering pings is still attached")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A client that keeps reading answers pings and stays.
func TestAnsweringAttachClientStays(t *testing.T) {
	h := startCfg(t, t.TempDir(), quickPings)
	defer h.stop()
	c := dial(t, h, "keeps-reading")
	defer c.ws.CloseNow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			if _, _, err := c.ws.Read(ctx); err != nil {
				return
			}
		}
	}()
	time.Sleep(time.Second)
	for _, x := range h.call("client.list", nil)["clients"].([]any) {
		if x.(map[string]any)["name"] == "keeps-reading" {
			return
		}
	}
	t.Fatal("a client answering pings was dropped")
}

// quickPings pings attached clients every 100 ms, with 200 ms for a pong.
func quickPings(c *Config) {
	c.attachPingEvery, c.attachPongWait = 100*time.Millisecond, 200*time.Millisecond
}

// A client whose input the program isn't reading (stopped, or a big paste
// into a busy program) is alive: the server is the one blocked, writing to
// the pty (on Linux; macOS drops what doesn't fit), so the client's pongs
// wait. It isn't dropped. The hook holds the handler as such a write would.
func TestBlockedInputDoesNotDropTheClient(t *testing.T) {
	h := startCfg(t, t.TempDir(), func(c *Config) {
		quickPings(c)
		c.beforeAttachMessage = func(typ string) {
			if typ == "input" {
				time.Sleep(1500 * time.Millisecond)
			}
		}
	})
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"sleep", "30"}})
	bid := strs(created["block_ids"])[0]
	c := dial(t, h, "big-paste")
	defer c.ws.CloseNow()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		for {
			if _, _, err := c.ws.Read(ctx); err != nil {
				return
			}
		}
	}()
	c.send(map[string]any{"type": "input", "block_id": bid, "data_base64": base64.StdEncoding.EncodeToString([]byte("x"))})
	select {
	case <-closed:
		t.Fatal("a client whose input was blocked on the pty was dropped as silent")
	case <-time.After(1200 * time.Millisecond): // the handler is still blocked, the connection open
	}
}

// Shutdown ends attached clients too (http.Server.Shutdown leaves hijacked
// connections alone).
func TestShutdownEndsAttachedClients(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	c := dial(t, h, "to-be-closed")
	defer c.ws.CloseNow()
	h.stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		if _, _, err := c.ws.Read(ctx); err != nil {
			if ctx.Err() != nil {
				t.Fatal("still open 3 s after shutdown")
			}
			return
		}
	}
}

// A pinned tab is pinned for every client: the flag lives with the session,
// in session.view and across a restart.
func TestPinWindow(t *testing.T) {
	dir := t.TempDir()
	h := start(t, dir, nil)
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)
	view := h.call("session.view", map[string]any{"session_id": sid})
	wid := view["windows"].([]any)[0].(map[string]any)["window_id"].(string)
	h.call("session.pin_window", map[string]any{"session_id": sid, "window_id": wid, "pinned": true})
	view = h.call("session.view", map[string]any{"session_id": sid})
	if p, _ := view["windows"].([]any)[0].(map[string]any)["pinned"].(bool); !p {
		t.Fatalf("not pinned: %v", view["windows"])
	}
	h.call("session.pin_window", map[string]any{"session_id": sid, "window_id": wid, "pinned": false})
	view = h.call("session.view", map[string]any{"session_id": sid})
	if p, _ := view["windows"].([]any)[0].(map[string]any)["pinned"].(bool); p {
		t.Fatalf("still pinned: %v", view["windows"])
	}
	// Kept with the session: pinned again, then a restart.
	h.call("session.pin_window", map[string]any{"session_id": sid, "window_id": wid, "pinned": true})
	h.stop()
	h = start(t, dir, nil)
	defer h.stop()
	view = h.call("session.view", map[string]any{"session_id": sid})
	if p, _ := view["windows"].([]any)[0].(map[string]any)["pinned"].(bool); !p {
		t.Fatalf("not pinned after a restart: %v", view["windows"])
	}
}
