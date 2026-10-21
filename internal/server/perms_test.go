package server

import (
	"encoding/json"
	"slices"
	"testing"
)

// Every method says what a script needs to call it, so a new method can't
// slip past scripts' permissions.
func TestEveryMethodHasAPermission(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	known := []string{"read", "screen", "control", "spawn", "input", "ui", "config", "server"}
	for _, m := range h.srv.reg.List() {
		if !slices.Contains(known, m.Permission) {
			t.Errorf("%s: permission %q", m.Name, m.Permission)
		}
	}
}

// Running a program needs spawn; a folder with the default shell doesn't.
// Forwarded calls take the permission of the method they reach.
func TestPermissionByArguments(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	for _, c := range []struct{ method, args, want string }{
		{"session.create", `{}`, "control"},
		{"session.create", `{"cwd":"/tmp"}`, "control"},
		{"session.create", `{"command":[]}`, "control"},
		{"session.create", `{"command":["sh","-c","x"]}`, "spawn"},
		{"session.create", `{"layout":{"type":"split","a":{},"b":{"command":["make"]}}}`, "spawn"},
		{"session.new_window", `{"session_id":"s","layout":{"cwd":"~"}}`, "control"},
		{"session.new_window", `{"session_id":"s","layout":{"type":"split","a":{"type":"split","a":{},"b":{"command":["x"]}},"b":{}}}`, "spawn"},
		{"session.new_split", `{"session_id":"s","block_id":"b"}`, "control"},
		{"session.new_split", `{"session_id":"s","block_id":"b","layout":{"command":["tail","-f","x"]}}`, "spawn"},
		{"block.method", `{"block_id":"b","method":"write"}`, "input"},
		{"block.method", `{"block_id":"b","method":"hive.terminal.send_key"}`, "input"},
		{"block.method", `{"block_id":"b","method":"format"}`, "screen"},
		{"block.method", `{"block_id":"b","method":"title"}`, "read"},
		{"config.set", `{"key":"theme","value":"day"}`, "config"},
		{"config.set", `{"key":"script-remote-run","value":"true"}`, "server"},
		{"config.set", `{"key":"listen","value":"tailnet://:1"}`, "server"},
		{"hive.terminal.list_dir", `{"block_id":"b"}`, "screen"},
		{"hive.terminal.wait", `{"block_id":"b"}`, "read"},
		{"server.shutdown", `{}`, "server"},
	} {
		got, err := h.srv.reg.PermissionOf(c.method, json.RawMessage(c.args))
		if err != nil || got != c.want {
			t.Errorf("%s %s = %q, %v; want %q", c.method, c.args, got, err, c.want)
		}
	}
	if _, err := h.srv.reg.PermissionOf("block.method", json.RawMessage(`{"block_id":"b","method":"nope"}`)); err == nil {
		t.Error("block.method to an unknown method has a permission")
	}
}
