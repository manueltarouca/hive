package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/script"
)

// A run's name can't reach outside the state folder, and a one-shot's
// storage is never a resident's.
func TestScriptNamesAndStorageStayApart(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	js, _ := script.Bundle("", `export default async () => { await h.store.set("k", 1) }`, t.TempDir())
	if e := h.callErr("script.run", map[string]any{"code": js, "name": "../../../../tmp/escape"}); e.Code != "bad_request" {
		t.Fatalf("traversal: %v", e)
	}
	r := h.call("script.run", map[string]any{"code": js, "name": "labeler"})
	h.call("script.result", map[string]any{"run_id": r["run_id"], "timeout": 5})
	if _, err := os.Stat(filepath.Join(h.state, "scripts", "labeler.store.json")); err == nil {
		t.Fatal("a one-shot wrote the resident's storage")
	}
	if _, err := os.Stat(filepath.Join(h.state, "scripts", "oneshot", "labeler.store.json")); err != nil {
		t.Fatalf("one-shot storage: %v", err)
	}
	// A manifest's name doesn't rename the run either.
	js, _ = script.Bundle("", `export default hive.script({ name: "../../x", permissions: [], async setup(h) { await h.store.set("k", 1) } })`, t.TempDir())
	r = h.call("script.run", map[string]any{"code": js})
	h.call("script.result", map[string]any{"run_id": r["run_id"], "timeout": 5})
	if _, err := os.Stat(filepath.Join(h.state, "x.store.json")); err == nil {
		t.Fatal("a manifest name escaped")
	}
}

// Code that isn't a bundle runs on the script's loop, where stop reaches it.
func TestRawCodeIsStoppable(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	r := h.call("script.run", map[string]any{"code": "while(true){}"})
	time.Sleep(50 * time.Millisecond)
	t0 := time.Now()
	h.call("script.stop", map[string]any{"run_id": r["run_id"]})
	res := h.call("script.result", map[string]any{"run_id": r["run_id"], "timeout": 5})
	if res["state"] != "ended" || time.Since(t0) > time.Second {
		t.Fatalf("res = %v after %v", res, time.Since(t0))
	}
	r = h.call("script.run", map[string]any{"code": "1 + 1"})
	res = h.call("script.result", map[string]any{"run_id": r["run_id"], "timeout": 5})
	if !strings.Contains(res["error"].(string), "isn't a bundle") {
		t.Fatalf("not a bundle: %v", res)
	}
}

// Runs are capped, a script's children stop with it, and scripts hear
// other scripts end.
func TestRunCapChildrenAndScriptEvents(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	forever, _ := script.Bundle("", `h.every("1s", () => {})`, t.TempDir())
	b, _ := json.Marshal(forever)
	out := h.runScript(`
const ended = h.wait("script_ended", {}, { timeout: "5s" });
const quick = await h.script.run({ code: `+string(mustBundle(t, `export default () => 1`))+` });
const ev: any = await ended;
const child = await h.script.run({ code: `+string(b)+` });
h.after("10ms", () => h.stop());
export default () => (ev.run_id === quick.run_id) + " " + child.run_id;`, nil)
	parts := strings.Fields(out.str(t))
	if parts[0] != "true" {
		t.Fatalf("script_ended not heard: %+v", out)
	}
	res := h.call("script.result", map[string]any{"run_id": parts[1], "timeout": 5})
	if res["state"] != "ended" {
		t.Fatalf("child still %v", res["state"])
	}
	var ids []string
	for i := 0; ; i++ {
		var r map[string]any
		err := h.c.Call(t.Context(), "script.run", map[string]any{"code": forever}, &r)
		if err != nil {
			if !strings.Contains(err.Error(), "running already") || i < 60 {
				t.Fatalf("run %d: %v", i, err)
			}
			break
		}
		ids = append(ids, r["run_id"].(string))
	}
	for _, id := range ids {
		h.call("script.stop", map[string]any{"run_id": id})
	}
}

func mustBundle(t *testing.T, code string) []byte {
	js, err := script.Bundle("", code, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(js)
	return b
}

// A resident that stops itself says so; a quarantined one stays put when
// its file changes.
func TestResidentStopsItselfAndQuarantineHolds(t *testing.T) {
	h := startScripts(t, map[string]string{
		"quitter.ts": `export default hive.script({ name: "quitter", permissions: [], setup(h) { h.after("50ms", () => h.stop()); } });`,
		"spin.ts":    `export default hive.script({ name: "spin", permissions: [], setup(h) { h.every("10ms", () => { while (true) {} }); } });`,
	}, func(c *Config) { c.scriptBudget = 20 * time.Millisecond })
	defer h.stop()
	for _, n := range []string{"quitter", "spin"} {
		h.until(n+" pending", func() bool { return h.script(n).State == script.StatePending })
		h.call("script.grant", map[string]any{"name": n})
	}
	h.until("stopped", func() bool { return h.script("quitter").State == script.StateStopped })
	h.until("quarantined", func() bool { return h.script("spin").State == script.StateQuarantined })
	writeScript(t, h.dir, "spin.ts", `export default hive.script({ name: "spin", permissions: [], setup(h) {} });`)
	time.Sleep(300 * time.Millisecond)
	if s := h.script("spin"); s.State != script.StateQuarantined {
		t.Fatalf("an edit lifted the quarantine: %+v", s)
	}
}
