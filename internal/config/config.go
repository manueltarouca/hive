// Package config reads and writes hive's settings file: Ghostty-style
// `key = value` lines, one implementation for the server, the CLI and (by
// running `hive config`) the app.
package config

import (
	"errors"
	"fmt"
	"github.com/manueltarouca/hive/internal/keymap"
	"github.com/manueltarouca/hive/internal/remote"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Config is the settings, with defaults for whatever the file doesn't say.
type Config struct {
	Theme        string  `json:"theme"`
	FontFamily   string  `json:"font-family"`
	FontSize     float64 `json:"font-size"`
	Bell         string  `json:"bell"`
	Notify       string  `json:"notify"`
	ConfirmPaste bool    `json:"confirm-paste"`
	// Motion is how tabs, panes and the sidebar move: snappy, natural or off.
	Motion string `json:"motion"`
	// ConfirmClose is when closing a tab or its last pane asks first.
	ConfirmClose string `json:"confirm-close"`
	// DimPanes is how much the panes you're not typing in step back.
	DimPanes string `json:"dim-panes"`
	// BalanceSplits evens out a tab's panes when the app opens or closes one.
	BalanceSplits bool `json:"balance-splits"`
	// FocusFollowsPointer focuses a pane when the pointer moves over it.
	FocusFollowsPointer bool `json:"focus-follows-pointer"`
	// Per agent state: whether it notifies, and the sound it plays.
	NotifyBlocked bool   `json:"notify-blocked"`
	NotifyDone    bool   `json:"notify-done"`
	NotifyStopped bool   `json:"notify-stopped"`
	SoundBlocked  string `json:"sound-blocked"`
	SoundDone     string `json:"sound-done"`
	SoundStopped  string `json:"sound-stopped"`
	// DockBadge counts the agents that need you on the Dock icon.
	DockBadge bool `json:"dock-badge"`
	// KeyHints shows the keys that can follow a sequence's first key, and a mode's keys.
	KeyHints bool `json:"key-hints"`
	// CopyOnSelect copies text as soon as it's selected.
	CopyOnSelect bool `json:"copy-on-select"`
	// Nil lets programs decide, blinking by default (Ghostty's blank).
	CursorStyleBlink *bool  `json:"cursor-style-blink"`
	ScrollbackLimit  uint64 `json:"scrollback-limit"`
	ResumeAgents     bool   `json:"resume-agents"`
	// Listeners the server adds when it starts, as --extra-listen takes them.
	Listen []string `json:"listen"`
	// Tailscale starts the server as its own Tailscale device (as --tailscale).
	Tailscale         bool   `json:"tailscale"`
	TailscaleHostname string `json:"tailscale-hostname"`
	// ScriptRemoteRun lets callers on other machines run scripts here.
	ScriptRemoteRun bool `json:"script-remote-run"`
	// Scripts runs the scripts in the scripts folder.
	Scripts bool `json:"scripts"`
	// Keybind changes the apps' shortcuts: one KEY=ACTION per line.
	Keybind []string `json:"keybind"`
}

// Level is how bad a diagnostic is: an error keeps the default.
type Level string

const (
	Warning Level = "warning"
	Error   Level = "error"
)

// Diagnostic is one problem with one line of the file.
type Diagnostic struct {
	Line    int    `json:"line"`
	Key     string `json:"key,omitempty"`
	Level   Level  `json:"level"`
	Message string `json:"message"`
}

func (d Diagnostic) String() string { return fmt.Sprintf("line %d: %s", d.Line, d.Message) }

// Key describes one setting.
type Key struct {
	Name    string `json:"name"`
	Default string `json:"default"`
	// Who reads it: the app, or the server (which may run elsewhere).
	Server bool   `json:"server"`
	Help   string `json:"help"`
	// The values it takes, when they're a fixed list.
	Choices []string `json:"choices,omitempty"`
	// Repeat: the key may appear on many lines, each adding a value.
	Repeat bool `json:"repeat,omitempty"`
	set    func(c *Config, v string) error
}

const minScrollback = 64 << 10

// maxSize caps sizes at 1 TiB: anything larger is a typo, and floats that
// big no longer convert to an integer exactly.
const maxSize = 1 << 40

// sounds are the macOS sounds an agent state can play, or none.
var sounds = []string{"none", "Basso", "Blow", "Bottle", "Frog", "Funk", "Glass", "Hero", "Morse", "Ping", "Pop", "Purr", "Sosumi", "Submarine", "Tink"}

// Keys lists every setting, in the order the template shows them.
var Keys = []Key{
	{Name: "theme", Default: "system", Help: "system, day or night", Choices: []string{"system", "day", "night"},
		set: oneOf(func(c *Config, v string) { c.Theme = v }, "system", "day", "night")},
	{Name: "font-family", Default: "", Help: "a monospaced font; empty is hive's bundled JetBrains Mono",
		set: func(c *Config, v string) error { c.FontFamily = v; return nil }},
	{Name: "font-size", Default: "13", Help: "points, 8 to 32",
		set: func(c *Config, v string) error {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || math.IsNaN(f) || f < 8 || f > 32 {
				return errors.New("must be a number from 8 to 32")
			}
			c.FontSize = f
			return nil
		}},
	{Name: "bell", Default: "flash", Choices: []string{"flash", "sound", "flash+sound", "none"}, Help: "when a program rings the bell: flash, sound, flash+sound or none (none also skips the Dock bounce)",
		set: oneOf(func(c *Config, v string) { c.Bell = v }, "flash", "sound", "flash+sound", "none")},
	{Name: "notify", Default: "background", Choices: []string{"background", "always", "never"}, Help: "agent notifications: background (when hive isn't in front), always or never",
		set: oneOf(func(c *Config, v string) { c.Notify = v }, "background", "always", "never")},
	{Name: "confirm-paste", Default: "true", Help: "ask before pasting lines into a program that would run each one",
		set: boolean(func(c *Config, b bool) { c.ConfirmPaste = b })},
	{Name: "motion", Default: "snappy", Choices: []string{"snappy", "natural", "off"}, Help: "how tabs move: snappy settles quickly, natural springs with a slight overshoot, off moves them at once. Reduce Motion in macOS always moves them at once",
		set: oneOf(func(c *Config, v string) { c.Motion = v }, "snappy", "natural", "off")},
	{Name: "confirm-close", Default: "running", Choices: []string{"running", "always", "never"}, Help: "ask before closing a tab, or its last pane: running (only when a program other than the shell runs in it), always or never",
		set: oneOf(func(c *Config, v string) { c.ConfirmClose = v }, "running", "always", "never")},
	{Name: "dim-panes", Default: "light", Choices: []string{"light", "more", "off"}, Help: "how much the panes you're not typing in step back: light, more or off",
		set: oneOf(func(c *Config, v string) { c.DimPanes = v }, "light", "more", "off")},
	{Name: "balance-splits", Default: "true", Help: "give every pane in a tab the same room when the app opens or closes one; this replaces sizes set by dragging or --ratio",
		set: boolean(func(c *Config, b bool) { c.BalanceSplits = b })},
	{Name: "focus-follows-pointer", Default: "false", Help: "focus a pane when the pointer moves over it, without a click",
		set: boolean(func(c *Config, b bool) { c.FocusFollowsPointer = b })},
	{Name: "notify-blocked", Default: "true", Help: "notify when an agent needs you (when notify allows)",
		set: boolean(func(c *Config, b bool) { c.NotifyBlocked = b })},
	{Name: "notify-done", Default: "true", Help: "notify when an agent is done (when notify allows)",
		set: boolean(func(c *Config, b bool) { c.NotifyDone = b })},
	{Name: "notify-stopped", Default: "true", Help: "notify when an agent stopped and won't go on by itself, such as at a usage limit (when notify allows)",
		set: boolean(func(c *Config, b bool) { c.NotifyStopped = b })},
	{Name: "sound-blocked", Default: "Tink", Choices: sounds, Help: "the sound when an agent needs you: a macOS sound, or none",
		set: oneOf(func(c *Config, v string) { c.SoundBlocked = v }, sounds...)},
	{Name: "sound-done", Default: "Glass", Choices: sounds, Help: "the sound when an agent is done: a macOS sound, or none",
		set: oneOf(func(c *Config, v string) { c.SoundDone = v }, sounds...)},
	{Name: "sound-stopped", Default: "Basso", Choices: sounds, Help: "the sound when an agent stopped: a macOS sound, or none",
		set: oneOf(func(c *Config, v string) { c.SoundStopped = v }, sounds...)},
	{Name: "dock-badge", Default: "true", Help: "count the agents that need you on hive's Dock icon",
		set: boolean(func(c *Config, b bool) { c.DockBadge = b })},
	{Name: "copy-on-select", Default: "false", Help: "copy text as soon as you select it with the mouse; cmd+C still works",
		set: boolean(func(c *Config, b bool) { c.CopyOnSelect = b })},
	{Name: "key-hints", Default: "true", Help: "show the keys that can follow while a key sequence is half typed, and a mode's keys",
		set: boolean(func(c *Config, b bool) { c.KeyHints = b })},
	{Name: "cursor-style-blink", Default: "", Choices: []string{"", "true", "false"}, Help: "whether the cursor blinks: blank or true blinks, as in Ghostty; false keeps it steady. Programs can still change it (DECSCUSR, DEC mode 12)",
		set: func(c *Config, v string) error {
			if v == "" {
				c.CursorStyleBlink = nil
				return nil
			}
			return boolean(func(c *Config, b bool) { c.CursorStyleBlink = &b })(c, v)
		}},
	{Name: "scrollback-limit", Default: "16m", Server: true, Help: "history kept per terminal, in bytes (k, m, g); at least 64k. Lowering it drops old lines at once",
		set: func(c *Config, v string) error {
			n, err := parseSize(v)
			if err != nil || n < minScrollback {
				return errors.New("must be a size of at least 64k, such as 16m")
			}
			c.ScrollbackLimit = n
			return nil
		}},
	{Name: "resume-agents", Default: "true", Server: true, Help: "resume agents' sessions when the server starts again",
		set: boolean(func(c *Config, b bool) { c.ResumeAgents = b })},
	{Name: "listen", Default: "", Server: true, Help: "listeners to add when the server starts, separated by spaces, such as tailnet://:4483 (see the Remote hosts docs). Read at start; listener options to hive server start replace it",
		set: func(c *Config, v string) error {
			var out []string
			for _, l := range strings.Fields(v) {
				sp, err := remote.ParseSpec(l)
				if err != nil {
					return fmt.Errorf("%s: %v", l, err)
				}
				// A plain listener lets in anyone who reaches it: an option
				// for a test, never a standing setting.
				if sp.Kind == remote.Plain {
					return fmt.Errorf("%s: only tailnet:// and tailnet-serve:// listeners can be a setting", l)
				}
				if !slices.Contains(out, l) { // the same listener twice would fight over its port
					out = append(out, l)
				}
			}
			c.Listen = out
			return nil
		}},
	{Name: "tailscale", Default: "false", Server: true, Help: "join your tailnet as the server's own device, built in, so this Mac needs no Tailscale app (as --tailscale). Read at start; the first time, sign in from Settings, Server, or the link in the server's log",
		set: boolean(func(c *Config, b bool) { c.Tailscale = b })},
	{Name: "tailscale-hostname", Default: "", Server: true, Help: "the built-in Tailscale device's name; empty is hive",
		set: func(c *Config, v string) error {
			for _, r := range v {
				if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
					return errors.New("must be lowercase letters, digits and dashes")
				}
			}
			c.TailscaleHostname = v
			return nil
		}},
	{Name: "keybind", Default: "", Server: true, Repeat: true, Help: "change a shortcut, one per line: KEY=ACTION, such as cmd+shift+t=client.new_tab, or KEY=unbind; see the Scripts docs",
		set: func(c *Config, v string) error {
			if v == "" {
				c.Keybind = nil
				return nil
			}
			if _, err := keymap.ParseBinding(v); err != nil {
				return err
			}
			c.Keybind = append(c.Keybind, v)
			return nil
		}},
	{Name: "scripts", Default: "true", Server: true, Help: "run the scripts in the scripts folder (beside this file) when the server starts",
		set: boolean(func(c *Config, b bool) { c.Scripts = b })},
	{Name: "script-remote-run", Default: "false", Server: true, Help: "let callers on other machines (over Tailscale) run scripts on this server",
		set: boolean(func(c *Config, b bool) { c.ScriptRemoteRun = b })},
}

func oneOf(apply func(*Config, string), allowed ...string) func(*Config, string) error {
	return func(c *Config, v string) error {
		if slices.Contains(allowed, v) {
			apply(c, v)
			return nil
		}
		return fmt.Errorf("must be %s", strings.Join(allowed, ", "))
	}
}

func boolean(apply func(*Config, bool)) func(*Config, string) error {
	return func(c *Config, v string) error {
		switch strings.ToLower(v) {
		case "true", "yes", "on":
			apply(c, true)
		case "false", "no", "off":
			apply(c, false)
		default:
			return errors.New("must be true or false")
		}
		return nil
	}
}

func key(name string) *Key {
	for i := range Keys {
		if Keys[i].Name == name {
			return &Keys[i]
		}
	}
	return nil
}

// Defaults is the settings with nothing in the file.
func Defaults() Config {
	var c Config
	for _, k := range Keys {
		if err := k.set(&c, k.Default); err != nil {
			panic(fmt.Sprintf("default %s: %v", k.Name, err))
		}
	}
	return c
}

// line splits one line the way Ghostty does: trimmed, whole-line comments
// and blanks skipped, key and value split at the first =, quotes removed.
func line(raw string) (k, v string, hasEq, skip bool) {
	s := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
	if s == "" || s[0] == '#' {
		return "", "", false, true
	}
	before, after, ok := strings.Cut(s, "=")
	if !ok {
		return s, "", false, false
	}
	k, v = strings.TrimSpace(before), strings.TrimSpace(after)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
	}
	return k, v, true, false
}

// Parse reads a file's text. It never fails: problems come back as
// diagnostics with line numbers, and a bad value keeps the default.
func Parse(src string) (Config, []Diagnostic) {
	c := Defaults()
	var diags []Diagnostic
	seen := map[string]int{}
	src = strings.TrimPrefix(src, "\xef\xbb\xbf")
	for i, raw := range strings.Split(src, "\n") {
		n := i + 1
		name, value, hasEq, skip := line(raw)
		if skip {
			continue
		}
		k := key(name)
		if !hasEq {
			if k != nil && (k.Default == "true" || k.Default == "false" || k.Name == "cursor-style-blink") {
				value = "true" // a boolean alone means true, as in Ghostty
			} else {
				diags = append(diags, Diagnostic{n, name, Error, fmt.Sprintf("%q needs a value: key = value", name)})
				continue
			}
		}
		if k == nil {
			diags = append(diags, Diagnostic{n, name, Warning, fmt.Sprintf("unknown setting %q", name)})
			continue
		}
		if value == "" {
			value = k.Default // an empty value resets, as in Ghostty
		}
		if err := k.set(&c, value); err != nil {
			diags = append(diags, Diagnostic{n, name, Error, fmt.Sprintf("%s %v (got %q)", name, err, value)})
			continue // an earlier line's value, if any, stays
		}
		if prev, ok := seen[name]; ok && !k.Repeat {
			diags = append(diags, Diagnostic{n, name, Warning, fmt.Sprintf("%s is also set on line %d; this one wins", name, prev)})
		}
		seen[name] = n
	}
	return c, diags
}

// parseSize reads 65536, 64k, 16m, 1g or 2.5m (powers of 1024).
func parseSize(s string) (uint64, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mult, s = 1<<10, s[:len(s)-1]
	case strings.HasSuffix(s, "m"):
		mult, s = 1<<20, s[:len(s)-1]
	case strings.HasSuffix(s, "g"):
		mult, s = 1<<30, s[:len(s)-1]
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f*mult > maxSize {
		return 0, fmt.Errorf("bad size %q", s)
	}
	return uint64(f * mult), nil
}

// Path is the settings file: HIVE_CONFIG (tests, screenshots), else
// $XDG_CONFIG_HOME/hive/config, else ~/.config/hive/config.
func Path() string {
	if p := os.Getenv("HIVE_CONFIG"); p != "" {
		return p
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "hive", "config")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hive", "config")
}

// Scripts is the folder of resident scripts: HIVE_SCRIPTS, or scripts/
// beside the settings file.
func Scripts() string {
	if d := os.Getenv("HIVE_SCRIPTS"); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(Path()), "scripts")
}

// Load reads the file at path. A missing file is the defaults; an unreadable
// one is the defaults and one diagnostic.
func Load(path string) (Config, []Diagnostic) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), []Diagnostic{{Level: Error, Message: fmt.Sprintf("can't read %s: %v", path, err)}}
	}
	return Parse(string(b))
}

// Template is a new file: every setting commented out with its default.
func Template() string {
	var b strings.Builder
	b.WriteString("# hive settings. Each line is `key = value`; a line starting with # is a comment.\n")
	b.WriteString("# Everything here is commented out at its default: remove the # to change it.\n")
	b.WriteString("# The app reloads this file when you save it. Check it with `hive config check`.\n")
	for _, k := range Keys {
		b.WriteString("\n# " + k.Help + ".")
		if k.Server {
			b.WriteString(" Read by the server.")
		}
		b.WriteString("\n# " + k.Name + " = " + k.Default + "\n")
	}
	return b.String()
}
