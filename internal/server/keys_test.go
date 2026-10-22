package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type kmBinding struct {
	Key, Mode, Action, Source, Title string
	Args                             json.RawMessage
}

func (h *harness) keymap(kind string) (map[string]kmBinding, []map[string]any, []any) {
	h.t.Helper()
	out := h.call("keymap.get", map[string]any{"client_kind": kind})
	raw, _ := json.Marshal(out["bindings"])
	var list []kmBinding
	_ = json.Unmarshal(raw, &list)
	m := map[string]kmBinding{}
	for _, b := range list {
		m[b.Mode+"/"+b.Key] = b
	}
	var modes []map[string]any
	for _, x := range out["modes"].([]any) {
		modes = append(modes, x.(map[string]any))
	}
	return m, modes, out["warnings"].([]any)
}

// The Mac's defaults, a script's keys over them, and the settings file
// over both; a script's keys go when it stops.
func TestKeymapLayers(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	os.WriteFile(cfg, []byte("keybind = cmd+t=client.palette\nkeybind = shift+cmd+w=unbind\n"), 0o600)
	h := startScripts(t, map[string]string{"keys.ts": `export default hive.script({ name: "keys", permissions: ["ui"],
  setup(h) {
    h.bind("cmd+t", "nope");                 // the settings file wins this one
    h.bind("ctrl+b>c", "client.new_tab");
    h.mode("resize", { exclusive: true });
    h.bind("resize/left", "client.resize", { direction: "left" });
    h.bind("cmd+e", () => {});
  } });`}, func(c *Config) { c.ConfigPath = cfg })
	defer h.stop()
	m, _, _ := h.keymap("mac")
	if m["/cmd+d"].Action != "client.split_right" || m["/cmd+d"].Title != "Split Right" {
		t.Fatalf("defaults: %+v", m["/cmd+d"])
	}
	if b := m["/cmd+t"]; b.Action != "client.palette" || b.Source != "settings" {
		t.Fatalf("settings: %+v", b)
	}
	if _, ok := m["/shift+cmd+w"]; ok {
		t.Fatal("unbind didn't")
	}
	h.call("script.grant", map[string]any{"name": "keys"})
	// Grant answers once the script read its manifest; setup binds just after.
	h.until("keys bound", func() bool { m, _, _ := h.keymap("mac"); _, ok := m["/cmd+e"]; return ok })
	m, modes, warns := h.keymap("mac")
	if b := m["/ctrl+b>c"]; b.Action != "client.new_tab" || b.Source != "script:keys" {
		t.Fatalf("script: %+v", b)
	}
	if b := m["resize/left"]; string(b.Args) != `{"direction":"left"}` {
		t.Fatalf("mode: %+v", b)
	}
	if len(modes) != 1 || modes[0]["name"] != "resize" || modes[0]["exclusive"] != true {
		t.Fatalf("modes: %v", modes)
	}
	if b := m["/cmd+e"]; b.Action != "keys-key-1" {
		t.Fatalf("a function's key: %+v", b)
	}
	if b := m["/cmd+t"]; b.Source != "settings" || len(warns) == 0 {
		t.Fatalf("cmd+t: %+v, warnings %v", b, warns)
	}
	h.call("script.disable", map[string]any{"name": "keys"})
	h.until("keys gone", func() bool { m, _, _ := h.keymap("mac"); _, ok := m["/ctrl+b>c"]; return !ok })
	// Other apps get no Mac defaults.
	if m, _, _ := h.keymap("android"); len(m) != 1 {
		t.Fatalf("android: %v", m)
	}
	if e := h.callErr("client.perform", map[string]any{"client_id": "client:none", "action": "client.zoom"}); e.Code != "not_found" {
		t.Fatalf("perform: %v", e)
	}
	_ = time.Second
}
