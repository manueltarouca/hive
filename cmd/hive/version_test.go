package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A release build is stamped with its version at link time, and both the CLI
// and the server report it.
func TestVersionIsStampedAtBuild(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hive")
	build := exec.Command("go", "build", "-o", bin,
		"-ldflags", "-X github.com/manueltarouca/hive/internal/server.Version=9.8.7-rc.1", ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "hive 9.8.7-rc.1" {
		t.Fatalf("hive version = %q", got)
	}

	dir, err := os.MkdirTemp("/tmp", "hive-ver.")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	call := exec.Command(bin, "api", "call", "server.version")
	call.Env = testEnv(dir)
	out, err = call.Output()
	stop := exec.Command(bin, "server", "stop")
	stop.Env = call.Env
	defer stop.Run()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"9.8.7-rc.1"`) {
		t.Fatalf("server.version = %s", out)
	}
	ownSettings(t, bin, call.Env, dir)
}

// ownSettings fails unless the test's server reads a settings file in the
// test's own dir: never the user's.
func ownSettings(t *testing.T, bin string, env []string, dir string) {
	t.Helper()
	get := exec.Command(bin, "api", "call", "config.get")
	get.Env = env
	out, err := get.Output()
	if err != nil {
		t.Fatalf("config.get: %v", err)
	}
	var v struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(out, &v); err != nil || !strings.HasPrefix(v.Path, dir+"/") {
		t.Fatalf("the test server's settings file is %q, not in %s", v.Path, dir)
	}
}
