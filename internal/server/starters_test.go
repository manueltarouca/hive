package server

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/manueltarouca/hive/examples"
	"github.com/manueltarouca/hive/internal/script"
)

func starter(t *testing.T, name string) string {
	t.Helper()
	s, ok := examples.Get(name)
	if !ok {
		t.Fatalf("no starter %s", name)
	}
	return s.Code
}

func examplesDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "examples", "scripts")
}

// The starters' pure parts, run as scripts import them.
func TestStarterHelpers(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	js, err := script.Bundle("", `
import { parseReset } from "./limit-resume.ts";
import { settled } from "./stall-watch.ts";
import { waiting } from "./needs-you.ts";
import { summarize } from "./digest.ts";
const now = new Date(2026, 9, 22, 18, 0);
const at = (s: string) => { const d = parseReset(s, now); return d ? [d.getMonth() + 1, d.getDate(), d.getHours(), d.getMinutes()].join(" ") : "none"; };
const resets = [
  "You've hit your session limit · resets 11:10am (Europe/Lisbon)",
  "You've hit your session limit · resets 10pm (Europe/Lisbon)",
  "You've hit your weekly limit · resets Oct 25 at 9am (Europe/Lisbon)",
  "Usage limit reached · limit resets Oct 5 at 9am",
  "limit · resets in 2h 30m",
  "limit · resets 12am",
  "You've hit your limit",
].map(at);
const screen = "⏺ Running the tests\nok 1\n✻ Thinking… (34s · ↑ 1.2k tokens · esc to interrupt)\n" + "\n".repeat(8);
const later = screen.replace("34s", "35s");
const row = (state: string, kind: string, t: string) => ({ block_id: state + kind, session_id: "s", session_label: "wkspc", label: state,
  agent: { harness: "claude", state, kind, revision: 1, updated_at: t } }) as any;
const order = waiting([row("done", "", "2026-10-22T10:00:00Z"), row("blocked", "permission_prompt", "2026-10-22T11:00:00Z"),
  row("done", "error", "2026-10-22T09:00:00Z"), row("working", "", "2026-10-22T08:00:00Z")], false).map((r) => r.label + r.agent.kind);
const d = summarize([row("blocked", "permission_prompt", new Date().toISOString()), row("working", "", new Date().toISOString()),
  row("done", "error", new Date().toISOString())], [{ t: Date.now(), label: "x", session: "wkspc", state: "done", kind: "error" }], 0);
export default () => ({ resets, same: settled(screen) === settled(later), order, title: d.title, body: d.body });
`, examplesDir())
	if err != nil {
		t.Fatal(err)
	}
	run := h.call("script.run", map[string]any{"code": js})
	out := h.call("script.result", map[string]any{"run_id": run["run_id"], "timeout": 10})
	var got struct {
		Resets []string
		Same   bool
		Order  []string
		Title  string
		Body   string
	}
	raw, _ := json.Marshal(out["result"])
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("result %v: %v", out, err)
	}
	want := []string{"10 23 11 10", "10 22 22 0", "10 25 9 0", "10 5 9 0", "10 22 20 30", "10 23 0 0", "none"}
	if strings.Join(got.Resets, ",") != strings.Join(want, ",") {
		t.Errorf("resets = %q, want %q", got.Resets, want)
	}
	if !got.Same {
		t.Error("a ticking timer counts as the screen moving")
	}
	if strings.Join(got.Order, ",") != "blockedpermission_prompt,doneerror" {
		t.Errorf("waiting order = %v", got.Order)
	}
	if got.Title != "1 waiting, 1 stopped, 1 working" || !strings.Contains(got.Body, "1 stop at a limit") {
		t.Errorf("digest = %q / %q", got.Title, got.Body)
	}
}

// limitAgent is a pane where Claude Code stops at its usage limit: it
// reports working, then error with the reason, then echoes what it gets.
func limitAgent(h *residentHarness, reason string) (bid, sid string) {
	msg := base64.StdEncoding.EncodeToString([]byte(reason))
	created := h.call("session.create", map[string]any{"command": fakeAgent(h.t,
		`nap 0.5; osc "state=error:app=claude-code:msg=`+msg+`";`)})
	bid = strs(created["block_ids"])[0]
	sid, _ = created["session_id"].(string)
	h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": "working"})
	return
}

func (h *residentHarness) screen(bid string) string {
	out := h.call("hive.terminal.format", map[string]any{"block_id": bid, "args": map[string]any{"tail": 40}})
	s, _ := out["content"].(string)
	return s
}

func (h *residentHarness) notes() []Notification {
	var out struct {
		Notifications []Notification `json:"notifications"`
	}
	raw, _ := json.Marshal(h.call("notification.list", map[string]any{"all": true}))
	_ = json.Unmarshal(raw, &out)
	return out.Notifications
}

func (h *residentHarness) badges(bid string) []Badge {
	var out struct {
		Badges []Badge `json:"badges"`
	}
	raw, _ := json.Marshal(h.call("status.list", map[string]any{}))
	_ = json.Unmarshal(raw, &out)
	var mine []Badge
	for _, b := range out.Badges {
		if b.BlockID == bid {
			mine = append(mine, b)
		}
	}
	return mine
}

func grantStarter(t *testing.T, h *residentHarness, name string) {
	t.Helper()
	h.until(name+" pending", func() bool { return h.script(name).State == script.StatePending })
	h.call("script.grant", map[string]any{"name": name})
	h.until(name+" running", func() bool { return h.script(name).State == script.StateRunning })
}

func TestStarterLimitResume(t *testing.T) {
	h := startScripts(t, map[string]string{"limit-resume.ts": starter(t, "limit-resume")}, nil)
	defer h.stop()
	grantStarter(t, h, "limit-resume")
	h.call("script.set_option", map[string]any{"name": "limit-resume", "key": "delay_min", "value": 0})

	// Typing by itself: the limit resets, "resume" goes in, and you hear.
	bid, _ := limitAgent(h, "You've hit your session limit · resets in 0m")
	defer func() {
		if t.Failed() {
			t.Logf("logs:\n%s\nscreen: %q\nagents: %v\nbadges: %v", h.logs("limit-resume"), h.screen(bid), h.call("agent.list", nil), h.badges(bid))
		}
	}()
	h.until("resume typed", func() bool { return strings.Contains(h.screen(bid), "resume") })
	h.until("told", func() bool {
		for _, n := range h.notes() {
			if strings.HasPrefix(n.Title, "Resumed") && len(n.Actions) == 2 && n.Actions[0].Action == "hive.set_option" {
				return true
			}
		}
		return false
	})
	acts := h.activity("limit-resume")
	found := false
	for _, e := range acts {
		if e.Method == "agent.act" && e.BlockID == bid && e.Preview == "resume⏎" && e.Result == "ok" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no agent.act entry: %+v", acts)
	}
	h.until("badge gone", func() bool { return len(h.badges(bid)) == 0 })

	// Typing off: only told, with a button; nothing typed; the mark stays
	// across a reload.
	h.call("script.set_option", map[string]any{"name": "limit-resume", "key": "auto", "value": false})
	bid2, _ := limitAgent(h, "You've hit your session limit · resets in 0m")
	h.until("asked", func() bool {
		for _, n := range h.notes() {
			if strings.HasSuffix(n.Title, "limit has reset") && n.BlockID == bid2 {
				return true
			}
		}
		return false
	})
	if strings.Contains(h.screen(bid2), "resume") {
		t.Fatal("typed with auto off")
	}
	h.call("script.reload", map[string]any{"name": "limit-resume"})
	h.until("mark kept after reload", func() bool {
		b := h.badges(bid2)
		return len(b) == 1 && strings.HasPrefix(b[0].Msg, "limited")
	})
	// Its button resumes it.
	h.callAny("action.run", map[string]any{"name": "resume", "args": map[string]any{"block_id": bid2}})
	h.until("resumed by the button", func() bool { return strings.Contains(h.screen(bid2), "resume") })

	// Read off the screen: the hook never said done, the agent still says
	// working, and Claude's banner is on screen.
	h.call("script.set_option", map[string]any{"name": "limit-resume", "key": "auto", "value": true})
	created := h.call("session.create", map[string]any{"command": fakeAgent(t,
		`nap 0.5; print "  ⎿  You've hit your session limit · resets in 0m\r\n";`)})
	bid4 := strs(created["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": bid4, "harness": "claude", "state": "working"})
	h.until("resumed from the banner", func() bool { return strings.Contains(h.screen(bid4), "resume") })

	// Watch mode: the mark, no typing, and the refusal listed.
	h.call("script.set_mode", map[string]any{"name": "limit-resume", "mode": "watch"})
	bid3, _ := limitAgent(h, "You've hit your session limit · resets in 0m")
	h.until("refused and listed", func() bool {
		for _, e := range h.activity("limit-resume") {
			if e.BlockID == bid3 && e.Result == "watching" {
				return true
			}
		}
		return false
	})
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(h.screen(bid3), "resume") {
		t.Fatal("typed in watch mode")
	}
}

func TestStarterTeamAndLaneKeeper(t *testing.T) {
	h := startScripts(t, map[string]string{"team.ts": starter(t, "team"), "lane-keeper.ts": starter(t, "lane-keeper")}, nil)
	defer h.stop()
	grantStarter(t, h, "team")
	grantStarter(t, h, "lane-keeper")
	created := h.call("session.create", map[string]any{"label": "crew", "command": []string{"cat", "-v"}})
	sid, _ := created["session_id"].(string)
	lane := strs(created["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": lane, "harness": "claude", "state": "working"})
	w := h.call("session.new_window", map[string]any{"session_id": sid, "label": "orchestrator", "layout": map[string]any{"command": []string{"cat"}}})
	orch := strs(w["block_ids"])[0]
	h.call("session.set_block_label", map[string]any{"session_id": sid, "block_id": orch, "label": "orchestrator"})
	h.call("agent.report", map[string]any{"block_id": orch, "harness": "claude", "state": "idle"})

	// Without a session at hand, it won't act on every session.
	if e := h.callErr("action.run", map[string]any{"name": "pause"}); e == nil || !strings.Contains(e.Message, "all=true") {
		t.Fatalf("pause with no session: %v", e)
	}
	// Pause presses Esc in working agents only, resume types into those.
	out := h.callAny("action.run", map[string]any{"name": "pause", "session_id": sid})
	if names, _ := out.([]any); len(names) != 1 {
		t.Fatalf("paused %v, want the one working agent", out)
	}
	h.until("esc sent", func() bool { return strings.Contains(h.screen(lane), "^[") })
	h.call("agent.report", map[string]any{"block_id": lane, "harness": "claude", "state": "idle"})
	h.callAny("action.run", map[string]any{"name": "resume", "session_id": sid})
	h.until("continue typed", func() bool { return strings.Contains(h.screen(lane), "continue") })
	if strings.Contains(h.screen(orch), "continue") {
		t.Fatal("resumed an agent it never paused")
	}

	// lane-keeper tells an idle orchestrator about a lane.
	h.call("script.set_option", map[string]any{"name": "lane-keeper", "key": "session", "value": "crew"})
	h.callAny("action.run", map[string]any{"name": "tell", "args": map[string]any{"block_id": lane, "line": "lane has been idle 12 min."}})
	h.until("orchestrator told", func() bool { return strings.Contains(h.screen(orch), "[hive lane-keeper] lane has been idle 12 min.") })
	if !strings.Contains(strings.Join(func() []string {
		var s []string
		for _, n := range h.notes() {
			s = append(s, n.Title)
		}
		return s
	}(), "|"), "Told the orchestrator") {
		t.Fatal("didn't say it told the orchestrator")
	}
}

func TestStarterDigestNeedsYouDevPanes(t *testing.T) {
	h := startScripts(t, map[string]string{"digest.ts": starter(t, "digest"), "needs-you.ts": starter(t, "needs-you"),
		"dev-panes.ts": starter(t, "dev-panes")}, nil)
	defer h.stop()
	for _, n := range []string{"digest", "needs-you", "dev-panes"} {
		grantStarter(t, h, n)
	}
	a := h.call("session.create", map[string]any{"label": "work", "command": []string{"cat"}})
	blocked := strs(a["block_ids"])[0]
	h.call("agent.report", map[string]any{"block_id": blocked, "harness": "claude", "state": "blocked",
		"kind": "permission_prompt", "message": "Claude needs your permission to use Bash"})

	// The digest: one notification, counts first.
	if out := h.callAny("action.run", map[string]any{"name": "digest"}); out != "1 waiting" {
		t.Fatalf("digest = %v", out)
	}
	// The jump key's action names who waits longest.
	if out, _ := h.callAny("action.run", map[string]any{"name": "next-waiting"}).(string); !strings.Contains(out, "blocked, Claude needs your permission") {
		t.Fatalf("next-waiting = %q", out)
	}

	// dev-panes: a port while its server runs, a failure until the next
	// command; never on agent panes.
	sh := h.call("session.new_window", map[string]any{"session_id": a["session_id"], "label": "dev",
		"layout": map[string]any{"command": []string{"sh"}}})
	dev := strs(sh["block_ids"])[0]
	type_ := func(s string) {
		h.call("hive.terminal.write", map[string]any{"block_id": dev, "args": map[string]any{"data": s}})
	}
	// The command line itself mustn't say it: the shell echoes it.
	type_("python3 -c \"print('Listen' + 'ing on :8080', flush=True); import time; time.sleep(30)\"\r")
	h.until("port mark", func() bool {
		b := h.badges(dev)
		return len(b) == 1 && b[0].Msg == "port 8080"
	})
	type_("\x03")
	// It looks every 5 s; slower under a loaded test run.
	for deadline := time.Now().Add(20 * time.Second); len(h.badges(dev)) != 0; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("port mark stayed after the server stopped: process %v, screen %q, logs %s",
				h.call("hive.terminal.process", map[string]any{"block_id": dev}), h.screen(dev), h.logs("dev-panes"))
		}
	}
	type_("printf -- '--- F''AIL: TestX\\nok  other 0.1s\\n'\r")
	h.until("fail mark kept past ok lines", func() bool {
		b := h.badges(dev)
		return len(b) == 1 && strings.Contains(b[0].Msg, "FAIL: TestX")
	})
	h.call("hive.terminal.write", map[string]any{"block_id": blocked, "args": map[string]any{"data": "panic: in an agent\n"}})
	time.Sleep(500 * time.Millisecond)
	if len(h.badges(blocked)) != 0 {
		t.Fatal("marked an agent's pane")
	}
}
