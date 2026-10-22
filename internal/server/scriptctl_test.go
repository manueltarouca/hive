package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/script"
)

// typer types into a pane when its action runs, and reads its switch at
// that moment.
const typer = `export default hive.script({
  name: "typer",
  permissions: ["input", "ui"],
  options: {
    auto: { title: "Type by itself", default: true },
    times: { title: "How many", type: "number", default: 2, min: 1, max: 5 },
  },
  setup(h) {
    h.actions.register({ name: "type", run: async (ctx, a) => {
      if (!h.options.auto) return "skipped";
      await h.terminal.write({ block_id: a.block, args: { data: "resume\r" } });
      return "typed " + h.options.times;
    } });
    h.on("options_changed", (ctx, ev) => console.log("option " + ev.key + " is now " + ev.value));
    h.actions.register({ name: "offer", run: async (ctx, a) => {
      const n = await h.notify.send({ title: "Typed resume", actions: [
        { title: "Stop typing", action: "hive.set_option", args: { key: "auto", value: false } },
        { title: "Watch only", action: "hive.set_mode", args: { mode: "watch" } },
      ] });
      return n.notification_id;
    } });
    h.actions.register({ name: "offer-bad", run: async () => {
      try {
        await h.notify.send({ title: "x", actions: [{ title: "Dismiss", action: "hive.set_option", args: { key: "auto", value: "false" } }] });
        return "sent";
      } catch (e) { return e.code; }
    } });
    h.actions.register({ name: "self-on", run: async () => {
      await h.script.set_option({ name: "typer", key: "auto", value: true });
    } });
  },
});`

func (h *residentHarness) activity(name string) []script.Entry {
	h.t.Helper()
	var out struct {
		Entries []script.Entry `json:"entries"`
	}
	raw, _ := json.Marshal(h.call("audit.list", map[string]any{"name": name}))
	_ = json.Unmarshal(raw, &out)
	return out.Entries
}

func (h *harness) callAny(method string, params any) any {
	h.t.Helper()
	var out any
	if err := h.c.Call(context.Background(), method, params, &out); err != nil {
		h.t.Fatalf("%s: %v", method, err)
	}
	return out
}

func TestScriptsAreSeenAndCanBeTurnedOff(t *testing.T) {
	h := startScripts(t, map[string]string{"typer.ts": typer}, nil)
	defer h.stop()
	h.until("typer pending", func() bool { return h.script("typer").State == script.StatePending })
	h.call("script.grant", map[string]any{"name": "typer"})
	h.until("typer running", func() bool { return h.script("typer").State == script.StateRunning })
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	bid := strs(created["block_ids"])[0]

	// Its switches show, with their values.
	res := h.script("typer")
	if res.Mode != script.ModeAct || len(res.Options) != 2 || res.Options[0].Key != "auto" || res.Options[0].Value != true {
		t.Fatalf("script = %+v", res)
	}

	// What it does is listed, with what it typed for the owner.
	if out := h.callAny("action.run", map[string]any{"name": "type", "args": map[string]any{"block": bid}}); out != "typed 2" {
		t.Fatalf("type = %v", out)
	}
	acts := h.activity("typer")
	last := acts[len(acts)-1]
	if last.Method != "hive.terminal.write" || last.BlockID != bid || last.Preview != "resume⏎" || last.Result != "ok" ||
		!strings.Contains(last.Summary, "typed 7 characters") || last.Script != "typer" {
		t.Fatalf("activity = %+v", last)
	}
	// The file on disk keeps only a hash of it.
	if b, _ := os.ReadFile(filepath.Join(h.state, "scripts", "audit.jsonl")); strings.Contains(string(b), "resume") {
		t.Fatalf("typed text on disk: %s", b)
	}

	// A switch turned off is read at the next act.
	h.call("script.set_option", map[string]any{"name": "typer", "key": "auto", "value": false})
	if out := h.callAny("action.run", map[string]any{"name": "type", "args": map[string]any{"block": bid}}); out != "skipped" {
		t.Fatalf("typed with auto off: %v", out)
	}
	h.until("options_changed heard", func() bool { return strings.Contains(h.logs("typer"), "option auto is now false") })
	// Bad values are refused; values survive a reload.
	if e := h.callErr("script.set_option", map[string]any{"name": "typer", "key": "times", "value": 9}); e == nil || e.Code != "bad_request" {
		t.Fatalf("times=9: %v", e)
	}
	h.call("script.reload", map[string]any{"name": "typer"})
	if v := h.script("typer").Options[0].Value; v != false {
		t.Fatalf("auto after reload = %v", v)
	}

	// A script can't turn itself back on.
	if e := h.callErr("action.run", map[string]any{"name": "self-on"}); e == nil || !strings.Contains(e.Message, "permission") {
		t.Fatalf("self-on: %v", e)
	}
	// Not even one allowed everything.
	all := h.runScript(`
const r = [];
for (const [m, a] of [["script.set_option", { name: "typer", key: "auto", value: true }], ["script.set_mode", { name: "typer", mode: "act" }],
                      ["script.enable", { name: "typer" }], ["script.grant", { name: "typer" }]]) {
  try { await h.call(m, a); r.push("ok"); } catch (e) { r.push(e.message.includes("scripts can't") ? "refused" : e.message); }
}
export default () => r;`, map[string]any{"allow": []string{"server", "control", "input", "ui", "config", "spawn"}})
	if got := string(all.Result); got != `["refused","refused","refused","refused"]` {
		t.Fatalf("a script managing scripts: %s", got)
	}

	// Watch mode: the change is refused and listed, whatever the code does.
	h.call("script.set_option", map[string]any{"name": "typer", "key": "auto", "value": true})
	h.call("script.set_mode", map[string]any{"name": "typer", "mode": "watch"})
	if e := h.callErr("action.run", map[string]any{"name": "type", "args": map[string]any{"block": bid}}); e == nil {
		t.Fatal("typed in watch mode")
	}
	acts = h.activity("typer")
	if last := acts[len(acts)-1]; last.Method != "hive.terminal.write" || last.Result != "watching" {
		t.Fatalf("watch entry = %+v", last)
	}
	if h.script("typer").Mode != script.ModeWatch {
		t.Fatal("mode not shown")
	}

	// The notification's buttons are run by the server, for the person.
	h.call("script.set_mode", map[string]any{"name": "typer", "mode": "act"})
	if out := h.callAny("action.run", map[string]any{"name": "offer-bad"}); out != "bad_request" {
		t.Fatalf("a button with a wrong value: %v", out)
	}
	noteID, _ := h.callAny("action.run", map[string]any{"name": "offer"}).(string)
	var offered *Notification
	for _, n := range h.notes() {
		if n.ID == noteID {
			n := n
			offered = &n
		}
	}
	// What the buttons do is the server's to say, not the script's.
	if offered == nil || offered.Actions[0].Step != `Sets typer's "Type by itself" to off` ||
		!strings.Contains(offered.Actions[1].Step, "will only watch") {
		t.Fatalf("buttons = %+v", offered)
	}
	h.callAny("action.run", map[string]any{"name": "hive.set_option", "notification_id": noteID})
	if v := h.script("typer").Options[0].Value; v != false {
		t.Fatalf("button didn't turn auto off: %v", v)
	}
	acts = h.activity("typer")
	if last := acts[len(acts)-1]; last.Method != "script.set_option" || !strings.Contains(last.Principal, "notification button") {
		t.Fatalf("button entry = %+v", last)
	}
	h.callAny("action.run", map[string]any{"name": "hive.set_mode", "notification_id": noteID})
	if h.script("typer").Mode != script.ModeWatch {
		t.Fatal("button didn't set watch")
	}
	// Turning off is listed too.
	h.call("script.disable", map[string]any{"name": "typer"})
	acts = h.activity("typer")
	if last := acts[len(acts)-1]; last.Summary != "turned off" {
		t.Fatalf("disable entry = %+v", last)
	}
}

// A script with ui alone can show things in the app, not change your
// layout, and can't hide another's notifications.
func TestUIScriptsCanOnlyShow(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	other := h.call("notify.send", map[string]any{"title": "from the cli"})
	out := h.runScript(`
const r = [];
try { await h.client.perform({ client_id: "c1", action: "client.close_pane" }); r.push("closed"); } catch (e) { r.push(e.code); }
try { await h.client.perform({ client_id: "c1", action: "client.reveal" }); r.push("revealed"); } catch (e) { r.push(e.code); }
try { await h.notification.ack({ notification_id: "`+other["notification_id"].(string)+`" }); r.push("acked"); } catch (e) { r.push(e.code); }
export default () => r;`, map[string]any{"allow": []string{"ui"}})
	if got := string(out.Result); got != `["permission_denied","not_found","permission_denied"]` {
		t.Fatalf("ui script = %s", got)
	}
}

// A script doesn't hear its own activity: in watch mode, a handler that
// acts on script_acted would otherwise be refused, hear that, and loop.
func TestScriptDoesNotHearItsOwnActivity(t *testing.T) {
	h := startScripts(t, map[string]string{"loopy.ts": `export default hive.script({
  name: "loopy", permissions: ["input"],
  setup(h) {
    let n = 0;
    h.on("script_acted", async () => {
      n++;
      await h.store.set("heard", n);
      await h.terminal.write({ block_id: "nope", args: { data: "x" } }).catch(() => {});
    });
    h.actions.register({ name: "poke", run: async () => { await h.terminal.write({ block_id: "nope", args: { data: "x" } }).catch(() => {}); } });
  },
});`}, nil)
	defer h.stop()
	grantStarter(t, h, "loopy")
	h.call("script.set_mode", map[string]any{"name": "loopy", "mode": "watch"})
	h.callAny("action.run", map[string]any{"name": "poke"})
	// It hears people's changes (grant, mode) and its poke, then stops.
	time.Sleep(500 * time.Millisecond)
	n := len(h.activity("loopy"))
	time.Sleep(500 * time.Millisecond)
	if m := len(h.activity("loopy")); m != n || n > 6 {
		t.Fatalf("it kept waking itself: %d, then %d entries", n, m)
	}
}
