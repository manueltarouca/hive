package keymap

import (
	"strings"
	"testing"
)

func TestParseKeys(t *testing.T) {
	for in, want := range map[string]string{
		"cmd+shift+d":      "shift+cmd+d",
		"Shift+Cmd+D":      "shift+cmd+d",
		"super+t":          "cmd+t",
		"opt+left":         "alt+left",
		"ctrl+b>c":         "ctrl+b>c",
		"ctrl+b > shift+c": "ctrl+b>shift+c",
		"cmd+return":       "cmd+enter",
		"esc":              "escape",
		"cmd+[":            "cmd+[",
		"ctrl+[KeyA]":      "ctrl+[KeyA]",
		"f5":               "f5",
		"cmd+=":            "cmd+=",
	} {
		k, err := Parse(in)
		if err != nil || k.String() != want {
			t.Errorf("%q = %q, %v; want %q", in, k.String(), err, want)
		}
	}
	for _, bad := range []string{"", "cmd+", "cmd+shift", "hyper+x", "cmd+xyz", "ctrl+b>", ">c", "cmd+t+t"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestParseLine(t *testing.T) {
	b, err := ParseBinding("cmd+shift+t=client.new_tab")
	if err != nil || b.Key != "shift+cmd+t" || b.Action != "client.new_tab" || b.Mode != "" {
		t.Fatalf("%+v %v", b, err)
	}
	b, err = ParseBinding(`resize/left=client.resize:{"direction":"left"}`)
	if err != nil || b.Mode != "resize" || b.Key != "left" || b.Action != "client.resize" || string(b.Args) != `{"direction":"left"}` {
		t.Fatalf("%+v %v", b, err)
	}
	if b, err := ParseBinding("cmd+t=unbind"); err != nil || b.Action != Unbind {
		t.Fatalf("%+v %v", b, err)
	}
	for _, bad := range []string{"cmd+t", "cmd+t=", "cmd+t=Bad Name", `cmd+t=x:{nope`} {
		if _, err := ParseBinding(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// Defaults, then scripts, then the settings file: the later layer wins a
// key, unbind removes one, and a key that starts a sequence and also acts
// alone is reported.
func TestMerge(t *testing.T) {
	scripts := []Binding{
		{Key: "cmd+t", Action: "scratch", Source: "script:s"},
		{Key: "ctrl+b>c", Action: "client.new_tab", Source: "script:s"},
	}
	user := []Binding{
		{Key: "cmd+t", Action: "client.new_tab", Source: "settings"},
		{Key: "shift+cmd+w", Action: Unbind, Source: "settings"},
		{Key: "ctrl+b", Action: "client.palette", Source: "settings"},
	}
	km, warns := Merge(Defaults("mac"), scripts, user)
	got := map[string]Binding{}
	for _, b := range km.Bindings {
		got[b.Mode+"/"+b.Key] = b
	}
	if b := got["/cmd+t"]; b.Action != "client.new_tab" || b.Source != "settings" {
		t.Errorf("cmd+t = %+v", b)
	}
	if _, ok := got["/shift+cmd+w"]; ok {
		t.Error("unbind kept shift+cmd+w")
	}
	if got["/cmd+d"].Action != "client.split_right" {
		t.Errorf("defaults lost: %+v", got["/cmd+d"])
	}
	joined := strings.Join(warns, "\n")
	if !strings.Contains(joined, "cmd+t") || !strings.Contains(joined, "ctrl+b") {
		t.Errorf("warnings = %q", joined)
	}
}

func TestDefaultsAreValid(t *testing.T) {
	for _, b := range Defaults("mac") {
		if k, err := Parse(b.Key); err != nil || k.String() != b.Key {
			t.Errorf("default %q: %v", b.Key, err)
		}
		if b.Title == "" {
			t.Errorf("default %q has no title", b.Key)
		}
	}
}

// Every default action has a key a Portuguese (PT-PT) keyboard types
// without Option: letters, digits, named keys, and , . - ' + < on it.
func TestDefaultsTypeOnPortuguese(t *testing.T) {
	direct := "abcdefghijklmnopqrstuvwxyz0123456789,.-'+<"
	ok := map[string]bool{}
	for _, b := range Defaults("mac") {
		k, _ := Parse(b.Key)
		key := k[0].Key
		if len(k) == 1 && (len(key) > 1 || strings.Contains(direct, key)) {
			ok[b.Action] = true
		}
	}
	for _, b := range Defaults("mac") {
		if !ok[b.Action] {
			t.Errorf("%s (%s) has no key a PT-PT keyboard types without Option", b.Action, b.Key)
		}
	}
}
