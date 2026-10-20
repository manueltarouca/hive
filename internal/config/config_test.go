package config

import (
	"encoding/json"
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

// Fable's review: non-finite numbers passed range checks and broke JSON.
func TestNonFiniteValuesAreRejected(t *testing.T) {
	c, diags := Parse("font-size = nan\nscrollback-limit = inf\n")
	if c.FontSize != 13 || c.ScrollbackLimit != 16<<20 || len(diags) != 2 {
		t.Fatalf("config = %+v, diags = %v", c, diags)
	}
	for _, v := range []string{"1e400", "1e30g", "inf", "nan"} {
		if _, err := parseSize(v); err == nil {
			t.Errorf("parseSize(%q) accepted", v)
		}
	}
	if _, err := json.Marshal(c); err != nil {
		t.Fatal(err)
	}
}

// A newline in a value would write a second line: refused.
func TestSetRefusesLineBreaks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := Set(path, "font-family", "Menlo\nscrollback-limit = 1"); err == nil {
		t.Fatal("a value with a newline was written")
	}
}

// Only a commented setting is the commented default, not a person's prose.
func TestSetKeepsProseComments(t *testing.T) {
	in := "# font-size = 13 was too small for me\n"
	if got := setIn(in, "font-size", "15"); got != in+"font-size = 15\n" {
		t.Fatalf("setIn = %q", got)
	}
}

// A relative symlink resolves from the link's real folder, as the kernel
// does, and a chain of links is followed to the end.
func TestSetFollowsRelativeAndChainedLinks(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// ~/.config is itself a link into dotfiles; the file is a relative link.
	must(os.MkdirAll(filepath.Join(root, "dotfiles", "config", "hive"), 0o755))
	must(os.MkdirAll(filepath.Join(root, "dotfiles", "x"), 0o755))
	must(os.Symlink(filepath.Join(root, "dotfiles", "config"), filepath.Join(root, ".config")))
	must(os.Symlink("../../x/config", filepath.Join(root, ".config", "hive", "config")))
	must(Set(filepath.Join(root, ".config", "hive", "config"), "theme", "night"))
	if b, err := os.ReadFile(filepath.Join(root, "dotfiles", "x", "config")); err != nil || !strings.Contains(string(b), "theme = night") {
		t.Fatalf("relative link target: %q %v", b, err)
	}
	// link -> link -> missing file.
	must(os.Symlink(filepath.Join(root, "end"), filepath.Join(root, "mid")))
	must(os.Symlink(filepath.Join(root, "mid"), filepath.Join(root, "start")))
	must(Create(filepath.Join(root, "start")))
	if _, err := os.Stat(filepath.Join(root, "end")); err != nil {
		t.Fatalf("chained link: %v", err)
	}
}

// Concurrent writers don't lose each other's edits.
func TestConcurrentSetsKeepBothEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	done := make(chan error, 2)
	go func() { done <- Set(path, "theme", "night") }()
	go func() { done <- Set(path, "bell", "none") }()
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	c, _ := Load(path)
	if c.Theme != "night" || c.Bell != "none" {
		t.Fatalf("an edit was lost: %+v", c)
	}
}

// A repeat that fails doesn't claim to win.
func TestFailedRepeatSaysTheEarlierLineStays(t *testing.T) {
	c, diags := Parse("theme = night\ntheme = bogus\n")
	if c.Theme != "night" {
		t.Fatalf("theme = %s", c.Theme)
	}
	for _, d := range diags {
		if strings.Contains(d.Message, "wins") {
			t.Fatalf("a failed repeat claims to win: %v", d)
		}
	}
}

// As in Ghostty, a boolean setting alone on its line means true.
func TestBareBooleanIsTrue(t *testing.T) {
	c, diags := Parse("confirm-paste = false\nconfirm-paste\n")
	if !c.ConfirmPaste || len(diags) != 1 || diags[0].Level != Warning {
		t.Fatalf("config = %+v, diags = %v", c, diags)
	}
}

// cursor-style-blink is Ghostty's: blank lets programs decide (DEC mode 12)
// and the cursor blinks by default; true or false is the default that
// programs can still change with DECSCUSR. A bare key is true.
func TestCursorStyleBlink(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		src  string
		want *bool
	}{
		{"", nil},
		{"cursor-style-blink = true\n", &yes},
		{"cursor-style-blink = false\n", &no},
		{"cursor-style-blink = false\ncursor-style-blink =\n", nil},
		{"cursor-style-blink\n", &yes},
	} {
		c, diags := Parse(tc.src)
		if !reflect.DeepEqual(c.CursorStyleBlink, tc.want) || len(diags) > 1 {
			t.Errorf("%q: blink = %v, diags = %v", tc.src, c.CursorStyleBlink, diags)
		}
	}
	if _, diags := Parse("cursor-style-blink = maybe\n"); len(diags) != 1 || diags[0].Level != Error {
		t.Fatalf("diags = %v", diags)
	}
	b, _ := json.Marshal(Defaults())
	if !strings.Contains(string(b), `"cursor-style-blink":null`) {
		t.Fatalf("json = %s", b)
	}
}

// listen: the listeners a server adds when it starts, checked as
// --extra-listen checks them.
func TestListen(t *testing.T) {
	c, d := Parse("listen = tailnet://:4483 tailnet-serve:///run/hive.sock\n")
	if len(d) != 0 || len(c.Listen) != 2 || c.Listen[0] != "tailnet://:4483" {
		t.Fatalf("listen = %q %v", c.Listen, d)
	}
	if c, d := Parse("listen = nonsense://x\n"); len(d) != 1 || len(c.Listen) != 0 {
		t.Fatalf("bad listen = %q %v", c.Listen, d)
	}
	// A plain listener lets in anyone who reaches it: an option, never a setting.
	if c, d := Parse("listen = 127.0.0.1:4500\n"); len(d) != 1 || len(c.Listen) != 0 {
		t.Fatalf("plain listen = %q %v", c.Listen, d)
	}
	if len(Defaults().Listen) != 0 {
		t.Fatal("a server listens only on its socket by default")
	}
}
