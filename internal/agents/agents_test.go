package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResumeArgv(t *testing.T) {
	cases := map[string][]string{
		"claude":   {"claude", "--resume", "id1"},
		"codex":    {"codex", "resume", "id1"},
		"opencode": {"opencode", "-s", "id1"},
		"gemini":   {"gemini", "--resume", "id1"},
		"grok":     {"grok", "--resume", "id1"},
	}
	for h, want := range cases {
		if got := ResumeArgv(h, "id1"); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v", h, got)
		}
	}
	if ResumeArgv("unknown", "x") != nil || ResumeArgv("claude", "") != nil {
		t.Error("want nil")
	}
	t.Setenv("HIVE_RESUME_AIDER", `["aider","--restore","{id}"]`)
	if got := ResumeArgv("aider", "s9"); !reflect.DeepEqual(got, []string{"aider", "--restore", "s9"}) {
		t.Errorf("override: %v", got)
	}
}

func TestParseClaudeHooks(t *testing.T) {
	cases := []struct {
		in    string
		event string
		state string
	}{
		{`{"session_id":"s1","transcript_path":"/t.jsonl","cwd":"/w","hook_event_name":"SessionStart","source":"startup"}`, "SessionStart", "idle"},
		{`{"session_id":"s1","cwd":"/w","hook_event_name":"UserPromptSubmit","prompt":"hi"}`, "UserPromptSubmit", "working"},
		{`{"session_id":"s1","cwd":"/w","hook_event_name":"Notification","message":"Claude needs your permission to use Bash"}`, "Notification", "blocked"},
		{`{"session_id":"s1","cwd":"/w","hook_event_name":"Stop"}`, "Stop", "done"},
		{`{"session_id":"s1","cwd":"/w","hook_event_name":"SessionEnd","reason":"exit"}`, "SessionEnd", "ended"},
	}
	for _, c := range cases {
		r, err := ParseHook("claude", []byte(c.in))
		if err != nil {
			t.Fatal(err)
		}
		if r.Event != c.event || r.State != c.state || r.SessionID != "s1" || r.Cwd != "/w" {
			t.Errorf("%s: %+v", c.event, r)
		}
	}
	r, _ := ParseHook("claude", []byte(cases[2].in))
	if !strings.Contains(r.Message, "permission") {
		t.Errorf("message = %q", r.Message)
	}
}

func TestInstallClaudeIsIdempotentAndPreserves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"model":"opus","hooks":{"Stop":[{"hooks":[{"type":"command","command":"say done"}]}]}}`), 0o600)
	for i := 0; i < 2; i++ {
		if _, err := InstallClaude(dir, "/usr/local/bin/hive", false); err != nil {
			t.Fatal(err)
		}
	}
	var s map[string]any
	data, _ := os.ReadFile(path)
	json.Unmarshal(data, &s)
	if s["model"] != "opus" {
		t.Fatal("lost other settings")
	}
	hooks := s["hooks"].(map[string]any)
	for _, ev := range ClaudeEvents {
		groups := hooks[ev].([]any)
		n := 0
		for _, g := range groups {
			for _, h := range g.(map[string]any)["hooks"].([]any) {
				if strings.Contains(h.(map[string]any)["command"].(string), "agent hook claude") {
					n++
				}
			}
		}
		if n != 1 {
			t.Errorf("%s: %d hive hooks", ev, n)
		}
	}
	if !strings.Contains(string(data), "say done") {
		t.Fatal("lost user's Stop hook")
	}
	if _, err := UninstallClaude(dir, false); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "agent hook claude") || !strings.Contains(string(data), "say done") {
		t.Fatalf("uninstall: %s", data)
	}
}

func TestClaudeNotificationTypesAndPostToolUse(t *testing.T) {
	cases := map[string]string{
		`{"hook_event_name":"Notification","notification_type":"permission_prompt","message":"needs permission"}`: "blocked",
		`{"hook_event_name":"Notification","notification_type":"idle_prompt","message":"waiting for input"}`:      "done",
		`{"hook_event_name":"Notification","notification_type":"auth_success","message":"ok"}`:                    "",
		`{"hook_event_name":"PostToolUse","tool_name":"Bash"}`:                                                    "working",
	}
	for in, want := range cases {
		r, err := ParseHook("claude", []byte(in))
		if want == "" {
			if err == nil && r.State != "" {
				t.Errorf("%s: want no state change, got %q", in, r.State)
			}
			continue
		}
		if err != nil || r.State != want {
			t.Errorf("%s: %q %v", in, r.State, err)
		}
	}
}

func TestInstallFollowsSymlinkedSettings(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles-settings.json")
	os.WriteFile(target, []byte(`{"model":"opus"}`), 0o644)
	cfg := filepath.Join(dir, "claude")
	os.MkdirAll(cfg, 0o700)
	link := filepath.Join(cfg, "settings.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallClaude(cfg, "/bin/hive", false); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("settings.json symlink was replaced by a file")
	}
	data, _ := os.ReadFile(target)
	if !strings.Contains(string(data), "agent hook claude") {
		t.Fatalf("target not updated: %s", data)
	}
}

func TestResumeArgvRefusesFlagLikeIDs(t *testing.T) {
	if ResumeArgv("claude", "-x") != nil || ResumeArgv("claude", "--resume") != nil || ResumeArgv("claude", "a b") != nil {
		t.Fatal("flag-like or spaced session IDs must not reach a command line")
	}
}

// Some agents run Claude Code's hooks too (Grok's CLI reads the same
// settings), so a "claude" report can come from another agent. The program
// in the pane decides: resuming grok's session with claude would fail.
func TestHarnessFollowsTheRunningAgent(t *testing.T) {
	cases := []struct{ reported, running, want string }{
		{"claude", "claude", "claude"},
		{"claude", "grok", "grok"},
		{"claude", "codex", "codex"},
		{"claude", "zsh", "claude"}, // a hook can run as the shell takes over
		{"claude", "", "claude"},
		{"codex", "node", "codex"},
	}
	for _, c := range cases {
		if got := Harness(c.reported, c.running); got != c.want {
			t.Errorf("Harness(%q, %q) = %q, want %q", c.reported, c.running, got, c.want)
		}
	}
}

// Grok runs the hooks in ~/.claude/settings.json and says so in the
// variables it sets on every hook process.
func TestHookHarnessUnderGrok(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := HookHarness("claude", env(nil)); got != "claude" {
		t.Errorf("plain claude hook = %q", got)
	}
	if got := HookHarness("claude", env(map[string]string{"GROK_SESSION_ID": "01a1", "GROK_HOOK_EVENT": "session_start"})); got != "grok" {
		t.Errorf("hook under grok = %q", got)
	}
}

// Grok's events, as its hooks guide shows them: Claude's event names, its
// own camelCase session field, and subagent sessions tagged subagentType.
func TestParseGrokHooks(t *testing.T) {
	r, err := ParseHook("grok", []byte(`{"hookEventName":"session_start","hook_event_name":"SessionStart","sessionId":"01a14b1e-1122-7a50-a819-084bfc5e3bbf","cwd":"/Users/x/media"}`))
	if err != nil || r.Harness != "grok" || r.State != "idle" || r.SessionID != "01a14b1e-1122-7a50-a819-084bfc5e3bbf" {
		t.Fatalf("grok SessionStart = %+v, %v", r, err)
	}
	if r, err := ParseHook("grok", []byte(`{"hook_event_name":"Stop","session_id":"s1"}`)); err != nil || r.State != "done" {
		t.Fatalf("grok Stop = %+v, %v", r, err)
	}
	// A subagent's session starting, ending or stopping says nothing about
	// the agent in the pane, and its ID isn't the one to resume.
	for _, ev := range []string{"SessionStart", "SessionEnd", "Stop"} {
		_, err := ParseHook("grok", []byte(`{"hook_event_name":"`+ev+`","sessionId":"child-1","subagentType":"explore"}`))
		if !errors.Is(err, ErrNoChange) {
			t.Errorf("subagent %s: err = %v, want ErrNoChange", ev, err)
		}
	}
}

// Claude Code also reports its state itself (OSC 7501), including after an
// interrupt, when no hook runs. That status maps onto hive's states.
func TestStateFromProgramStatus(t *testing.T) {
	cases := []struct {
		harness, app, state string
		want                string
		ok                  bool
	}{
		{"claude", "claude-code", "idle", "idle", true},
		{"claude", "claude-code", "working", "working", true},
		{"claude", "claude-code", "blocked", "blocked", true},
		{"claude", "claude-code", "done", "done", true},
		// error: failed and stopped, such as Claude Code at the usage limit.
		{"claude", "claude-code", "error", "done", true},
		{"grok", "grok", "working", "working", true},
		{"claude", "claude-code", "clear", "", false}, // a record removed says nothing
		{"claude", "cargo", "working", "", false},     // another program in the pane
		{"codex", "claude-code", "idle", "", false},
	}
	for _, c := range cases {
		got, ok := StateFromStatus(c.harness, c.app, c.state)
		if got != c.want || ok != c.ok {
			t.Errorf("StateFromStatus(%q, %q, %q) = %q, %v; want %q, %v", c.harness, c.app, c.state, got, ok, c.want, c.ok)
		}
	}
}

// A subagent waiting for permission or using tools is the agent's work:
// it shows, without the subagent's session ID.
func TestSubagentWorkStillShows(t *testing.T) {
	r, err := ParseHook("grok", []byte(`{"hook_event_name":"Notification","notification_type":"permission_prompt","sessionId":"child-1","subagentType":"explore","message":"Grok needs permission","transcript_path":"/x/child.jsonl"}`))
	if err != nil || r.State != "blocked" || r.SessionID != "" || r.TranscriptPath != "" || r.Message != "Grok needs permission" {
		t.Fatalf("subagent permission prompt = %+v, %v", r, err)
	}
	r, err = ParseHook("grok", []byte(`{"hook_event_name":"PostToolUse","sessionId":"child-1","subagentType":"explore"}`))
	if err != nil || r.State != "working" || r.SessionID != "" {
		t.Fatalf("subagent tool use = %+v, %v", r, err)
	}
}
