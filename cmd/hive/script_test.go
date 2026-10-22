package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestScriptRun drives hive script run on the real binary: a TypeScript
// file with an import, its log on stderr, its result on stdout, arguments,
// permissions and exit codes.
func TestScriptRun(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hive")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	dir, err := os.MkdirTemp("/tmp", "hive-script.")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	env := testEnv(dir)
	hive := func(stdin string, args ...string) (string, string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(stdin)
		var out, errb strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("hive %v: %v", args, err)
		}
		return out.String(), errb.String(), code
	}
	defer hive("", "server", "stop")
	if _, e, code := hive("", "new", "work", "--cwd", dir, "--", "cat"); code != 0 {
		t.Fatalf("new: %s", e)
	}

	os.WriteFile(filepath.Join(dir, "lib.ts"), []byte(`export const greet = (n: string): string => "hello " + n;`), 0o644)
	os.WriteFile(filepath.Join(dir, "main.ts"), []byte(`import { greet } from "./lib";
const { sessions } = await h.session.list();
console.log(greet(String(h.args.who)));
h.log("warn", "careful");
export default () => ({ sessions: sessions.map((s: any) => s.label) });
`), 0o644)
	out, errOut, code := hive("", "script", "run", "main.ts", "who=mt")
	if code != 0 || strings.TrimSpace(out) != `{"sessions":["work"]}` || errOut != "hello mt\nwarn: careful\n" {
		t.Fatalf("run: exit %d\nstdout %q\nstderr %q", code, out, errOut)
	}

	// -e and stdin; a failing script exits 1 with its error.
	if out, _, code := hive("", "script", "run", "-e", "export default () => 1 + 1"); code != 0 || strings.TrimSpace(out) != "2" {
		t.Fatalf("-e: %d %q", code, out)
	}
	if _, errOut, code := hive("throw new Error('nope')", "script", "run", "-"); code != 1 || !strings.Contains(errOut, "nope") {
		t.Fatalf("stdin throw: %d %q", code, errOut)
	}
	// Read only unless --allow says more; -s gives the session.
	_, errOut, code = hive("", "-s", "work", "script", "run", "-e", `await h.session.set_label({ label: "x" })`)
	if code != 1 || !strings.Contains(errOut, "needs the control permission") {
		t.Fatalf("denied: %d %q", code, errOut)
	}
	if _, errOut, code := hive("", "-s", "work", "script", "run", "--allow", "control", "-e", `await h.session.set_label({ label: "renamed" })`); code != 0 {
		t.Fatalf("allowed: %d %q", code, errOut)
	}
	if out, _, _ := hive("", "ls"); !strings.Contains(out, "renamed") {
		t.Fatalf("ls: %s", out)
	}
	// A broken script doesn't reach the server.
	if _, errOut, code := hive("", "script", "run", "-e", "const = 1"); code == 0 || !strings.Contains(errOut, "script.ts:1:") {
		t.Fatalf("syntax: %d %q", code, errOut)
	}
	if out, _, code := hive("", "script", "ls"); code != 0 || !strings.Contains(out, "failed") {
		t.Fatalf("script ls: %d %s", code, out)
	}

	// A script in the folder waits for a grant, then runs.
	os.MkdirAll(filepath.Join(dir, "scripts"), 0o755)
	os.WriteFile(filepath.Join(dir, "scripts", "hello.ts"), []byte(`export default hive.script({
  name: "hello", permissions: ["screen", "input"],
  setup(h) { console.log("hello from the folder"); },
});`), 0o644)
	if out, _, _ := hive("", "script", "reload"); !strings.Contains(out, "pending") || !strings.Contains(out, "asks input,screen") {
		t.Fatalf("reload: %s", out)
	}
	if out, _, code := hive("", "script", "grant", "hello"); code != 0 || !strings.Contains(out, "may now: read, input, screen") ||
		!strings.Contains(out, "read your terminals and type into them") {
		t.Fatalf("grant: %d %s", code, out)
	}
	if out, _, _ := hive("", "script", "ls"); !strings.Contains(out, "hello   running") {
		t.Fatalf("ls: %s", out)
	}
	if out, _, _ := hive("", "script", "logs", "hello"); !strings.Contains(out, "hello from the folder") {
		t.Fatalf("logs: %s", out)
	}
	if out, _, _ := hive("", "script", "audit"); !strings.Contains(out, "session.set_label") {
		t.Fatalf("audit: %s", out)
	}

	// Actions from the CLI, and the keymap.
	os.WriteFile(filepath.Join(dir, "scripts", "greet.ts"), []byte(`export default hive.script({
  name: "greet", permissions: ["ui"],
  setup(h) { h.actions.register({ name: "greet", title: "Say hello", run: (ctx, a: any) => "hi " + a.who }); },
});`), 0o644)
	hive("", "script", "reload")
	hive("", "script", "grant", "greet")
	if out, _, _ := hive("", "actions"); !strings.Contains(out, "greet   Say hello") {
		t.Fatalf("actions: %s", out)
	}
	if out, e, code := hive("", "do", "greet", "who=mt"); code != 0 || strings.TrimSpace(out) != `"hi mt"` {
		t.Fatalf("do: %d %q %q", code, out, e)
	}
	if out, _, _ := hive("", "keymap"); !strings.Contains(out, "cmd+d") || !strings.Contains(out, "client.split_right") {
		t.Fatalf("keymap: %s", out)
	}
}
