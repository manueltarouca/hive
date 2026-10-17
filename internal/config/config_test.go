package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The grammar is Ghostty's: whole-line # comments, key = value split at the
// first =, whitespace and CR trimmed, surrounding double quotes removed.
func TestParseGrammar(t *testing.T) {
	src := "\xef\xbb\xbf# hive\r\n\n  # indented comment\ntheme = night\r\nfont-family =  \"Berkeley Mono \"\nfont-size=13.5\nbell = flash # not a comment\n"
	c, diags := Parse(src)
	if c.Theme != "night" || c.FontFamily != "Berkeley Mono " || c.FontSize != 13.5 {
		t.Fatalf("config = %+v", c)
	}
	// A # after a value is part of it, so this one is invalid.
	if len(diags) != 1 || diags[0].Line != 7 || diags[0].Key != "bell" || diags[0].Level != Error {
		t.Fatalf("diags = %+v", diags)
	}
}

func TestDefaults(t *testing.T) {
	c, diags := Parse("")
	want := Config{Theme: "system", FontFamily: "", FontSize: 13, Bell: "flash", Notify: "background",
		ConfirmPaste: true, ScrollbackLimit: 16 << 20, ResumeAgents: true}
	if !reflect.DeepEqual(c, want) || len(diags) != 0 {
		t.Fatalf("defaults = %+v, %v", c, diags)
	}
}

// Every key checks its value; a bad one keeps the default and says why, on
// its line. Unknown keys and repeats warn; an empty value resets.
func TestValuesAndDiagnostics(t *testing.T) {
	c, diags := Parse(strings.Join([]string{
		"theme = dusk",           // 1 error
		"font-size = 40",         // 2 error: 8–32
		"scrollback-limit = 64m", // 3 ok
		"scrollback-limit = 1k",  // 4 error: floor 64k (last valid stays)
		"confirm-paste = no",     // 5 ok
		"notify = never",         // 6 ok
		"resume-agents = false",  // 7 ok
		"colour = red",           // 8 warning: unknown
		"notify = always",        // 9 warning: repeat, last wins
		"bell =",                 // 10 reset to default
		"just-a-word",            // 11 error: no =
	}, "\n"))
	if c.Theme != "system" || c.FontSize != 13 || c.ScrollbackLimit != 64<<20 || c.ConfirmPaste ||
		c.Notify != "always" || c.ResumeAgents || c.Bell != "flash" {
		t.Fatalf("config = %+v", c)
	}
	got := map[int]Level{}
	for _, d := range diags {
		got[d.Line] = d.Level
		if d.Message == "" {
			t.Errorf("line %d: no message", d.Line)
		}
	}
	want := map[int]Level{1: Error, 2: Error, 4: Error, 8: Warning, 9: Warning, 11: Error}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diag lines = %v, want %v (%+v)", got, want, diags)
	}
}

func TestSizes(t *testing.T) {
	for in, want := range map[string]uint64{"65536": 65536, "64k": 64 << 10, "16M": 16 << 20, "1g": 1 << 30, "2.5m": 5 << 19} {
		if got, err := parseSize(in); err != nil || got != want {
			t.Errorf("parseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}

// Set edits one key in place, keeping every other byte: the last active line
// for the key, else its commented default, else a new line at the end.
func TestSetEditsInPlace(t *testing.T) {
	cases := []struct{ in, key, value, want string }{
		{"# mine\ntheme = day\nfont-size = 14\n", "theme", "night", "# mine\ntheme = night\nfont-size = 14\n"},
		{"theme = day\ntheme = system\n", "theme", "night", "theme = day\ntheme = night\n"},
		{"# theme = system\n# font-size = 13\n", "theme", "night", "theme = night\n# font-size = 13\n"},
		{"font-size = 14", "theme", "night", "font-size = 14\ntheme = night\n"},
		{"", "theme", "night", "theme = night\n"},
		{"theme = day\r\nbell = none\r\n", "theme", "night", "theme = night\r\nbell = none\r\n"},
	}
	for _, c := range cases {
		if got := setIn(c.in, c.key, c.value); got != c.want {
			t.Errorf("setIn(%q, %s=%s) =\n%q\nwant\n%q", c.in, c.key, c.value, got, c.want)
		}
	}
}

// Set refuses unknown keys and bad values: what's written always parses.
func TestSetValidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := Set(path, "theme", "dusk"); err == nil {
		t.Fatal("bad value written")
	}
	if err := Set(path, "colour", "red"); err == nil {
		t.Fatal("unknown key written")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a refused write created the file")
	}
}

// Writes go to a symlink's target, so stow-managed dotfiles keep their link,
// and keep the file's mode.
func TestSetThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	dot := filepath.Join(dir, "dotfiles", "hive")
	if err := os.MkdirAll(dot, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dot, "config")
	if err := os.WriteFile(target, []byte("# from my dotfiles\ntheme = day\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Set(link, "theme", "night"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced: %v %v", fi, err)
	}
	b, _ := os.ReadFile(target)
	if string(b) != "# from my dotfiles\ntheme = night\n" {
		t.Fatalf("target = %q", b)
	}
	if fi, _ := os.Stat(target); fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
}

// A new file starts from the template: every key commented with its
// default, so the chosen line is the only active one.
func TestSetCreatesFromTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hive", "config")
	if err := Set(path, "theme", "night"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	c, diags := Parse(string(b))
	if c.Theme != "night" || len(diags) != 0 {
		t.Fatalf("created file: %+v %v\n%s", c, diags, b)
	}
	for _, k := range Keys {
		if !strings.Contains(string(b), k.Name) {
			t.Errorf("template lacks %s", k.Name)
		}
	}
	if strings.Count(string(b), "\ntheme = night\n") != 1 {
		t.Fatalf("theme line:\n%s", b)
	}
}

// HIVE_CONFIG names the file (tests, screenshots); else XDG, else ~/.config.
func TestPath(t *testing.T) {
	t.Setenv("HIVE_CONFIG", "/x/hive.conf")
	if p := Path(); p != "/x/hive.conf" {
		t.Fatalf("Path() = %q", p)
	}
	t.Setenv("HIVE_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if p := Path(); p != "/xdg/hive/config" {
		t.Fatalf("Path() = %q", p)
	}
}

// Only the file's own folder is created, never a missing chain of them.
func TestSetCreatesOneFolderOnly(t *testing.T) {
	deep := filepath.Join(t.TempDir(), "a", "b", "config")
	if err := Set(deep, "theme", "night"); err == nil {
		t.Fatal("created a missing chain of folders")
	}
}
