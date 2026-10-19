package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// The echo probe: how a server feels to type into, locally or over a
// network (a tailnet). Over the attach stream, as the app uses it:
//   - call: a server.version call's round trip, the floor (network and
//     hive, no terminal);
//   - echo: a key typed into a pane running cat, until its echo comes back
//     (the terminal echoes at once): the whole typing loop;
//   - flood (--flood MB): a pane printing that much, as fast as it can:
//     what the link sustains, and whether the server had to resync.
// --server is a socket path or a URL (http://box:4483, https://box.ts.net).

type echoResult struct {
	Scenario string         `json:"scenario"`
	Meta     Meta           `json:"meta"`
	Params   map[string]any `json:"params"`
	Call     Dist           `json:"call_rtt"`
	Echo     Dist           `json:"echo_rtt"`
	// Echo's p50 over the call's: what the terminal path adds.
	OverheadMs float64 `json:"echo_overhead_p50_ms"`
	FloodMBps  float64 `json:"flood_mb_per_s,omitempty"`
	Resyncs    int     `json:"flood_resyncs"`
}

func echoProbe(args []string) error {
	fs := flag.NewFlagSet("echo", flag.ContinueOnError)
	server := fs.String("server", "", "the server: a socket path, or http(s)://host[:port]")
	n := fs.Int("n", 300, "keys typed")
	every := fs.Duration("every", 30*time.Millisecond, "time between keys (a typist's pace)")
	flood := fs.Int("flood", 0, "MB a pane prints as fast as it can (0: skip)")
	label := fs.String("label", "", "what the path is, for the result (direct, relay…)")
	out := fs.String("out", "bench/results", "where results go")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *server == "" {
		return errors.New("echo needs --server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	a, err := dialAttach(ctx, *server)
	if err != nil {
		return err
	}
	defer a.ws.CloseNow()

	// The floor: calls over the same stream.
	var calls []time.Duration
	for i := range 50 {
		t0 := time.Now()
		if _, err := a.call(ctx, "server.version", nil); err != nil {
			return err
		}
		if i >= 5 { // the first few warm up the path
			calls = append(calls, time.Since(t0))
		}
		time.Sleep(*every)
	}

	// A pane running cat: the terminal echoes each key as it arrives.
	created, err := a.call(ctx, "session.create", map[string]any{"label": "hivebench-echo", "command": []string{"cat"}})
	if err != nil {
		return err
	}
	var c struct {
		SessionID string   `json:"session_id"`
		BlockIDs  []string `json:"block_ids"`
	}
	_ = json.Unmarshal(created, &c)
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		_, _ = a.call(cctx, "session.close", map[string]any{"session_id": c.SessionID})
	}()
	bid := c.BlockIDs[0]
	if err := a.subscribe(ctx, bid); err != nil {
		return err
	}
	var echoes []time.Duration
	for i := range *n {
		key := byte('a' + i%26)
		if i%60 == 59 {
			key = '\r' // cat's line stays short
		}
		t0 := time.Now()
		if err := a.send(ctx, map[string]any{"type": "input", "block_id": bid, "data_base64": base64.StdEncoding.EncodeToString([]byte{key})}); err != nil {
			return err
		}
		want := []byte{key}
		if key == '\r' {
			want = []byte("\n")
		}
		if err := a.waitOutput(ctx, bid, want); err != nil {
			return fmt.Errorf("key %d: %w", i, err)
		}
		if key != '\r' && i >= 5 {
			echoes = append(echoes, time.Since(t0))
		}
		time.Sleep(*every)
	}
	res := echoResult{Scenario: "echo", Meta: meta(), Call: dist(calls), Echo: dist(echoes),
		Params: map[string]any{"server": redactURL(*server), "keys": *n, "every_ms": every.Milliseconds(), "path": *label}}
	res.OverheadMs = res.Echo.P50 - res.Call.P50
	fmt.Printf("%s %s\n  call  p50 %6.2f p95 %6.2f p99 %6.2f max %6.2f ms\n  echo  p50 %6.2f p95 %6.2f p99 %6.2f max %6.2f ms  (the terminal adds %.2f ms)\n",
		redactURL(*server), *label, res.Call.P50, pct(calls, 0.95), res.Call.P99, res.Call.Max,
		res.Echo.P50, pct(echoes, 0.95), res.Echo.P99, res.Echo.Max, res.OverheadMs)

	if *flood > 0 {
		mbps, resyncs, err := floodRun(ctx, a, c.SessionID, *flood)
		if err != nil {
			return err
		}
		res.FloodMBps, res.Resyncs = mbps, resyncs
		fmt.Printf("  flood %d MB at %.1f MB/s, %d resyncs\n", *flood, mbps, resyncs)
	}
	res.Meta.LoadAfter = loadAvg()
	name, err := writeResult(*out, "echo", res)
	if err == nil {
		fmt.Println("result:", name)
	}
	return err
}

// pct is a percentile of samples, in milliseconds.
func pct(d []time.Duration, p float64) float64 {
	if len(d) == 0 {
		return 0
	}
	s := slices.Clone(d)
	slices.Sort(s)
	return float64(s[min(len(s)-1, int(float64(len(s))*p))]) / float64(time.Millisecond)
}

// floodRun prints mb MB in a new window and reads it all.
func floodRun(ctx context.Context, a *attach, sid string, mb int) (float64, int, error) {
	cmd := fmt.Sprintf("head -c %d /dev/zero | tr '\\0' x; printf '\\nflood-done\\n'; exec cat", mb<<20)
	w, err := a.call(ctx, "session.new_window", map[string]any{"session_id": sid, "layout": map[string]any{"command": []string{"sh", "-c", cmd}}})
	if err != nil {
		return 0, 0, err
	}
	var nw struct {
		BlockIDs []string `json:"block_ids"`
	}
	_ = json.Unmarshal(w, &nw)
	bid := nw.BlockIDs[0]
	t0 := time.Now()
	if err := a.subscribe(ctx, bid); err != nil {
		return 0, 0, err
	}
	a.resyncs = 0
	if err := a.waitOutput(ctx, bid, []byte("flood-done")); err != nil {
		return 0, a.resyncs, err
	}
	return float64(mb) / time.Since(t0).Seconds(), a.resyncs, nil
}

// attach is one attach-stream connection, read in order on the caller's
// goroutine.
type attach struct {
	ws      *websocket.Conn
	nextID  int
	resyncs int
	tail    map[string][]byte // a block's last bytes, for a marker split across frames
}

func dialAttach(ctx context.Context, server string) (*attach, error) {
	hc := &http.Client{}
	wsURL := "ws://hive/v1/attach"
	if strings.HasPrefix(server, "/") {
		hc.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", server)
		}}
	} else {
		u, err := url.Parse(server)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("--server %s: a socket path or http(s)://host[:port]", server)
		}
		// HTTP/1.1: the attach stream is an HTTP/1.1 upgrade.
		hc.Transport = &http.Transport{TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
			TLSClientConfig: &tls.Config{NextProtos: []string{"http/1.1"}}}
		wsURL = map[string]string{"http": "ws", "https": "wss"}[u.Scheme] + "://" + u.Host + "/v1/attach"
	}
	ws, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: hc})
	if err != nil {
		return nil, err
	}
	ws.SetReadLimit(256 << 20)
	a := &attach{ws: ws, tail: map[string][]byte{}}
	if err := a.send(ctx, map[string]any{"type": "hello", "name": "hivebench", "kind": "bench"}); err != nil {
		return nil, err
	}
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return nil, err
		}
		if typ == websocket.MessageText && bytes.Contains(data, []byte(`"welcome"`)) {
			return a, nil
		}
	}
}

func (a *attach) send(ctx context.Context, m any) error {
	b, _ := json.Marshal(m)
	return a.ws.Write(ctx, websocket.MessageText, b)
}

func (a *attach) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	a.nextID++
	id := a.nextID
	if err := a.send(ctx, map[string]any{"type": "call", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		typ, data, err := a.ws.Read(ctx)
		if err != nil {
			return nil, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var m struct {
			Type   string          `json:"type"`
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(data, &m) != nil || m.Type != "result" || m.ID != id {
			continue
		}
		if len(m.Error) > 0 && string(m.Error) != "null" {
			return nil, fmt.Errorf("%s: %s", method, m.Error)
		}
		return m.Result, nil
	}
}

// subscribe waits for the block's snapshot.
func (a *attach) subscribe(ctx context.Context, bid string) error {
	if err := a.send(ctx, map[string]any{"type": "subscribe", "block_id": bid}); err != nil {
		return err
	}
	for {
		kind, id, _, err := a.frame(ctx)
		if err != nil {
			return err
		}
		if kind == 2 && id == bid {
			return nil
		}
	}
}

// waitOutput reads until the block's output contains want.
func (a *attach) waitOutput(ctx context.Context, bid string, want []byte) error {
	for {
		kind, id, payload, err := a.frame(ctx)
		if err != nil {
			return err
		}
		if id != bid || (kind != 1 && kind != 2) {
			continue
		}
		buf := append(a.tail[bid], payload...)
		if bytes.Contains(buf, want) {
			a.tail[bid] = nil
			return nil
		}
		if len(buf) > 64 {
			buf = buf[len(buf)-64:]
		}
		a.tail[bid] = append([]byte(nil), buf...)
	}
}

// frame reads the next binary frame: kind (1 output, 2 snapshot, 3
// resize), block, payload. A resync is counted and the block subscribed
// again.
func (a *attach) frame(ctx context.Context) (byte, string, []byte, error) {
	for {
		typ, data, err := a.ws.Read(ctx)
		if err != nil {
			return 0, "", nil, err
		}
		if typ == websocket.MessageText {
			var m struct {
				Type    string `json:"type"`
				BlockID string `json:"block_id"`
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "resync" {
				a.resyncs++
				_ = a.send(ctx, map[string]any{"type": "subscribe", "block_id": m.BlockID})
			}
			continue
		}
		if len(data) < 3 {
			continue
		}
		n := int(binary.BigEndian.Uint16(data[1:3]))
		if len(data) < 3+n+8 {
			continue
		}
		return data[0], string(data[3 : 3+n]), data[3+n+8:], nil
	}
}

// redactURL keeps a URL's scheme and host only.
func redactURL(s string) string {
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return s
}
