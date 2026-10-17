// Package agents knows the coding agents hive runs: how to resume each one,
// how to read the reports their hooks send, and how to install those hooks.
package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ResumeArgv returns the command that resumes an agent's session, or nil if
// hive doesn't know how. HIVE_RESUME_<HARNESS>, a JSON list such as
// ["claude","--resume","{id}"], overrides the built-in command.
func ResumeArgv(harness, sessionID string) []string {
	if !ValidSessionID(sessionID) {
		return nil // never let a reported ID become a flag or split into words
	}
	if tmpl := os.Getenv("HIVE_RESUME_" + strings.ToUpper(harness)); tmpl != "" {
		var argv []string
		if err := json.Unmarshal([]byte(tmpl), &argv); err == nil && len(argv) > 0 {
			for i := range argv {
				argv[i] = strings.ReplaceAll(argv[i], "{id}", sessionID)
			}
			return argv
		}
	}
	switch harness {
	case "claude":
		return []string{"claude", "--resume", sessionID}
	case "codex":
		return []string{"codex", "resume", sessionID}
	case "opencode":
		return []string{"opencode", "-s", sessionID}
	case "gemini":
		return []string{"gemini", "--resume", sessionID}
	case "grok":
		return []string{"grok", "--resume", sessionID}
	}
	return nil
}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ValidSessionID reports whether id can be passed to an agent's command
// line: letters, digits, dot, dash and underscore, not starting with a dash.
func ValidSessionID(id string) bool { return sessionIDPattern.MatchString(id) }

// Report is what one hook call says.
type Report struct {
	Harness        string `json:"harness"`
	Event          string `json:"event"`
	State          string `json:"state"` // idle, working, blocked, done, ended
	SessionID      string `json:"session_id,omitempty"`
	Cwd            string `json:"cwd,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	Message        string `json:"message,omitempty"`
	Source         string `json:"source,omitempty"`
}

// ClaudeEvents are the Claude Code hooks hive installs.
// PostToolUse is how a blocked agent is seen to continue: it fires once the
// tool the user approved has run.
var ClaudeEvents = []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "Notification", "PostToolUse", "Stop"}

var claudeStates = map[string]string{
	"SessionStart": "idle", "UserPromptSubmit": "working", "Notification": "blocked",
	"PostToolUse": "working", "Stop": "done", "SessionEnd": "ended",
}

// Names lists the agents hive knows. Their programs are named the same (see
// programOf in internal/block).
var Names = map[string]bool{"claude": true, "codex": true, "gemini": true, "grok": true, "opencode": true}

// HookHarness is the agent a hook really runs for. Grok runs the hooks in
// ~/.claude/settings.json for Claude Code compatibility, so hive's "claude"
// hook also fires inside grok; grok sets GROK_SESSION_ID on every hook.
func HookHarness(installed string, getenv func(string) string) string {
	if getenv("GROK_SESSION_ID") != "" || getenv("GROK_HOOK_EVENT") != "" {
		return "grok"
	}
	return installed
}

// Harness reconciles a report with the program running in the pane: when
// that is another agent, the report came through borrowed hooks and that
// agent is the one to name and resume. Anything else (a shell taking over,
// an unknown name) keeps the report's.
func Harness(reported, running string) string {
	if Names[running] && running != reported {
		return running
	}
	return reported
}

// statusApps names the app each agent reports in its program status
// (OSC 7501). Agents not listed report under their own name.
var statusApps = map[string]string{"claude": "claude-code"}

var statusStates = map[string]string{
	"idle": "idle", "working": "working", "waiting": "blocked", "done": "done", "failed": "done",
}

// StateFromStatus maps an agent's own status report (OSC 7501) to hive's
// agent states. Claude Code sends one at every change, also after an
// interrupt, when no hook runs. ok is false for another program's report
// or one that says nothing (a cleared record).
func StateFromStatus(harness, app, state string) (string, bool) {
	want := statusApps[harness]
	if want == "" {
		want = harness
	}
	if app != want {
		return "", false
	}
	s, ok := statusStates[state]
	return s, ok
}

// ErrNoChange means the hook carries nothing to report.
var ErrNoChange = errors.New("no state change")

// ParseHook reads a hook's JSON input.
func ParseHook(harness string, data []byte) (Report, error) {
	var in struct {
		SessionID      string `json:"session_id"`
		SessionIDCamel string `json:"sessionId"`    // grok
		Subagent       string `json:"subagentType"` // grok: a child session's events
		TranscriptPath string `json:"transcript_path"`
		Cwd            string `json:"cwd"`
		Event          string `json:"hook_event_name"`
		Source         string `json:"source"`
		Message        string `json:"message"`
		NotifType      string `json:"notification_type"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return Report{}, fmt.Errorf("hook input: %w", err)
	}
	if in.SessionID == "" {
		in.SessionID = in.SessionIDCamel
	}
	r := Report{Harness: harness, Event: in.Event, SessionID: in.SessionID, Cwd: in.Cwd,
		TranscriptPath: in.TranscriptPath, Message: in.Message, Source: in.Source}
	switch harness {
	case "claude", "codex", "grok":
		r.State = claudeStates[in.Event]
	default:
		return r, fmt.Errorf("unknown harness %q", harness)
	}
	if r.State == "" {
		return r, fmt.Errorf("unknown hook event %q", in.Event)
	}
	if in.Subagent != "" {
		// A subagent's session starting, ending or stopping isn't the pane's
		// agent; its waiting for permission or using tools is the agent's
		// work. Its session isn't the one to resume.
		switch in.Event {
		case "SessionStart", "SessionEnd", "Stop":
			return r, ErrNoChange
		}
		r.SessionID, r.Cwd, r.TranscriptPath = "", "", ""
	}
	if in.Event == "Notification" {
		switch in.NotifType {
		case "idle_prompt": // waiting for the next prompt, not stuck
			r.State = "done"
		case "", "permission_prompt", "elicitation_dialog":
		default: // auth_success and others say nothing about progress
			r.State = ""
			return r, ErrNoChange
		}
	}
	return r, nil
}

const marker = "agent hook claude"

// InstallClaude adds hive's hooks to Claude Code's settings.json in
// configDir, leaving everything else in the file alone. It is safe to run
// again. It returns a description of what changed.
func InstallClaude(configDir, hivePath string, dryRun bool) ([]string, error) {
	path := filepath.Join(configDir, "settings.json")
	settings, err := readJSON(path)
	if err != nil {
		return nil, err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	command := fmt.Sprintf("%q %s", hivePath, marker)
	var changes []string
	for _, ev := range ClaudeEvents {
		groups, _ := hooks[ev].([]any)
		groups, removed := withoutHive(groups)
		groups = append(groups, map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 5}},
		})
		hooks[ev] = groups
		if removed == 0 {
			changes = append(changes, "added "+ev+" hook")
		}
	}
	settings["hooks"] = hooks
	if dryRun {
		return changes, nil
	}
	return changes, writeJSON(path, settings)
}

// UninstallClaude removes hive's hooks from Claude Code's settings.json.
func UninstallClaude(configDir string, dryRun bool) ([]string, error) {
	path := filepath.Join(configDir, "settings.json")
	settings, err := readJSON(path)
	if err != nil {
		return nil, err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	var changes []string
	for ev, v := range hooks {
		groups, _ := v.([]any)
		groups, removed := withoutHive(groups)
		if removed > 0 {
			changes = append(changes, "removed "+ev+" hook")
		}
		if len(groups) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = groups
		}
	}
	if len(hooks) == 0 {
		delete(settings, "hooks")
	}
	if dryRun || len(changes) == 0 {
		return changes, nil
	}
	return changes, writeJSON(path, settings)
}

// withoutHive drops hive's hook commands, and any group left empty.
func withoutHive(groups []any) ([]any, int) {
	var out []any
	removed := 0
	for _, g := range groups {
		gm, ok := g.(map[string]any)
		if !ok {
			out = append(out, g)
			continue
		}
		hs, _ := gm["hooks"].([]any)
		var keep []any
		for _, h := range hs {
			hm, _ := h.(map[string]any)
			if cmd, _ := hm["command"].(string); strings.Contains(cmd, marker) {
				removed++
				continue
			}
			keep = append(keep, h)
		}
		if len(keep) == 0 && len(hs) > 0 {
			continue
		}
		gm["hooks"] = keep
		out = append(out, gm)
	}
	return out, removed
}

func readJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	// Settings are often symlinks into a dotfiles repo: write the target, so
	// the link survives the rename below.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	mode := fs.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp := path + ".hive.tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
