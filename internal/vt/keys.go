package vt

import (
	"fmt"
	"strings"
	"unicode"

	ghostty "go.mitchellh.com/libghostty"
)

type keyEvent struct {
	key  ghostty.Key
	mods ghostty.Mods
	utf8 string
	cp   rune
}

var namedKeys = map[string]ghostty.Key{
	"enter": ghostty.KeyEnter, "return": ghostty.KeyEnter,
	"escape": ghostty.KeyEscape, "esc": ghostty.KeyEscape,
	"tab": ghostty.KeyTab, "space": ghostty.KeySpace,
	"backspace": ghostty.KeyBackspace, "delete": ghostty.KeyDelete,
	"up": ghostty.KeyArrowUp, "down": ghostty.KeyArrowDown,
	"left": ghostty.KeyArrowLeft, "right": ghostty.KeyArrowRight,
	"home": ghostty.KeyHome, "end": ghostty.KeyEnd,
	"pageup": ghostty.KeyPageUp, "pagedown": ghostty.KeyPageDown,
	"insert": ghostty.KeyInsert,
	"f1":     ghostty.KeyF1, "f2": ghostty.KeyF2, "f3": ghostty.KeyF3, "f4": ghostty.KeyF4,
	"f5": ghostty.KeyF5, "f6": ghostty.KeyF6, "f7": ghostty.KeyF7, "f8": ghostty.KeyF8,
	"f9": ghostty.KeyF9, "f10": ghostty.KeyF10, "f11": ghostty.KeyF11, "f12": ghostty.KeyF12,
}

var namedText = map[string]string{"enter": "\r", "return": "\r", "tab": "\t", "space": " "}

var modNames = map[string]ghostty.Mods{
	"ctrl": ghostty.ModCtrl, "control": ghostty.ModCtrl,
	"alt": ghostty.ModAlt, "opt": ghostty.ModAlt, "option": ghostty.ModAlt,
	"shift": ghostty.ModShift,
	"cmd":   ghostty.ModSuper, "super": ghostty.ModSuper, "command": ghostty.ModSuper, "meta": ghostty.ModSuper,
}

// letter and digit keys by their character.
func charKey(r rune) (ghostty.Key, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return ghostty.KeyA + ghostty.Key(r-'a'), true
	case r >= '0' && r <= '9':
		return ghostty.KeyDigit0 + ghostty.Key(r-'0'), true
	}
	k, ok := punctKeys[r]
	return k, ok
}

var punctKeys = map[rune]ghostty.Key{
	'`': ghostty.KeyBackquote, '\\': ghostty.KeyBackslash, '[': ghostty.KeyBracketLeft,
	']': ghostty.KeyBracketRight, ',': ghostty.KeyComma, '=': ghostty.KeyEqual,
	'-': ghostty.KeyMinus, '.': ghostty.KeyPeriod, '\'': ghostty.KeyQuote,
	';': ghostty.KeySemicolon, '/': ghostty.KeySlash,
}

func parseKey(name string) (keyEvent, error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(name)), "+")
	var ev keyEvent
	for _, p := range parts[:len(parts)-1] {
		m, ok := modNames[p]
		if !ok {
			return ev, fmt.Errorf("unknown modifier %q in %q", p, name)
		}
		ev.mods |= m
	}
	last := parts[len(parts)-1]
	if k, ok := namedKeys[last]; ok {
		ev.key = k
		if ev.mods&^ghostty.ModShift == 0 {
			ev.utf8 = namedText[last]
		}
		if last == "space" {
			ev.cp = ' '
		}
		return ev, nil
	}
	r := []rune(last)
	if len(r) != 1 {
		return ev, fmt.Errorf("unknown key %q", name)
	}
	k, ok := charKey(r[0])
	if !ok {
		return ev, fmt.Errorf("unknown key %q", name)
	}
	ev.key, ev.cp = k, r[0]
	if ev.mods&^ghostty.ModShift == 0 {
		s := string(r[0])
		if ev.mods&ghostty.ModShift != 0 {
			s = string(unicode.ToUpper(r[0]))
		}
		ev.utf8 = s
	}
	return ev, nil
}
