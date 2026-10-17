package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/client"
)

type harness struct {
	t      *testing.T
	srv    *Server
	c      *client.Client
	sock   string
	state  string
	served chan error
}

func start(t *testing.T, state string, resume func(string, string) []string) *harness {
	t.Helper()
	sock := filepath.Join(os.TempDir(), fmt.Sprintf("hive-test-%d-%d.sock", os.Getpid(), time.Now().UnixNano()))
	srv, err := New(Config{Socket: sock, StateDir: state, FlushDelay: 50 * time.Millisecond, ResumeArgv: resume})
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, srv: srv, c: client.New(sock), sock: sock, state: state, served: make(chan error, 1)}
	go func() { h.served <- srv.Serve() }()
	deadline := time.Now().Add(3 * time.Second)
	for !h.c.Ping() {
		if time.Now().After(deadline) {
			t.Fatal("server never listened")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return h
}

func (h *harness) stop() {
	h.srv.Shutdown()
	select {
	case <-h.srv.Done():
	case <-time.After(10 * time.Second):
		h.t.Fatal("shutdown hung")
	}
}

func (h *harness) call(method string, params any) map[string]any {
	h.t.Helper()
	var out map[string]any
	if err := h.c.Call(context.Background(), method, params, &out); err != nil {
		h.t.Fatalf("%s: %v", method, err)
	}
	return out
}

func (h *harness) screen(sessionID, blockID string) string {
	out := h.call("hive.terminal.format", map[string]any{"session_id": sessionID, "block_id": blockID, "args": map[string]any{"trim": true}})
	return out["content"].(string)
}

func (h *harness) waitScreen(sessionID, blockID, want string) string {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var s string
	for time.Now().Before(deadline) {
		if s = h.screen(sessionID, blockID); strings.Contains(s, want) {
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("screen never showed %q:\n%s", want, s)
	return ""
}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func TestSessionsSplitsIO(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()

	created := h.call("session.create", map[string]any{"label": "work", "command": []string{"cat"}})
	sid := created["session_id"].(string)
	first := strs(created["block_ids"])[0]

	split := h.call("session.new_split", map[string]any{
		"session_id": sid, "block_id": first, "ratio": 0.3,
		"layout": map[string]any{"command": []string{"sh", "-c", "echo split-ready; exec cat"}, "label": "logs"},
	})
	second := strs(split["block_ids"])[0]
	if split["anchor_block_id"] != first || len(strs(split["split_ids"])) != 1 {
		t.Fatalf("split = %v", split)
	}
	h.waitScreen(sid, second, "split-ready")

	// Target resolution by label and suffix.
	res := h.call("target.resolve", map[string]any{"kind": "block", "target": "LOGS", "session_id": sid})
	if res["id"] != second {
		t.Fatalf("resolve label = %v", res)
	}
	res = h.call("target.resolve", map[string]any{"kind": "session", "target": sid[len(sid)-6:]})
	if res["id"] != sid {
		t.Fatalf("resolve suffix = %v", res)
	}

	// Write and keys, through block.method, the the reference app-shaped generic call.
	h.call("block.method", map[string]any{"block_id": first, "method": "write", "args": map[string]any{"data": "abc"}})
	h.call("hive.terminal.send_key", map[string]any{"block_id": first, "args": map[string]any{"keys": []string{"enter"}}})
	if s := h.waitScreen(sid, first, "abc\nabc"); s == "" {
		t.Fatal()
	}

	// Sizes follow the split: the new block got about 30% of 120 columns.
	size := h.call("hive.terminal.size", map[string]any{"block_id": second})
	if c := size["cols"].(float64); c < 30 || c > 40 {
		t.Fatalf("new block cols = %v", c)
	}

	view := h.call("session.view", map[string]any{"session_id": sid})
	if len(view["windows"].([]any)) != 1 || len(view["blocks"].(map[string]any)) != 2 {
		t.Fatalf("view = %v", view)
	}

	// Unknown argument and missing session are rejected with the reference app's codes.
	var cerr *client.Error
	err := h.c.Call(context.Background(), "session.set_block_label", map[string]any{"session_id": sid, "lable": "x"}, nil)
	if !errors.As(err, &cerr) || cerr.Code != "bad_request" {
		t.Fatalf("err = %v", err)
	}
	err = h.c.Call(context.Background(), "session.view", map[string]any{"session_id": "session:nope"}, nil)
	if !errors.As(err, &cerr) || cerr.Code != "not_found" {
		t.Fatalf("err = %v", err)
	}

	// Closing the second block collapses the layout.
	h.call("block.close", map[string]any{"session_id": sid, "block_id": second})
	view = h.call("session.view", map[string]any{"session_id": sid})
	layout := view["windows"].([]any)[0].(map[string]any)["layout"].(map[string]any)
	if layout["type"] != "block" || layout["block_id"] != first {
		t.Fatalf("layout = %v", layout)
	}
}

func TestEventsAndExitClosesBlock(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got := make(chan string, 64)
	go h.c.Events(ctx, sid, false, -1, func(e client.Event) bool {
		got <- string(e.Raw)
		return true
	})
	time.Sleep(100 * time.Millisecond)
	split := h.call("session.new_split", map[string]any{"session_id": sid, "block_id": strs(created["block_ids"])[0],
		"layout": map[string]any{"command": []string{"sh", "-c", "printf '\\033]2;job\\007'; exit 4"}}})
	bid := strs(split["block_ids"])[0]

	want := map[string]bool{"block_created": false, "title_changed": false, "child_exited": false, "block_closed": false}
	timeout := time.After(8 * time.Second)
	for !(want["block_created"] && want["title_changed"] && want["child_exited"] && want["block_closed"]) {
		select {
		case line := <-got:
			for k := range want {
				if strings.Contains(line, `"`+k+`"`) && strings.Contains(line, bid) {
					want[k] = true
				}
				if k == "child_exited" && strings.Contains(line, `"exit_code":4`) {
					want[k] = true
				}
			}
		case <-timeout:
			t.Fatalf("events seen: %v", want)
		}
	}
	// since= replays what we already saw.
	var replayed int
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	h.c.Events(ctx2, sid, false, 0, func(e client.Event) bool {
		if e.Type == "session_event" {
			replayed++
		}
		return replayed < 3
	})
	if replayed < 3 {
		t.Fatalf("replayed %d", replayed)
	}
}

func TestRestoreAfterRestart(t *testing.T) {
	state := t.TempDir()
	resume := func(harness, id string) []string {
		return []string{"sh", "-c", "echo resumed " + harness + " " + id + "; exec cat"}
	}
	h := start(t, state, resume)
	created := h.call("session.create", map[string]any{"label": "keep", "cols": 100, "rows": 30})
	sid := created["session_id"].(string)
	shell := strs(created["block_ids"])[0]
	// Fill part of the screen so the cursor is well below the top.
	h.call("hive.terminal.write", map[string]any{"block_id": shell, "args": map[string]any{"data": "for i in 1 2 3 4 5 6; do echo filler $i; done; echo before-restart-marker\n"}})
	h.waitScreen(sid, shell, "before-restart-marker\n")

	split := h.call("session.new_split", map[string]any{"session_id": sid, "block_id": shell, "direction": "vertical",
		"layout": map[string]any{"command": []string{"sh", "-c", "printf '\\033]2;✳ Fix login\\007'; exec cat"}, "label": "agent"}})
	agentBlock := strs(split["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": agentBlock, "harness": "claude", "state": "idle",
		"session_id": "11111111-2222", "cwd": os.TempDir(), "event": "SessionStart"})
	// /clear mints a new session id; the latest one wins.
	h.call("agent.report", map[string]any{"block_id": agentBlock, "harness": "claude", "state": "idle",
		"session_id": "33333333-4444", "event": "SessionStart"})
	h.call("session.set_window_label", map[string]any{"session_id": sid, "window_id": created["window_id"], "label": "main"})
	h.stop()

	h2 := start(t, state, resume)
	defer h2.stop()
	list := h2.call("session.list", nil)
	sessions := list["sessions"].([]any)
	if len(sessions) != 1 || sessions[0].(map[string]any)["label"] != "keep" {
		t.Fatalf("sessions = %v", sessions)
	}
	s := h2.waitScreen(sid, shell, "restored")
	if !strings.Contains(s, "before-restart-marker") {
		t.Fatalf("scrollback lost:\n%s", s)
	}
	if strings.LastIndex(s, "before-restart-marker") > strings.Index(s, "── restored") {
		t.Fatalf("restored banner is not below the old output:\n%s", s)
	}
	h2.waitScreen(sid, agentBlock, "resumed claude 33333333-4444")
	view := h2.call("session.view", map[string]any{"session_id": sid})
	w := view["windows"].([]any)[0].(map[string]any)
	if w["label"] != "main" || w["layout"].(map[string]any)["type"] != "split" {
		t.Fatalf("window = %v", w)
	}
	agents := h2.call("agent.list", nil)["agents"].([]any)
	if len(agents) != 1 {
		t.Fatalf("agents = %v", agents)
	}
	raw, _ := json.Marshal(agents[0])
	if !strings.Contains(string(raw), "33333333-4444") || !strings.Contains(string(raw), "Fix login") {
		t.Fatalf("agent = %s", raw)
	}
}

func TestEndedAgentIsNotResumed(t *testing.T) {
	state := t.TempDir()
	resume := func(harness, id string) []string { return []string{"sh", "-c", "echo SHOULD-NOT-RESUME; exec cat"} }
	h := start(t, state, resume)
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "idle", "session_id": "x1"})
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "ended", "event": "SessionEnd"})
	h.stop()
	h2 := start(t, state, resume)
	defer h2.stop()
	s := h2.waitScreen(sid, bid, "restored")
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(h2.screen(sid, bid), "SHOULD-NOT-RESUME") {
		t.Fatalf("ended agent resumed:\n%s", s)
	}
}

func TestAPIIntrospection(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	var list []map[string]any
	if err := h.c.Call(context.Background(), "api.list", nil, &list); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, m := range list {
		names[m["name"].(string)] = true
	}
	for _, want := range []string{"session.new_split", "session.view", "block.method", "hive.terminal.format", "agent.report", "server.version"} {
		if !names[want] {
			t.Errorf("missing %s", want)
		}
	}
	d := h.call("api.describe", map[string]any{"method": "session.new_split"})
	if d["input_schema"].(map[string]any)["required"] == nil {
		t.Fatalf("describe = %v", d)
	}
}

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
