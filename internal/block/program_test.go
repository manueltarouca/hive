package block

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The program a terminal runs, named the way a person would: an installed
// Claude Code's executable is its version number, and npm tools run as node.
func TestProgramOf(t *testing.T) {
	cases := []struct {
		name, exe string
		argv      []string
		want      string
	}{
		{"zsh", "/bin/zsh", []string{"-zsh"}, "zsh"},
		{"nvim", "/opt/homebrew/bin/nvim", []string{"nvim", "main.go"}, "nvim"},
		{"2.1.302", "/Users/x/.local/share/claude/versions/2.1.302", []string{"claude", "--resume", "abc"}, "claude"},
		{"2.1.302", "/Users/x/.local/share/claude/versions/2.1.302", nil, "claude"},
		{"node", "/opt/homebrew/bin/node", []string{"node", "/opt/homebrew/lib/node_modules/@anthropic-ai/claude-code/cli.js"}, "claude"},
		{"node", "/usr/local/bin/node", []string{"node", "/usr/local/lib/node_modules/@google/gemini-cli/dist/index.js"}, "gemini"},
		{"node", "/usr/local/bin/node", []string{"node", "/usr/local/bin/codex"}, "codex"},
		{"node", "/usr/local/bin/node", []string{"node", "--inspect", "server.js"}, "node"},
		{"node", "/usr/local/bin/node", []string{"node"}, "node"},
		{"python3.12", "/usr/bin/python3.12", []string{"python3", "-m", "http.server"}, "python"},
		{"Python", "/Library/Frameworks/Python.framework/Versions/3.12/Resources/Python.app/Contents/MacOS/Python", []string{"python3", "app.py"}, "python"},
		{"bun", "/Users/x/.bun/bin/bun", []string{"bun", "run", "dev"}, "bun"},
		{"htop", "", nil, "htop"},
		{"", "", nil, ""},
	}
	for _, c := range cases {
		if got := programOf(c.name, c.exe, c.argv); got != c.want {
			t.Errorf("programOf(%q, %q, %q) = %q, want %q", c.name, c.exe, c.argv, got, c.want)
		}
	}
}

// The server reads the foreground's executable and arguments to name it: a
// script run by an interpreter from Claude Code's package is Claude Code.
func TestForegroundNamesTheProgram(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	dir := filepath.Join(t.TempDir(), "node_modules", "@anthropic-ai", "claude-code")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "cli.py")
	if err := os.WriteFile(script, []byte("import time\ntime.sleep(30)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Start("block:program", Options{Command: []string{"python3", script}, Cols: 80, Rows: 24}, func(Event) {})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var p Process
	waitFor(t, "claude in foreground", func() bool {
		p, _ = b.Foreground()
		return p.Program == "claude"
	})
}
