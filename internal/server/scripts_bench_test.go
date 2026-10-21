package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/script"
)

// BenchmarkScriptCall: one call from a script into the registry and back
// (session.list over 4 sessions), measured inside the script.
func BenchmarkScriptCall(b *testing.B) {
	h := start(&testing.T{}, b.TempDir(), nil)
	defer h.stop()
	for range 4 {
		h.call("session.create", map[string]any{"command": []string{"cat"}})
	}
	n := 10000
	js, err := script.Bundle("", fmt.Sprintf(`
const t0 = Date.now();
for (let i = 0; i < %d; i++) await h.session.list();
export default () => Date.now() - t0;`, n), b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		r := h.call("script.run", map[string]any{"code": js})
		res := h.call("script.result", map[string]any{"run_id": r["run_id"], "timeout": 60})
		ms := res["result"].(float64)
		b.ReportMetric(ms*1e6/float64(n), "ns/call")
	}
}

// BenchmarkBundle20Files: esbuild bundling a script of 20 files.
func BenchmarkBundle20Files(b *testing.B) {
	dir := b.TempDir()
	main := ""
	for i := range 20 {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("m%d.ts", i)), []byte(fmt.Sprintf(`
export interface P%d { id: string; n?: number }
export async function f%d(p: P%d): Promise<number> { return (p.n ?? 0) + %d }
`, i, i, i, i)), 0o644)
		main += fmt.Sprintf("import { f%d } from \"./m%d\";\nawait f%d({ id: \"x\" });\n", i, i, i)
	}
	os.WriteFile(filepath.Join(dir, "main.ts"), []byte(main), 0o644)
	t0 := time.Now()
	if _, err := script.Bundle(filepath.Join(dir, "main.ts"), "", ""); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(time.Since(t0).Microseconds())/1000, "cold-ms")
	b.ResetTimer()
	for range b.N {
		if _, err := script.Bundle(filepath.Join(dir, "main.ts"), "", ""); err != nil {
			b.Fatal(err)
		}
	}
}
