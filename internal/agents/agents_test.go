package agents

import (
	"encoding/json"
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
