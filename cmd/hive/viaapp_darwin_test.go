package main

import "testing"

func TestAppOf(t *testing.T) {
	for exe, want := range map[string]string{
		"/Applications/Hive.app/Contents/Resources/hive":  "/Applications/Hive.app",
		"/Users/u/build/Hive.app/Contents/Resources/hive": "/Users/u/build/Hive.app",
		"/Users/u/code/hive/bin/hive":                     "",
		"/opt/homebrew/bin/hive":                          "",
		"/Users/u/Contents/Resources/hive":                "",
		"/Applications/Hive.app/Contents/MacOS/hive":      "",
	} {
		if got := appOf(exe); got != want {
			t.Errorf("appOf(%q) = %q, want %q", exe, got, want)
		}
	}
}

func TestAppFor(t *testing.T) {
	const def = "/tmp/hive-501/server.sock"
	none := func(string) string { return "" }
	installed := func(p string) bool { return p == "/Applications/Hive.app" }
	nowhere := func(string) bool { return false }
	bundled := "/Users/u/build/Hive.app/Contents/Resources/hive"
	loose := "/Users/u/.local/bin/hive"
	for _, c := range []struct {
		name, exe, sock string
		env             map[string]string
		isApp           func(string) bool
		want            string
	}{
		{"the bundle's own binary", bundled, def, nil, nowhere, "/Users/u/build/Hive.app"},
		{"a loose binary, the app installed", loose, def, nil, installed, "/Applications/Hive.app"},
		{"a loose binary, no app", loose, def, nil, nowhere, ""},
		{"another socket (-S)", bundled, "/tmp/other.sock", nil, installed, ""},
		{"a test's server", bundled, def, map[string]string{"HIVE_SERVER": def}, installed, ""},
		{"a second state dir", loose, def, map[string]string{"HIVE_STATE_DIR": "/tmp/s"}, installed, ""},
		{"a test's config", loose, def, map[string]string{"HIVE_CONFIG": "/tmp/c"}, installed, ""},
		{"a test's scripts", loose, def, map[string]string{"HIVE_SCRIPTS": "/tmp/x"}, installed, ""},
	} {
		getenv := none
		if c.env != nil {
			getenv = func(k string) string { return c.env[k] }
		}
		if got := appFor(c.exe, c.sock, def, getenv, c.isApp, "/Users/u"); got != c.want {
			t.Errorf("%s: appFor = %q, want %q", c.name, got, c.want)
		}
	}
}
