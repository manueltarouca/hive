package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manueltarouca/hive/internal/config"
)

// hive config: path, set, check, show, default. HIVE_CONFIG keeps it off the
// real file.
func TestConfigCommands(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hive")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	dir := t.TempDir()
	conf := filepath.Join(dir, "hive", "config")
	run := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		// The test's own socket and state too, in case a command starts a
		// server; HIVE_CONFIG last wins over testEnv's.
		cmd.Env = append(testEnv(dir), "HIVE_CONFIG="+conf)
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		}
		if err != nil {
			t.Fatal(err)
		}
		return string(out), 0
	}
	// Numbers read as the file writes them: no exponents.
	if out, code := run("config", "show"); code != 0 || strings.Contains(out, "e+") || !strings.Contains(out, "scrollback-limit = 16777216 ") {
		t.Fatalf("config show = %q %d", out, code)
	}
	if out, code := run("config", "path"); code != 0 || strings.TrimSpace(out) != conf {
		t.Fatalf("path = %q %d", out, code)
	}
	if _, err := os.Stat(conf); !os.IsNotExist(err) {
		t.Fatal("path created the file")
	}
	// --create makes the commented template, once; "Open Config File" uses it.
	if out, code := run("config", "path", "--create"); code != 0 || strings.TrimSpace(out) != conf {
		t.Fatalf("path --create = %q %d", out, code)
	}
	if b, _ := os.ReadFile(conf); !strings.Contains(string(b), "# theme = system") {
		t.Fatalf("created = %q", b)
	}
	if err := os.WriteFile(conf, []byte("theme = day\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := run("config", "path", "--create"); code != 0 {
		t.Fatal("path --create on an existing file failed")
	}
	if b, _ := os.ReadFile(conf); string(b) != "theme = day\n" {
		t.Fatalf("path --create overwrote the file: %q", b)
	}
	_ = os.Remove(conf)
	if out, code := run("config", "set", "theme", "dusk"); code == 0 || !strings.Contains(out, "theme must be") {
		t.Fatalf("bad set = %q %d", out, code)
	}
	if out, code := run("config", "set", "theme", "night"); code != 0 {
		t.Fatalf("set = %q %d", out, code)
	}
	if out, code := run("config", "check"); code != 0 {
		t.Fatalf("check of a good file = %q %d", out, code)
	}
	if err := os.WriteFile(conf, []byte("theme = night\nfont-size = 99\ncolour = red\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := run("config", "check")
	if code != 1 || !strings.Contains(out, "line 2") || !strings.Contains(out, "line 3") {
		t.Fatalf("check of a bad file = %q %d", out, code)
	}
	out, code = run("config", "show", "--json")
	var shown struct {
		Keys []struct {
			Name, Default, Help string
			Server              bool
		} `json:"keys"`
		Path        string         `json:"path"`
		Values      map[string]any `json:"values"`
		Diagnostics []struct {
			Line  int    `json:"line"`
			Level string `json:"level"`
		} `json:"diagnostics"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &shown) != nil {
		t.Fatalf("show --json = %q %d", out, code)
	}
	if len(shown.Keys) != len(config.Keys) || shown.Keys[0].Name != "theme" || shown.Keys[0].Help == "" || !shown.Keys[7].Server {
		t.Fatalf("keys = %+v", shown.Keys)
	}
	if shown.Path != conf || shown.Values["theme"] != "night" || shown.Values["font-size"] != 13.0 || len(shown.Diagnostics) != 2 {
		t.Fatalf("shown = %+v", shown)
	}
	if out, code := run("config", "default"); code != 0 || !strings.Contains(out, "# scrollback-limit = 16m") {
		t.Fatalf("default = %q %d", out, code)
	}
}
