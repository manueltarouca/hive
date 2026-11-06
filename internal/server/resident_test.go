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

type residentHarness struct {
	*harness
	dir string
}

func startScripts(t *testing.T, files map[string]string, adjust func(*Config)) *residentHarness {
	t.Helper()
	dir := t.TempDir()
	for name, code := range files {
		writeScript(t, dir, name, code)
	}
	h := startCfg(t, t.TempDir(), func(c *Config) {
		c.ScriptsDir, c.ConfigPoll = dir, 30*time.Millisecond
		if adjust != nil {
			adjust(c)
		}
	})
	return &residentHarness{h, dir}
}

func writeScript(t *testing.T, dir, name, code string) {
	t.Helper()
	p := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *residentHarness) script(name string) script.Resident {
	h.t.Helper()
	var out struct {
		Scripts []script.Resident `json:"scripts"`
	}
	raw, _ := json.Marshal(h.call("script.list", nil))
	_ = json.Unmarshal(raw, &out)
	for _, s := range out.Scripts {
		if s.Name == name {
			return s
		}
	}
	return script.Resident{}
}

func (h *residentHarness) until(what string, cond func() bool) {
	h.t.Helper()
	// Generous: a script's start (bundle, runtime, setup) is slow on a loaded CI runner.
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	// Say why: each script's state and log, which a CI run otherwise loses.
	raw, _ := json.Marshal(h.call("script.list", nil))
	h.t.Logf("scripts: %s", raw)
	var out struct {
		Scripts []struct{ Name string } `json:"scripts"`
	}
	_ = json.Unmarshal(raw, &out)
	for _, sc := range out.Scripts {
		h.t.Logf("%s log:\n%s", sc.Name, h.logs(sc.Name))
	}
	h.t.Fatalf("timed out: %s", what)
}

func (h *residentHarness) logs(name string) string {
	out := h.call("script.logs", map[string]any{"name": name})
	var b strings.Builder
	for _, l := range out["lines"].([]any) {
		b.WriteString(l.(map[string]any)["text"].(string) + "\n")
	}
	return b.String()
}

const labeler = `export default hive.script({
  name: "labeler",
  permissions: ["control"],
  async setup(h) {
    const n = ((await h.store.get("loads")) ?? 0) + 1;
    await h.store.set("loads", n);
    console.log("load " + n);
    h.on("session_created", async (ctx, ev) => {
      await h.session.set_label({ session_id: ev.session.session_id, label: "LABEL" });
    });
    h.on("server_started", () => console.log("server up"));
  },
});
`

// A script from the folder waits for a grant, then runs, reacts to events,
// keeps its storage across reloads, and survives a broken edit.
func TestResidentScriptLifecycle(t *testing.T) {
	h := startScripts(t, map[string]string{"labeler.ts": strings.ReplaceAll(labeler, "LABEL", "auto")}, nil)
	defer h.stop()

	h.until("pending", func() bool { return h.script("labeler").State == script.StatePending })
	if s := h.script("labeler"); strings.Join(s.Asks, ",") != "control" {
		t.Fatalf("asks = %v", s.Asks)
	}
	h.call("script.grant", map[string]any{"name": "labeler"})
	h.until("running", func() bool { return h.script("labeler").State == script.StateRunning })

	label := func() string {
		c := h.call("session.create", map[string]any{"command": []string{"cat"}})
		sid := c["session_id"].(string)
		var got string
		h.until("labelled", func() bool {
			got, _ = h.call("session.view", map[string]any{"session_id": sid})["label"].(string)
			return got != "" && !strings.Contains(got, "-") // generated names have dashes
		})
		return got
	}
	if got := label(); got != "auto" {
		t.Fatalf("label = %q", got)
	}

	// An edit reloads it; storage survives.
	writeScript(t, h.dir, "labeler.ts", strings.ReplaceAll(labeler, "LABEL", "auto2"))
	h.until("reloaded", func() bool { return strings.Contains(h.logs("labeler"), "load 2") })
	if got := label(); got != "auto2" {
		t.Fatalf("after reload, label = %q", got)
	}

	// A broken edit keeps the old version running.
	writeScript(t, h.dir, "labeler.ts", "export default hive.script({ name: ")
	h.until("error noted", func() bool { return h.script("labeler").Error != "" })
	if s := h.script("labeler"); s.State != script.StateRunning {
		t.Fatalf("broken edit: %+v", s)
	}
	if got := label(); got != "auto2" {
		t.Fatalf("after a broken edit, label = %q", got)
	}

	// Asking for more needs a new grant.
	writeScript(t, h.dir, "labeler.ts", strings.ReplaceAll(strings.ReplaceAll(labeler, "LABEL", "auto3"), `["control"]`, `["control", "input"]`))
	h.until("pending again", func() bool { return h.script("labeler").State == script.StatePending })

	// Disabled, it stops; removed, it's gone.
	h.call("script.disable", map[string]any{"name": "labeler"})
	if s := h.script("labeler"); s.State != script.StateDisabled {
		t.Fatalf("disabled: %+v", s)
	}
	os.Remove(filepath.Join(h.dir, "labeler.ts"))
	h.until("removed", func() bool { return h.script("labeler").Name == "" })
}

// Scripts load after sessions are restored and hear server_started.
func TestResidentScriptHearsStart(t *testing.T) {
	state := t.TempDir()
	dir := t.TempDir()
	writeScript(t, dir, "hello.ts", `export default hive.script({ name: "hello", permissions: [],
  setup(h) { h.on("restore_finished", (ctx, ev) => console.log("restored " + ev.sessions)); } });`)
	os.MkdirAll(filepath.Join(state, "scripts"), 0o700)
	os.WriteFile(filepath.Join(state, "scripts", "grants.json"), []byte(`{"hello":{"permissions":[],"hash":"`+permsHashFor()+`"}}`), 0o600)

	h := startCfg(t, state, func(c *Config) { c.ScriptsDir, c.ConfigPoll = dir, 30*time.Millisecond })
	h.call("session.create", map[string]any{"command": []string{"cat"}})
	h.stop()
	h = startCfg(t, state, func(c *Config) { c.ScriptsDir, c.ConfigPoll = dir, 30*time.Millisecond })
	defer h.stop()
	rh := &residentHarness{h, dir}
	rh.until("restored", func() bool { return strings.Contains(rh.logs("hello"), "restored 1") })
}

func permsHashFor() string {
	// The hash of an empty permission list, as grants.json stores it.
	return "e3b0c44298fc1c14"
}

// A script whose handlers keep running over their budget is quarantined;
// enable brings it back.
func TestResidentScriptQuarantine(t *testing.T) {
	h := startScripts(t, map[string]string{"spin.ts": `export default hive.script({ name: "spin", permissions: [],
  setup(h) { h.every("10ms", () => { while (true) {} }); } });`}, func(c *Config) { c.scriptBudget = 20 * time.Millisecond })
	defer h.stop()
	h.until("pending", func() bool { return h.script("spin").State == script.StatePending })
	h.call("script.grant", map[string]any{"name": "spin"})
	h.until("quarantined", func() bool { return h.script("spin").State == script.StateQuarantined })
	if s := h.script("spin"); !strings.Contains(s.Error, "ran over") {
		t.Fatalf("quarantine: %+v", s)
	}
	h.call("script.enable", map[string]any{"name": "spin"})
	if s := h.script("spin"); s.State != script.StateRunning {
		t.Fatalf("enabled: %+v", s)
	}
}

// A file without a manifest fails with a reason, and lib/ isn't loaded.
func TestResidentScriptNeedsManifest(t *testing.T) {
	h := startScripts(t, map[string]string{
		"plain.ts":   `import { x } from "./lib/x"; console.log(x);`,
		"lib/x.ts":   `export const x = 1;`,
		"types.d.ts": `declare const y: number;`,
	}, nil)
	defer h.stop()
	h.until("failed", func() bool { return h.script("plain").State == script.StateFailed })
	if s := h.script("plain"); !strings.Contains(s.Error, "hive.script") {
		t.Fatalf("plain: %+v", s)
	}
	if h.script("lib").Name != "" || h.script("x").Name != "" || h.script("types").Name != "" {
		t.Fatal("lib/ or a .d.ts was loaded as a script")
	}
}
