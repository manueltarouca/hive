package server

import (
	"strings"
	"testing"
)

// Output triggers hear lines as programs print them: split across reads,
// with colors, without a newline once output goes quiet, and only once
// for a redraw that repeats the same line.
func TestOutputTriggers(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	c := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := c["session_id"].(string)

	out := h.runScript(`
const seen: string[] = [];
const sub = h.trigger({ pattern: "(FAIL|panic): (\\w+)" }, (ctx: any, m: any) => { seen.push(m.groups.join(" ")) });
const port = h.wait_output({ pattern: "Listening on :(\\d+)", timeout: "5s" });
const [b] = await (await h.block()).new_split({ focus: false, layout: { command: ["sh", "-c",
  "printf 'ok 1\\n\\033[31mFA'; sleep 0.1; printf 'IL\\033[0m: TestOne\\n'; printf 'FAIL: TestOne\\n'; " +
  "printf 'panic: boom\\n'; printf 'Listening on :8080'; sleep 1" ] } });
const m = await port;
await h.sleep("300ms");
sub.cancel();
const none = await h.wait_output({ pattern: "never", timeout: "50ms" }).catch((e: any) => e.code);
export default () => ({ seen, port: m.groups[0], screen: m.screen, none });
`, map[string]any{"allow": []string{"spawn", "screen"}, "session_id": sid, "block_id": strs(c["block_ids"])[0]})
	if !out.OK || string(out.Result) != `{"seen":["FAIL TestOne","panic boom"],"port":"8080","screen":"main","none":"timeout"}` {
		t.Fatalf("out = %+v %s", out, out.Result)
	}

	// Without the screen permission, no triggers.
	out = h.runScript(`try { h.trigger({ pattern: "x" }, () => {}) } catch (e: any) { console.log(String(e.message)) }`, nil)
	if len(out.logs) != 1 || !strings.Contains(out.logs[0], "screen permission") {
		t.Fatalf("no screen: %+v", out)
	}

	// Search finds what was printed before.
	out = h.runScript(`
const [b] = await (await h.block()).new_split({ focus: false, layout: { command: ["sh", "-c", "seq 1 30; sleep 5"] } });
await h.sleep("300ms");
const r = await h.terminal.search({ block_id: b.id, args: { pattern: "^2(\\d)$" } });
export default () => r.matches.map((m: any) => m.groups[0]).join("");`,
		map[string]any{"allow": []string{"spawn", "screen"}, "session_id": sid, "block_id": strs(c["block_ids"])[0]})
	if out.str(t) != "0123456789" {
		t.Fatalf("search: %+v", out)
	}
}

// A snapshot trigger sees a program that never stops redrawing.
func TestSnapshotTriggerWhileDrawing(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	c := h.call("session.create", map[string]any{"command": []string{"cat"}})
	out := h.runScript(`
const m = h.wait_output({ pattern: "Do you want to proceed", mode: "snapshot", screen: "any", timeout: "5s" });
await (await h.block()).new_split({ focus: false, layout: { command: ["sh", "-c",
  "printf '\\033[?1049hDo you want to proceed?\\n'; i=0; while true; do printf '\\r%s' $i; i=$((i+1)); sleep 0.05; done"] } });
export default async () => (await m).match;`,
		map[string]any{"allow": []string{"spawn", "screen"}, "session_id": c["session_id"], "block_id": strs(c["block_ids"])[0]})
	if out.str(t) != "Do you want to proceed" {
		t.Fatalf("snapshot: %+v", out)
	}
}
