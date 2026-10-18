package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/manueltarouca/hive/internal/remote"
)

func parseRemote(t *testing.T, args ...string) *remoteFlags {
	t.Helper()
	fs := flags("server run")
	fs.Bool("fresh", false, "")
	fs.String("socket", "", "")
	rf := addRemoteFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return rf
}

func TestRemoteFlagsNone(t *testing.T) {
	rc, err := parseRemote(t, "--fresh", "--socket", "/tmp/x.sock").config(strings.NewReader(""))
	if err != nil || rc != nil {
		t.Fatalf("no remote options: %+v, %v", rc, err)
	}
}

func TestRemoteFlagsConfig(t *testing.T) {
	rc, err := parseRemote(t, "--extra-listen", "tailnet://:4483", "--extra-listen", "tailnet-serve:///run/hive/serve.sock",
		"--tailnet-allow-identified").config(strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rc.Listen) != 2 || rc.Listen[0].Kind != remote.Tailnet || rc.Listen[1].Kind != remote.TailnetServe || !rc.Policy.AllowIdentified {
		t.Fatalf("config = %+v", rc)
	}
}

func TestRemoteFlagsRefuse(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--extra-listen", "4483"}, "--extra-listen"},
		{[]string{"--tailscale-hostname", "box"}, "need --tailscale"},
		{[]string{"--tailscale", "--tailscale-advertise-tag", "tag:ci"}, "no owner"},
		{[]string{"--tailscale", "--tailscale-advertise-tag", "ci", "--tailscale-allow-tailnet-members"}, "tag:NAME"},
	} {
		_, err := parseRemote(t, c.args...).config(strings.NewReader(""))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: err = %v, want it to mention %q", c.args, err, c.want)
		}
	}
}

func TestAuthKeyFromStdin(t *testing.T) {
	rc, err := parseRemote(t, "--tailscale", "--tailscale-auth-key-file", "-").config(strings.NewReader("tskey-auth-abc\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rc.Node == nil || rc.Node.AuthKey != "tskey-auth-abc" {
		t.Fatalf("node = %+v", rc.Node)
	}
}

// `server start` passes its options to the server it spawns. A key read
// from standard input goes in the environment: a command line is readable
// by every account on the machine.
func TestDaemonArgsKeepTheKeyOffTheCommandLine(t *testing.T) {
	rf := parseRemote(t, "--tailscale", "--tailscale-hostname", "box", "--tailscale-auth-key-file", "-",
		"--extra-listen", "tailnet://:4483", "--extra-listen", ":9000", "--allow-anonymous")
	args, env, err := rf.daemonArgs(strings.NewReader("tskey-auth-secret\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(args, " "), "tskey") {
		t.Fatalf("the key is on the command line: %v", args)
	}
	if !slices.Contains(env, "TS_AUTHKEY=tskey-auth-secret") {
		t.Fatalf("env = %v", env)
	}
	for _, want := range []string{"--tailscale=true", "--tailscale-hostname=box", "--allow-anonymous=true"} {
		if !slices.Contains(args, want) {
			t.Errorf("args %v lack %s", args, want)
		}
	}
	if n := strings.Count(strings.Join(args, " "), "--extra-listen"); n != 2 {
		t.Errorf("--extra-listen passed %d times, want 2: %v", n, args)
	}
	// A key file is read by start itself: the server runs in /, so a
	// relative path wouldn't be found.
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("key", []byte("tskey-auth-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fargs, fenv, err := parseRemote(t, "--tailscale", "--tailscale-auth-key-file", "key").daemonArgs(strings.NewReader(""))
	if err != nil || slices.ContainsFunc(fargs, func(a string) bool { return strings.Contains(a, "auth-key-file") }) || !slices.Contains(fenv, "TS_AUTHKEY=tskey-auth-file") {
		t.Fatalf("key file: args %v, env %v, %v", fargs, fenv, err)
	}
	// And the spawned server reads it from there.
	t.Setenv("TS_AUTHKEY", "tskey-auth-secret")
	rc, err := parseRemote(t, args...).config(strings.NewReader(""))
	if err != nil || rc.Node == nil || rc.Node.AuthKey != "tskey-auth-secret" {
		t.Fatalf("spawned server's config: %+v, %v", rc, err)
	}
}
