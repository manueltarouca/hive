// Package config reads and writes hive's settings file: Ghostty-style
// `key = value` lines, one implementation for the server, the CLI and (by
// running `hive config`) the app.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config is the settings, with defaults for whatever the file doesn't say.
type Config struct {
	Theme           string  `json:"theme"`
	FontFamily      string  `json:"font-family"`
	FontSize        float64 `json:"font-size"`
	Bell            string  `json:"bell"`
	Notify          string  `json:"notify"`
	ConfirmPaste    bool    `json:"confirm-paste"`
	ScrollbackLimit uint64  `json:"scrollback-limit"`
	ResumeAgents    bool    `json:"resume-agents"`
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
	Name    string
	Default string
	// Who reads it: the app, or the server (which may run elsewhere).
	Server bool
	Help   string
	set    func(c *Config, v string) error
}

const minScrollback = 64 << 10

// Keys lists every setting, in the order the template shows them.
var Keys = []Key{
	{Name: "theme", Default: "system", Help: "system, day or night",
		set: oneOf(func(c *Config, v string) { c.Theme = v }, "system", "day", "night")},
	{Name: "font-family", Default: "", Help: "a monospaced font; empty is hive's bundled JetBrains Mono",
		set: func(c *Config, v string) error { c.FontFamily = v; return nil }},
	{Name: "font-size", Default: "13", Help: "points, 8 to 32",
		set: func(c *Config, v string) error {
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || f < 8 || f > 32 {
				return errors.New("must be a number from 8 to 32")
			}
			c.FontSize = f
			return nil
		}},
	{Name: "bell", Default: "flash", Help: "when a program rings the bell: flash, sound, flash+sound or none (none also skips the Dock bounce)",
		set: oneOf(func(c *Config, v string) { c.Bell = v }, "flash", "sound", "flash+sound", "none")},
	{Name: "notify", Default: "background", Help: "agent notifications: background (when hive isn't in front), always or never",
		set: oneOf(func(c *Config, v string) { c.Notify = v }, "background", "always", "never")},
	{Name: "confirm-paste", Default: "true", Help: "ask before pasting lines into a program that would run each one",
		set: boolean(func(c *Config, b bool) { c.ConfirmPaste = b })},
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
}

func oneOf(apply func(*Config, string), allowed ...string) func(*Config, string) error {
	return func(c *Config, v string) error {
		for _, a := range allowed {
			if v == a {
				apply(c, v)
				return nil
			}
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
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return s, "", false, false
	}
	k, v = strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
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
		if !hasEq {
			diags = append(diags, Diagnostic{n, name, Error, fmt.Sprintf("%q needs a value: key = value", name)})
			continue
		}
		k := key(name)
		if k == nil {
			diags = append(diags, Diagnostic{n, name, Warning, fmt.Sprintf("unknown setting %q", name)})
			continue
		}
		if prev, ok := seen[name]; ok {
			diags = append(diags, Diagnostic{n, name, Warning, fmt.Sprintf("%s is also set on line %d; this one wins", name, prev)})
		}
		seen[name] = n
		if value == "" {
			value = k.Default // an empty value resets, as in Ghostty
		}
		if err := k.set(&c, value); err != nil {
			diags = append(diags, Diagnostic{n, name, Error, fmt.Sprintf("%s %v (got %q)", name, err, value)})
		}
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
	if err != nil || f < 0 {
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
