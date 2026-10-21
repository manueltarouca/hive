package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/remote"
	"github.com/manueltarouca/hive/internal/script"
)

type scriptOut struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
	Code   string          `json:"code"`
	State  string          `json:"state"`
	logs   []string
}

// runScript bundles TypeScript, runs it on the server and waits for it,
// collecting its log.
func (h *harness) runScript(code string, params map[string]any) scriptOut {
	h.t.Helper()
	js, err := script.Bundle("", code, h.t.TempDir())
	if err != nil {
		h.t.Fatalf("bundle: %v", err)
	}
	if params == nil {
		params = map[string]any{}
	}
	params["code"] = js
	started := h.call("script.run", params)
	runID := started["run_id"].(string)
	var mu sync.Mutex
	var logs []string
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		_ = h.c.EventsOf(ctx, []string{"script_log", "script_ended"}, int64(started["seq"].(float64)), func(e client.Event) bool {
			var m map[string]any
			_ = json.Unmarshal(e.Payload, &m)
			if m["run_id"] != runID {
				return true
			}
			if e.Type == "script_log" {
				mu.Lock()
				logs = append(logs, m["level"].(string)+" "+m["text"].(string))
				mu.Unlock()
			}
			return e.Type != "script_ended"
		})
	}()
	var out scriptOut
	raw, err := h.c.CallRaw(context.Background(), "script.result", map[string]any{"run_id": runID, "timeout": 15})
	if err != nil {
		h.t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &out)
	if out.State != "ended" {
		h.t.Fatalf("run %s still %s", runID, out.State)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		h.t.Fatal("no script_ended event")
	}
	mu.Lock()
	out.logs = logs
	mu.Unlock()
	return out
}

func (o scriptOut) str(t *testing.T) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(o.Result, &s); err != nil {
		t.Fatalf("result %s (error %q): %v", o.Result, o.Error, err)
	}
	return s
}

func TestScriptCallsAndReturns(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	h.call("session.create", map[string]any{"label": "one", "command": []string{"cat"}})
	out := h.runScript(`
const list = await h.session.list();
console.log("sessions:", list.sessions.length);
const labels: string[] = [];
await Promise.all([1, 2, 3].map(async () => (await h.call("session.list")).sessions.length));
export default async function main(hh: any) {
  for (const s of (await hh.session.list()).sessions) labels.push(s.label);
  return labels.join(",") + " " + hh.args.who;
}`, map[string]any{"args": map[string]any{"who": "mt"}})
	if !out.OK || out.str(t) != "one mt" {
		t.Fatalf("out = %+v", out)
	}
	if len(out.logs) != 1 || out.logs[0] != "info sessions: 1" {
		t.Fatalf("logs = %q", out.logs)
	}
}

func TestScriptErrorsAndPermissions(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)
	bid := strs(created["block_ids"])[0]
	ctx := map[string]any{"session_id": sid, "block_id": bid}

	// Read only, by default.
	out := h.runScript(`try { await h.session.create({}) } catch (e: any) { console.log(e.code, e.status) }`, nil)
	if !out.OK || len(out.logs) != 1 || out.logs[0] != "info permission_denied 403" {
		t.Fatalf("out = %+v", out)
	}

	// control: the default shell in any folder, yes; a program, no;
	// typing, no. IDs come from the context.
	matrix := `
export default async () => {
  const r: string[] = [];
  const t = async (name: string, f: () => Promise<unknown>) => {
    try { await f(); r.push(name + " ok") } catch (e: any) { r.push(name + " " + e.code) }
  };
  await t("shell", () => h.session.new_split({ layout: { cwd: "/tmp" }, focus: false }));
  await t("program", () => h.session.new_split({ layout: { command: ["cat"] } }));
  await t("create", () => h.session.create({ command: ["sh", "-c", "true"] }));
  await t("write", () => h.call("block.method", { method: "write", args: { data: "x" } }));
  await t("write2", () => h.terminal.write({ args: { data: "x" } }));
  await t("format", () => h.terminal.format({}));
  return r.join("; ");
}`
	out = h.runScript(matrix, map[string]any{"allow": []string{"control"}, "session_id": sid, "block_id": bid})
	want := "shell ok; program permission_denied; create permission_denied; write permission_denied; write2 permission_denied; format permission_denied"
	if got := out.str(t); got != want {
		t.Fatalf("control:\n got %s\nwant %s", got, want)
	}
	out = h.runScript(matrix, map[string]any{"allow": []string{"spawn", "input", "screen"}, "session_id": sid, "block_id": bid})
	if got := out.str(t); strings.Contains(got, "denied") {
		t.Fatalf("spawn, input, screen: %s", got)
	}
	_ = ctx

	// An uncaught error ends the run with its message.
	out = h.runScript(`throw new Error("boom")`, nil)
	if out.OK || !strings.Contains(out.Error, "boom") {
		t.Fatalf("throw: %+v", out)
	}
	// A manifest declares the permissions.
	out = h.runScript(`export default hive.script({ name: "m", permissions: ["control"],
  async setup(h) { await h.session.set_label({ label: "set by m" }); } })`, map[string]any{"session_id": sid})
	if !out.OK {
		t.Fatalf("manifest: %+v", out)
	}
	if v := h.call("session.view", map[string]any{"session_id": sid}); v["label"] != "set by m" {
		t.Fatalf("label = %v", v["label"])
	}
	// Changes are audited.
	audit, _ := os.ReadFile(filepath.Join(h.state, "scripts", "audit.jsonl"))
	if !strings.Contains(string(audit), `"principal":"script:m"`) || !strings.Contains(string(audit), `"method":"session.set_label"`) {
		t.Fatalf("audit:\n%s", audit)
	}
}

// A script can't start another with more than it has.
func TestNestedScriptCannotWiden(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	inner, err := script.Bundle("", `export default async () => {
  try { await h.session.create({ command: ["true"] }); return "spawned" } catch (e: any) { return e.code } }`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(inner)
	out := h.runScript(`export default async () => {
  const r = await h.script.run({ code: `+string(b)+`, allow: ["spawn"] });
  const res = await h.script.result({ run_id: r.run_id, timeout: 10 });
  return res.result;
}`, map[string]any{"allow": []string{"read"}})
	if got := out.str(t); got != "permission_denied" {
		t.Fatalf("nested = %q (%+v)", got, out)
	}
}

func TestScriptWaitsAndEvents(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)
	bid := strs(created["block_ids"])[0]
	// A program that exits at once: the wait must not miss it.
	out := h.runScript(`
const b = await h.block();
const [split] = await b.new_split({ layout: { command: ["sh", "-c", "exit 3"] }, focus: false });
const r = await split.wait({ timeout: "5s" });
const ev = await h.wait("terminal.title_changed", { block_id: b.id }, { timeout: "50ms" }).catch((e: any) => e.code);
export default () => "exit " + r.exit_code + " " + ev;
`, map[string]any{"allow": []string{"spawn"}, "session_id": sid, "block_id": bid})
	if got := out.str(t); got != "exit 3 timeout" {
		t.Fatalf("got %q (%+v)", got, out)
	}
	// Handlers run until stopped; h.stop ends a run cleanly.
	out = h.runScript(`
let n = 0;
h.on("session_label_changed", {}, (ctx: any, ev: any) => { n++; if (ev.label === "stop") { console.log(ctx.event, n); h.stop(); } }, { self: true });
await h.session.set_label({ label: "a" });
await h.session.set_label({ label: "stop" });
`, map[string]any{"allow": []string{"control"}, "session_id": sid})
	if !out.OK || len(out.logs) != 1 || out.logs[0] != "info session_label_changed 2" {
		t.Fatalf("on: %+v", out)
	}
	// Timers.
	out = h.runScript(`
let ticks = 0;
await new Promise<void>((done) => { const t = h.every("10ms", () => { if (++ticks === 3) { t.cancel(); done(); } }) });
await h.sleep("5ms");
export default () => ticks;`, nil)
	if string(out.Result) != "3" {
		t.Fatalf("timers: %+v", out)
	}
}

func TestScriptStop(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	js, _ := script.Bundle("", `while (true) {}`, t.TempDir())
	r := h.call("script.run", map[string]any{"code": js})
	time.Sleep(50 * time.Millisecond)
	t0 := time.Now()
	h.call("script.stop", map[string]any{"run_id": r["run_id"]})
	res := h.call("script.result", map[string]any{"run_id": r["run_id"], "timeout": 5})
	if res["state"] != "ended" || res["code"] != "script_unloaded" || time.Since(t0) > time.Second {
		t.Fatalf("res = %v after %v", res, time.Since(t0))
	}
	// --for ends it on its own.
	r = h.call("script.run", map[string]any{"code": js, "for": 0.1})
	res = h.call("script.result", map[string]any{"run_id": r["run_id"], "timeout": 5})
	if res["state"] != "ended" || !strings.Contains(res["error"].(string), "--for") {
		t.Fatalf("for: %v", res)
	}
}

func TestScriptRunRefusedRemotely(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	js, _ := script.Bundle("", `1`, t.TempDir())
	raw, _ := json.Marshal(map[string]any{"code": js})
	ctx := remote.WithIdentity(context.Background(), remote.Identity{Principal: "user:x", Via: "tailnet"})
	_, err := h.srv.reg.Call(ctx, "script.run", raw)
	var e interface{ Error() string }
	if !errors.As(err, &e) || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("err = %v", err)
	}
}

// The examples in docs/scripts.md build, and the ones that act work.
func TestScriptDocsExamples(t *testing.T) {
	doc, err := os.ReadFile("../../docs/scripts.md")
	if err != nil {
		t.Fatal(err)
	}
	var blocks []string
	for _, part := range strings.Split(string(doc), "```ts\n")[1:] {
		code, _, _ := strings.Cut(part, "```")
		blocks = append(blocks, code)
	}
	if len(blocks) < 3 {
		t.Fatalf("%d examples", len(blocks))
	}
	for i, code := range blocks {
		if _, err := script.Bundle("", code, t.TempDir()); err != nil {
			t.Errorf("example %d: %v\n%s", i+1, err, code)
		}
	}
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	dir := t.TempDir()
	created := h.call("session.create", map[string]any{"cwd": dir})
	sid := created["session_id"].(string)
	bid := strs(created["block_ids"])[0]
	at := map[string]any{"session_id": sid, "block_id": bid}

	// The first: tests in a split. make isn't needed: a Makefile-free folder
	// fails, which is a result too.
	out := h.runScript(blocks[0], map[string]any{"allow": []string{"spawn"}, "session_id": sid, "block_id": bid})
	if !out.OK || len(out.logs) != 1 || !strings.HasPrefix(out.logs[0], "info tests ") {
		t.Fatalf("tests example: %+v", out)
	}
	// The manifest example labels the pane by its folder.
	h.waitScreen(sid, bid, "$")
	for i, code := range blocks {
		if strings.Contains(code, "label-by-folder") {
			out = h.runScript(code, at)
			if !out.OK {
				t.Fatalf("example %d: %+v", i+1, out)
			}
		}
	}
	d := h.call("session.describe_block", map[string]any{"session_id": sid, "block_id": bid})
	if d["label"] != filepath.Base(dir) {
		t.Fatalf("label = %v, want %s", d["label"], filepath.Base(dir))
	}
}

// hive.d.ts is generated from the registry; with a TypeScript compiler on
// hand (HIVE_TSC, or tsc on PATH), it and the docs' examples type-check.
func TestScriptTypes(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	var methods []script.MethodSchema
	raw, err := h.c.CallRaw(context.Background(), "api.list", map[string]any{"full": true})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &methods); err != nil {
		t.Fatal(err)
	}
	dts := script.Types("test", methods)
	for _, want := range []string{
		`"session.new_split": { args: {`,
		"new_split(args: Methods[\"session.new_split\"][\"args\"])",
		"format(args: Methods[\"hive.terminal.format\"][\"args\"])",
		"list(args?: Methods[\"session.list\"][\"args\"])",
		`direction?: "horizontal" | "vertical";`,
		"Permission: control; spawn when it runs a command",
	} {
		if !strings.Contains(dts, want) {
			t.Errorf("hive.d.ts lacks %s", want)
		}
	}
	tsc := os.Getenv("HIVE_TSC")
	if tsc == "" {
		if p, err := exec.LookPath("tsc"); err == nil {
			tsc = p
		}
	}
	if tsc == "" {
		t.Skip("no TypeScript compiler: set HIVE_TSC to check the types")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hive.d.ts"), []byte(dts), 0o644)
	doc, _ := os.ReadFile("../../docs/scripts.md")
	files := []string{"hive.d.ts"}
	for i, part := range strings.Split(string(doc), "```ts\n")[1:] {
		code, _, _ := strings.Cut(part, "```")
		name := fmt.Sprintf("example%d.ts", i+1)
		os.WriteFile(filepath.Join(dir, name), []byte(code+"\nexport {};\n"), 0o644)
		files = append(files, name)
	}
	cfg, _ := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "es2022", "module": "esnext", "moduleResolution": "bundler",
			"strict": true, "noEmit": true, "useUnknownInCatchVariables": false, "lib": []string{"es2022"}},
		"files": files,
	})
	os.WriteFile(filepath.Join(dir, "tsconfig.json"), cfg, 0o644)
	cmd := exec.Command(tsc, "-p", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
	// And they catch mistakes: a misspelled argument, a wrong type.
	os.WriteFile(filepath.Join(dir, "bad.ts"), []byte(`
await h.session.new_split({ block_id: "b", directoin: "vertical" });
await h.session.set_label({ session_id: "s", label: 3 });
const n: number = (await h.session.list()).sessions[0].label;
export {};
`), 0o644)
	cfg, _ = json.Marshal(map[string]any{
		"compilerOptions": map[string]any{"target": "es2022", "module": "esnext", "strict": true, "noEmit": true, "lib": []string{"es2022"}},
		"files":           []string{"hive.d.ts", "bad.ts"},
	})
	os.WriteFile(filepath.Join(dir, "tsconfig.json"), cfg, 0o644)
	out, err := exec.Command(tsc, "-p", dir).CombinedOutput()
	if err == nil || strings.Count(string(out), "error TS") != 3 {
		t.Fatalf("bad.ts should fail with 3 errors:\n%s", out)
	}
}

// A script's own changes don't wake its own handlers, unless it asks; two
// scripts waking each other stop at depth 8.
func TestScriptCausesAndLoops(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)

	out := h.runScript(`
let mine = 0;
const sub = h.on("session_label_changed", () => { mine++ });
await h.session.set_label({ label: "x" });
await h.sleep("100ms");
sub.cancel();
export default () => mine;`, map[string]any{"allow": []string{"control"}, "session_id": sid})
	if string(out.Result) != "0" {
		t.Fatalf("own change woke its handler: %+v", out)
	}

	pingpong := func(tag string) string {
		js, err := script.Bundle("", `
h.on("session_label_changed", async (ctx: any, ev: any) => {
  if (ev.label.startsWith("`+tag+`")) return;
  await h.session.set_label({ label: "`+tag+`" + ctx.depth });
});`, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		r := h.call("script.run", map[string]any{"code": js, "allow": []string{"control"}, "session_id": sid, "for": 1.5})
		return r["run_id"].(string)
	}
	a, b := pingpong("a"), pingpong("b")
	h.call("session.set_label", map[string]any{"session_id": sid, "label": "go"})
	for _, id := range []string{a, b} {
		h.call("script.result", map[string]any{"run_id": id, "timeout": 10})
	}
	label := h.call("session.view", map[string]any{"session_id": sid})["label"].(string)
	if label != "a7" && label != "b7" {
		t.Fatalf("the chain ended at %q, want depth 7", label)
	}
}
