// Package keymap is hive's key bindings as data: the apps' default
// shortcuts, what scripts bind, and what the settings file changes. The
// server merges them; apps read the result and resolve keys themselves, so
// typing never waits on the network.
package keymap

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Chord is one key with its modifiers.
type Chord struct {
	Ctrl, Alt, Shift, Cmd bool
	Key                   string
}

// Key is a chord, or a sequence of them (ctrl+b>c).
type Key []Chord

var named = map[string]string{
	"enter": "enter", "return": "enter", "escape": "escape", "esc": "escape", "tab": "tab", "space": "space",
	"backspace": "backspace", "delete": "delete", "up": "up", "down": "down", "left": "left", "right": "right",
	"home": "home", "end": "end", "pageup": "pageup", "pagedown": "pagedown",
}

var physical = regexp.MustCompile(`^\[[A-Za-z0-9]+\]$`)

func parseChord(s string) (Chord, error) {
	var c Chord
	parts := strings.Split(strings.TrimSpace(s), "+")
	// A last part that is empty means the key is "+" itself: cmd++.
	if len(parts) > 1 && parts[len(parts)-1] == "" && len(parts) > 2 && parts[len(parts)-2] == "" {
		parts = append(parts[:len(parts)-2], "+")
	}
	for i, p := range parts {
		p = strings.TrimSpace(p)
		last := i == len(parts)-1
		switch strings.ToLower(p) {
		case "ctrl", "control":
			if last {
				return c, fmt.Errorf("%q: no key after the modifiers", s)
			}
			c.Ctrl = true
			continue
		case "alt", "opt", "option":
			if last {
				return c, fmt.Errorf("%q: no key after the modifiers", s)
			}
			c.Alt = true
			continue
		case "shift":
			if last {
				return c, fmt.Errorf("%q: no key after the modifiers", s)
			}
			c.Shift = true
			continue
		case "cmd", "command", "super":
			if last {
				return c, fmt.Errorf("%q: no key after the modifiers", s)
			}
			c.Cmd = true
			continue
		}
		if !last {
			return c, fmt.Errorf("%q: %q isn't a modifier (ctrl, alt, shift, cmd)", s, p)
		}
		switch {
		case p == "":
			return c, fmt.Errorf("%q: no key", s)
		case named[strings.ToLower(p)] != "":
			c.Key = named[strings.ToLower(p)]
		case len(p) > 1 && (p[0] == 'f' || p[0] == 'F') && isNum(p[1:]):
			c.Key = strings.ToLower(p)
		case physical.MatchString(p):
			c.Key = p
		case len([]rune(p)) == 1:
			c.Key = strings.ToLower(p)
		default:
			return c, fmt.Errorf("%q: unknown key %q", s, p)
		}
	}
	return c, nil
}

func isNum(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != "" && len(s) <= 2
}

// Parse reads a key: cmd+shift+d, ctrl+[KeyA] (a physical key), or a
// sequence such as ctrl+b>c.
func Parse(s string) (Key, error) {
	if strings.TrimSpace(s) == "" {
		return nil, fmt.Errorf("no key")
	}
	var k Key
	for _, part := range strings.Split(s, ">") {
		c, err := parseChord(part)
		if err != nil {
			return nil, err
		}
		k = append(k, c)
	}
	return k, nil
}

func (c Chord) String() string {
	var b strings.Builder
	for _, m := range []struct {
		on   bool
		name string
	}{{c.Ctrl, "ctrl"}, {c.Alt, "alt"}, {c.Shift, "shift"}, {c.Cmd, "cmd"}} {
		if m.on {
			b.WriteString(m.name + "+")
		}
	}
	b.WriteString(c.Key)
	return b.String()
}

// String is the key in its one spelling, modifiers in macOS's order.
func (k Key) String() string {
	parts := make([]string, len(k))
	for i, c := range k {
		parts[i] = c.String()
	}
	return strings.Join(parts, ">")
}

// Unbind as a binding's action removes the key.
const Unbind = "unbind"

// Binding ties a key, in a mode or not, to an action.
type Binding struct {
	Key    string          `json:"key"`
	Mode   string          `json:"mode,omitempty"`
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args,omitempty"`
	// Title names what it does, for menus and the shortcuts window.
	Title  string `json:"title,omitempty"`
	Source string `json:"source" desc:"default, script:NAME or settings"`
}

// Mode is a set of bindings that apply while it's on (entered with the
// client.mode action, left with Escape). An exclusive mode swallows every
// other key.
type Mode struct {
	Name      string `json:"name"`
	Exclusive bool   `json:"exclusive,omitempty"`
	Source    string `json:"source"`
}

// Keymap is what a client reads.
type Keymap struct {
	Bindings []Binding `json:"bindings"`
	Modes    []Mode    `json:"modes"`
}

var actionRE = regexp.MustCompile(`^(client\.)?[a-z0-9][a-z0-9_.-]*$`)

// ParseBinding reads a settings line's value: KEY=ACTION, KEY=ACTION:ARGS
// (JSON), MODE/KEY=..., or KEY=unbind.
func ParseBinding(s string) (Binding, error) {
	lhs, rhs, ok := strings.Cut(s, "=")
	// cmd+= binds the = key: the split is at the last = before the action.
	if i := strings.LastIndex(s, "=="); i >= 0 {
		lhs, rhs, ok = s[:i+1], s[i+2:], true
	}
	if !ok || strings.TrimSpace(rhs) == "" {
		return Binding{}, fmt.Errorf("%q: KEY=ACTION, such as cmd+shift+t=client.new_tab", s)
	}
	var b Binding
	lhs = strings.TrimSpace(lhs)
	if mode, key, ok := strings.Cut(lhs, "/"); ok && key != "" && !strings.HasSuffix(lhs, "/") {
		b.Mode, lhs = strings.TrimSpace(mode), key
	}
	k, err := Parse(lhs)
	if err != nil {
		return Binding{}, err
	}
	b.Key = k.String()
	action, args, hasArgs := strings.Cut(strings.TrimSpace(rhs), ":")
	b.Action = strings.TrimSpace(action)
	if !actionRE.MatchString(b.Action) {
		return Binding{}, fmt.Errorf("%q: an action is a name such as client.new_tab or a script's action", b.Action)
	}
	if hasArgs {
		if !json.Valid([]byte(args)) {
			return Binding{}, fmt.Errorf("%q: arguments are JSON, such as {\"direction\":\"left\"}", args)
		}
		b.Args = json.RawMessage(args)
	}
	b.Source = "settings"
	return b, nil
}

// Merge layers bindings: each layer's key replaces an earlier one's, and
// unbind removes it. It says, in plain words, where layers fight and where
// a key acts alone and also starts a sequence.
func Merge(layers ...[]Binding) (Keymap, []string) {
	type slot struct{ mode, key string }
	final := map[slot]Binding{}
	var order []slot
	var warns []string
	for _, layer := range layers {
		for _, b := range layer {
			s := slot{b.Mode, b.Key}
			if old, ok := final[s]; ok && old.Source != b.Source && b.Action != Unbind {
				warns = append(warns, fmt.Sprintf("%s: %s (%s) replaces %s (%s)", b.Key, b.Action, b.Source, old.Action, old.Source))
			}
			if _, ok := final[s]; !ok {
				order = append(order, s)
			}
			final[s] = b
		}
	}
	km := Keymap{Bindings: []Binding{}, Modes: []Mode{}}
	modes := map[string]bool{}
	for _, s := range order {
		b := final[s]
		if b.Action == Unbind {
			continue
		}
		km.Bindings = append(km.Bindings, b)
		if b.Mode != "" && !modes[b.Mode] {
			modes[b.Mode] = true
			km.Modes = append(km.Modes, Mode{Name: b.Mode, Source: b.Source})
		}
	}
	for _, b := range km.Bindings {
		k, _ := Parse(b.Key)
		if len(k) < 2 {
			continue
		}
		if alone, ok := final[slot{b.Mode, k[0].String()}]; ok && alone.Action != Unbind {
			warns = append(warns, fmt.Sprintf("%s acts alone (%s) and also starts %s (%s): the sequence never runs", k[0], alone.Action, b.Key, b.Action))
		}
	}
	sort.Strings(warns)
	return km, warns
}

func arg(k, v string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{k: v})
	return b
}

// Defaults are an app's own shortcuts, as data: client actions the app
// runs itself.
func Defaults(clientKind string) []Binding {
	if clientKind != "mac" {
		return []Binding{}
	}
	d := func(key, action, title string, args json.RawMessage) Binding {
		return Binding{Key: key, Action: action, Title: title, Args: args, Source: "default"}
	}
	out := []Binding{
		d("cmd+,", "client.settings", "Settings…", nil),
		d("shift+cmd+p", "client.palette", "Command Palette…", nil),
		d("shift+cmd+n", "client.new_session", "New Session", nil),
		d("cmd+k", "client.session_picker", "Change Session…", nil),
		d("cmd+t", "client.new_tab", "New Tab", nil),
		d("cmd+d", "client.split_right", "Split Right", nil),
		d("shift+cmd+d", "client.split_down", "Split Down", nil),
		d("cmd+w", "client.close_pane", "Close Pane", nil),
		d("shift+cmd+w", "client.close_tab", "Close Tab", nil),
		d("shift+cmd+s", "client.sidebar", "Toggle Agents Sidebar", nil),
		d("shift+cmd+enter", "client.zoom", "Zoom Pane", nil),
		d("shift+cmd+]", "client.next_tab", "Next Tab", nil),
		d("shift+cmd+[", "client.previous_tab", "Previous Tab", nil),
		d("cmd+/", "client.shortcuts", "Keyboard Shortcuts", nil),
		// Keys any layout types without Option: on a Portuguese keyboard
		// [, ] and / need Option or Shift. Apps show the first binding of an
		// action the keyboard in use can type.
		d("ctrl+tab", "client.next_tab", "Next Tab", nil),
		d("ctrl+shift+tab", "client.previous_tab", "Previous Tab", nil),
		d("cmd+'", "client.shortcuts", "Keyboard Shortcuts", nil),
	}
	for _, dir := range []string{"left", "right", "up", "down"} {
		name := strings.ToUpper(dir[:1]) + dir[1:]
		out = append(out, d("alt+cmd+"+dir, "client.focus", "Focus "+name, arg("direction", dir)))
	}
	for _, dir := range []string{"left", "right", "up", "down"} {
		name := strings.ToUpper(dir[:1]) + dir[1:]
		out = append(out, d("ctrl+cmd+"+dir, "client.resize", "Resize "+name, arg("direction", dir)))
	}
	for i := 1; i <= 9; i++ {
		b, _ := json.Marshal(map[string]int{"index": i - 1})
		out = append(out, d(fmt.Sprintf("cmd+%d", i), "client.tab", fmt.Sprintf("Tab %d", i), b))
	}
	return out
}
