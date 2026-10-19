package main

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/hosts"
)

func TestResolveGrammar(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIVE_CONFIG", filepath.Join(dir, "config"))
	// No network: names resolve from a table, and a probe answers at the
	// host's first candidate.
	oldLookup, oldProbe := lookupHost, probe
	defer func() { lookupHost, probe = oldLookup, oldProbe }()
	lookupHost = func(_ context.Context, name string) ([]string, error) {
		if name == "shortname" {
			return []string{"100.64.0.9"}, nil
		}
		return nil, errors.New("no such host")
	}
	probe = func(_ context.Context, host string) (*client.Client, error) {
		return client.NewURL(client.Candidates(host)[0])
	}
	if err := hosts.Add(hosts.Host{Label: "hivetest-box", Address: "https://box.example.ts.net"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		in, want, err string
	}{
		{"/tmp/x.sock", "/tmp/x.sock", ""},
		{"./x.sock", "./x.sock", ""},
		{"https://box.example.ts.net", "https://box.example.ts.net", ""},
		{"http://100.64.0.1:4483", "http://100.64.0.1:4483", ""},
		{"hivetest-box", "https://box.example.ts.net", ""},
		{"HIVETEST-BOX", "https://box.example.ts.net", ""},
		{"nope-not-a-host-zz", "", "no host by that name"},
		// A name that resolves (as a MagicDNS short name does) is a host.
		{"shortname", "https://shortname", ""},
		{"box.example.ts.net", "https://box.example.ts.net", ""},
		{"fd7a:115c:a1e0::1", "https://[fd7a:115c:a1e0::1]", ""},
		{"root@hivetest-box", "", "account"},
		{"https://root@box.example.ts.net", "", "account"},
	} {
		c0, err := resolve(c.in)
		switch {
		case c.err != "":
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("resolve(%q) = %v, want an error mentioning %q", c.in, err, c.err)
			}
		case err != nil:
			t.Errorf("resolve(%q): %v", c.in, err)
		case c0.String() != c.want:
			t.Errorf("resolve(%q) = %s, want %s", c.in, c0, c.want)
		}
	}
}

// The CLI against a server that listens on the network: commands work by
// URL and by a saved label, nothing is started locally, and the CLI's own
// directory means nothing on the other machine.
func TestRemoteCLI(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hive")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	dir, err := os.MkdirTemp("/tmp", "hive-remote.")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	// The "remote" server: its own socket, state, settings and home.
	serverHome := filepath.Join(dir, "server-home")
	os.MkdirAll(serverHome, 0o700)
	srvDir := filepath.Join(dir, "srv")
	srv := exec.Command(bin, "server", "run", "--socket", filepath.Join(srvDir, "s.sock"), "--state-dir", filepath.Join(srvDir, "state"),
		"--extra-listen", addr, "--allow-anonymous")
	srv.Env = append(testEnv(srvDir), "HOME="+serverHome)
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { srv.Process.Signal(os.Interrupt); srv.Wait() }()

	// The client: a socket nobody listens on, so a local auto-start would
	// show up as that socket appearing.
	cliDir := filepath.Join(dir, "cli")
	env := testEnv(cliDir)
	hive := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = env
		cmd.Dir = dir // not the server's home
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		} else if err != nil {
			t.Fatalf("hive %v: %v", args, err)
		}
		return string(out), 0
	}
	must := func(args ...string) string {
		t.Helper()
		out, code := hive(args...)
		if code != 0 {
			t.Fatalf("hive %v: exit %d\n%s", args, code, out)
		}
		return out
	}
	url := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, code := hive("-S", url, "whoami"); code == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("remote server never answered")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if out := must("-S", url, "whoami"); !strings.Contains(out, "anonymous via plain") {
		t.Fatalf("whoami = %q", out)
	}
	must("hosts", "add", "hivetest-box", url)
	if out := must("hosts", "ls"); !strings.Contains(out, "hivetest-box") || !strings.Contains(out, url) || !strings.Contains(out, "on-demand") {
		t.Fatalf("hosts ls = %q", out)
	}
	// --always sticks when the host is saved again without a choice.
	must("hosts", "add", "--always", "hivetest-box", url)
	must("hosts", "add", "hivetest-box", url)
	if out := must("hosts", "ls"); !strings.Contains(out, "always") {
		t.Fatalf("hosts ls after --always and a re-save = %q", out)
	}
	must("hosts", "add", "hivetest-box", url, "--on-demand")
	// A flag anywhere; both at once is a mistake.
	must("hosts", "add", "hivetest-box", "--always", url)
	if out := must("hosts", "ls"); !strings.Contains(out, "always") {
		t.Fatalf("a flag between label and URL: %q", out)
	}
	if out, code := hive("hosts", "add", "hivetest-box", url, "--always", "--on-demand"); code != 2 || !strings.Contains(out, "one of") {
		t.Fatalf("both flags: exit %d, %q", code, out)
	}
	must("hosts", "add", "hivetest-box", url, "--on-demand")
	must("-S", "hivetest-box", "new", "far", "--", "sh", "-c", "pwd -P; exec cat")
	if out := must("-S", "hivetest-box", "ls"); !strings.Contains(out, "far") {
		t.Fatalf("ls on the remote = %q", out)
	}
	home, _ := filepath.EvalSymlinks(serverHome)
	var screen string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		screen = must("-S", "hivetest-box", "-s", "far", "capture", "--trim")
		if strings.Contains(screen, home) {
			break
		}
	}
	if !strings.Contains(screen, home) {
		t.Fatalf("the remote block started in %q, want the server's home %s (not the CLI's %s)", screen, home, dir)
	}
	if out, code := hive("-S", "hivetest-box", "new", "rel", "--cwd", "src", "--", "cat"); code != 2 || !strings.Contains(out, "absolute") {
		t.Fatalf("a relative --cwd on a remote: exit %d, %q", code, out)
	}
	if out, code := hive("-S", "hivetest-box", "config", "set", "font-size", "14"); code != 2 || !strings.Contains(out, "this machine's settings") {
		t.Fatalf("config set with a remote -S: exit %d, %q", code, out)
	}
	if out := must("-S", "hivetest-box", "client", "ls"); !strings.Contains(out, "PRINCIPAL") {
		t.Fatalf("client ls = %q", out)
	}
	if _, err := os.Stat(filepath.Join(cliDir, "s.sock")); err == nil {
		t.Fatal("a remote target started a local server")
	}

	// Nothing there: unreachable, and still nothing started here.
	if out, code := hive("-S", "http://127.0.0.1:1", "ls"); code == 0 || !strings.Contains(out, "unreachable") {
		t.Fatalf("an unreachable remote: exit %d, %q", code, out)
	}
	if out, code := hive("-S", "root@hivetest-box", "ls"); code == 0 || !strings.Contains(out, "account") {
		t.Fatalf("user@host: exit %d, %q", code, out)
	}
	if out, code := hive("-S", url, "server", "start"); code == 0 || !strings.Contains(out, "this machine") {
		t.Fatalf("server start on a remote: exit %d, %q", code, out)
	}
	// Refused without touching the network: a bare host that doesn't exist
	// answers at once, and no socket file named after it appears.
	start := time.Now()
	if out, code := hive("-S", "nothing.invalid", "server", "run"); code != 2 || time.Since(start) > 3*time.Second {
		t.Fatalf("server run with a remote -S: exit %d after %v, %q", code, time.Since(start), out)
	}
	if _, err := os.Stat(filepath.Join(dir, "nothing.invalid")); err == nil {
		t.Fatal("server run made a socket named after the host")
	}
	if _, err := os.Stat(filepath.Join(cliDir, "s.sock")); err == nil {
		t.Fatal("a remote target started a local server")
	}
	must("hosts", "rm", "hivetest-box")
	if out, code := hive("-S", "hivetest-box", "ls"); code == 0 || !strings.Contains(out, "no host") {
		t.Fatalf("a removed host: exit %d, %q", code, out)
	}
}

// HIVE_SERVER is a socket path, whatever it looks like: the server sets it
// in every pane, and hooks and the app read it as one.
func TestHiveServerIsAlwaysAPath(t *testing.T) {
	for _, v := range []string{"s.sock", "box.example.ts.net", "https://box.example.ts.net"} {
		c, err := clientFor(globals{server: v})
		if err != nil || c.Remote() || c.Socket != v {
			t.Errorf("HIVE_SERVER=%s: %v, %v", v, c, err)
		}
	}
	c, err := clientFor(globals{server: "https://box.example.ts.net", explicitServer: true})
	if err != nil || !c.Remote() {
		t.Errorf("-S https://…: %v, %v", c, err)
	}
}
