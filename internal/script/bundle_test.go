package script

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// run evaluates a bundle and returns its resolved exports.
func runBundle(t *testing.T, js string) *goja.Object {
	t.Helper()
	rt := goja.New()
	fn, err := rt.RunString(js)
	if err != nil {
		t.Fatalf("eval: %v\n%s", err, js)
	}
	call, _ := goja.AssertFunction(fn)
	p, err := call(goja.Undefined())
	if err != nil {
		t.Fatal(err)
	}
	pr := p.Export().(*goja.Promise)
	if pr.State() != goja.PromiseStateFulfilled {
		t.Fatalf("state %v: %v", pr.State(), pr.Result())
	}
	return pr.Result().ToObject(rt)
}

func TestBundleTypeScriptImportsAndTopLevelAwait(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "lib.ts"), []byte(`export const twice = (n: number): number => n * 2;`), 0o644)
	os.WriteFile(filepath.Join(dir, "main.ts"), []byte(`
import { twice } from "./lib";
interface P { n: number }
const p: P = { n: await Promise.resolve(21) };
async function* gen() { yield p.n }
let got = 0;
for await (const v of gen()) got = v;
export const answer = twice(got);
export default function main() { return "hi" }
`), 0o644)
	js, err := Bundle(filepath.Join(dir, "main.ts"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	ex := runBundle(t, js)
	if ex.Get("answer").ToInteger() != 42 {
		t.Fatalf("answer = %v", ex.Get("answer"))
	}
	if _, ok := goja.AssertFunction(ex.Get("default")); !ok {
		t.Fatal("no default export")
	}
}

func TestBundleCodeWithoutExports(t *testing.T) {
	js, err := Bundle("", `const x: number = await Promise.resolve(1); globalThis.done = x + 1;`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ex := runBundle(t, js)
	if len(ex.Keys()) != 0 {
		t.Fatalf("exports = %v", ex.Keys())
	}
}

func TestBundleRefusesPackagesAndReportsErrors(t *testing.T) {
	_, err := Bundle("", `import x from "left-pad"; x()`, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "not packages") {
		t.Fatalf("err = %v", err)
	}
	_, err = Bundle("", "const = 1", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "script.ts:1:") {
		t.Fatalf("err = %v", err)
	}
}
