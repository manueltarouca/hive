package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/script"
)

func (h *harness) callErr(method string, params any) *client.Error {
	h.t.Helper()
	err := h.c.Call(context.Background(), method, params, nil)
	var e *client.Error
	if !errors.As(err, &e) {
		h.t.Fatalf("%s: want an API error, got %v", method, err)
	}
	return e
}

// A rule sees a blocked agent and sends a notification whose button
// answers it; a button from an older state does nothing.
func TestAgentRuleSuggests(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	c := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := c["session_id"].(string), strs(c["block_ids"])[0]

	js, err := script.Bundle("", `
h.agents.rule({
  name: "approve-read",
  when: { state: "blocked", message: "permission to use (Read|Grep)\\b" },
  do: { write: "approved\n" },
});`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := h.call("script.run", map[string]any{"code": js, "allow": []string{"input", "ui"}, "session_id": sid, "for": 20})
	defer h.call("script.stop", map[string]any{"run_id": run["run_id"]})

	notes := make(chan map[string]any, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.c.Events(ctx, "", true, int64(h.srv.bus.Seq()), func(e client.Event) bool {
		if e.Type == "notification" {
			var p struct {
				N map[string]any `json:"notification"`
			}
			_ = json.Unmarshal(e.Payload, &p)
			notes <- p.N
		}
		return true
	})
	time.Sleep(100 * time.Millisecond)
	report := func(kind, msg string) {
		h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "blocked", "kind": kind, "message": msg})
	}
	// A question isn't a permission prompt, and Bash isn't Read.
	report("elicitation_dialog", "Claude needs your permission to use Read")
	report("permission_prompt", "Claude needs your permission to use Bash")
	report("permission_prompt", "Claude needs your permission to use Read")
	var n map[string]any
	select {
	case n = <-notes:
	case <-time.After(5 * time.Second):
		t.Fatal("no notification")
	}
	if !strings.Contains(n["body"].(string), "use Read") || n["block_id"] != bid {
		t.Fatalf("notification = %v", n)
	}
	select {
	case extra := <-notes:
		t.Fatalf("a second notification: %v", extra)
	case <-time.After(200 * time.Millisecond):
	}
	button := n["actions"].([]any)[0].(map[string]any)
	if button["step"] != `Type "approved\n" in the agent's pane` {
		t.Fatalf("step = %v", button["step"])
	}
	tap := map[string]any{"name": button["action"], "args": button["args"], "origin": "notification"}

	// The agent moved on: the old button sends nothing.
	report("permission_prompt", "Claude needs your permission to use Grep")
	if e := h.callErr("action.run", tap); e.Code != "conflict" {
		t.Fatalf("stale tap: %v", e)
	}
	n = <-notes
	tap["args"] = n["actions"].([]any)[0].(map[string]any)["args"]
	h.call("action.run", tap)
	h.waitScreen(sid, bid, "approved")
}

// Notifications are kept for apps that were closed; buttons only name the
// sender's own actions; badges go when their script stops.
func TestNotificationsAndBadges(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	c := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := c["session_id"].(string), strs(c["block_ids"])[0]

	n := h.call("notify.send", map[string]any{"title": "Tests failed", "body": "3 failures", "target": bid, "level": "error"})
	if n["source"] != "cli" || n["session_id"] != sid {
		t.Fatalf("notification = %v", n)
	}
	list := h.call("notification.list", nil)["notifications"].([]any)
	if len(list) != 1 {
		t.Fatalf("list = %v", list)
	}
	h.call("notification.ack", map[string]any{"notification_id": n["notification_id"]})
	if l := h.call("notification.list", nil)["notifications"].([]any); len(l) != 0 {
		t.Fatalf("after ack = %v", l)
	}
	for i := 0; i < 9; i++ {
		h.call("notify.send", map[string]any{"title": "x"})
	}
	if e := h.callErr("notify.send", map[string]any{"title": "x"}); e.Code != "rate_limited" {
		t.Fatalf("rate: %v", e)
	}

	// A script's badge and action; another sender can't use its action.
	out := h.runScript(`
h.actions.register({ name: "hello", title: "Say hello", run: (ctx, a: any) => "hi " + a.who });
await h.status.set({ target: h.ctx.block_id!, id: "tests", state: "error", msg: "3 failing" });
const r = await h.actions.run("hello", { who: "me" });
h.after("10ms", () => h.stop()); // its action would keep it running
export default () => r;`, map[string]any{"allow": []string{"ui"}, "session_id": sid, "block_id": bid})
	if out.str(t) != "hi me" {
		t.Fatalf("action: %+v", out)
	}
	// The script ended: its action and badge are gone.
	if a := h.call("action.list", nil)["actions"].([]any); len(a) != 0 {
		t.Fatalf("actions = %v", a)
	}
	if b := h.call("status.list", nil)["badges"].([]any); len(b) != 0 {
		t.Fatalf("badges = %v", b)
	}
	h.call("status.set", map[string]any{"target": bid, "id": "ci", "state": "working", "progress": 40})
	if b := h.call("status.list", map[string]any{"target": bid})["badges"].([]any); len(b) != 1 || b[0].(map[string]any)["app"] != "cli" {
		t.Fatalf("badges = %v", b)
	}
	if e := h.callErr("notify.send", map[string]any{"title": "x2", "actions": []any{map[string]any{"title": "Go", "action": "hello"}}}); e.Code != "not_found" && e.Code != "rate_limited" {
		t.Fatalf("foreign action: %v", e)
	}
}
