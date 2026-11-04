package server

import (
	"encoding/json"
	"strings"

	"github.com/manueltarouca/hive/internal/api"
)

// Permissions scripts need for each method (see api.Perm). Most are fixed
// where the method is registered; these depend on the arguments.

// spawnNote is shown by api describe for methods that can run a program.
const spawnNote = "spawn when it runs a command (in command, or anywhere in layout)"

type layoutCommands struct {
	Command []string        `json:"command"`
	A       *layoutCommands `json:"a"`
	B       *layoutCommands `json:"b"`
}

func (l *layoutCommands) runs() bool {
	return l != nil && (len(l.Command) > 0 || l.A.runs() || l.B.runs())
}

// permByLayout: control to arrange the default shell, in any folder; spawn
// to run a program. An empty command means the default shell.
func permByLayout(raw json.RawMessage) (string, error) {
	var in struct {
		Command []string        `json:"command"`
		Layout  *layoutCommands `json:"layout"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return "", api.BadRequest("input: %v", err)
		}
	}
	if len(in.Command) > 0 || in.Layout.runs() {
		return "spawn", nil
	}
	return "control", nil
}

// permOfSetting: settings that change who gets in or what scripts may do
// need server.
func permOfSetting(raw json.RawMessage) (string, error) {
	var in struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", api.BadRequest("input: %v", err)
	}
	if strings.HasPrefix(in.Key, "script") || strings.HasPrefix(in.Key, "tailscale") || in.Key == "listen" {
		return "server", nil
	}
	return "config", nil
}

// blockMethodName is the terminal method block.method forwards to.
func blockMethodName(method string) string {
	if !strings.HasPrefix(method, terminalPrefix) {
		return terminalPrefix + method
	}
	return method
}

// permOfForwarded: block.method needs what the method it reaches needs.
func (s *Server) permOfForwarded(raw json.RawMessage) (string, error) {
	var in struct {
		Method string `json:"method"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", api.BadRequest("input: %v", err)
	}
	return s.reg.PermissionOf(blockMethodName(in.Method), nil)
}
