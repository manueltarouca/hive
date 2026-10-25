package main

/*
#include <notify.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/paths"
)

// startViaApp asks Hive.app to start the server. macOS checks what a pane's
// programs may do (local network, screen recording, accessibility) against
// the app responsible for them: a server Hive.app starts is Hive's, one a
// shell starts is an anonymous binary, denied all of it without a prompt.
// False: not asked, or no answer in time.
func startViaApp(sock string) bool {
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	home, _ := os.UserHomeDir()
	app := appFor(exe, sock, paths.Socket(), os.Getenv, func(p string) bool {
		_, err := os.Stat(filepath.Join(p, "Contents", "Resources", "hive"))
		return err == nil
	}, home)
	if app == "" {
		return false
	}
	c := client.New(sock)
	// Running: it starts the server when told. Not running: opening it does,
	// hidden. (Opening a running app would show its window.)
	name := C.CString(startServerNote(sock))
	C.notify_post(name)
	C.free(unsafe.Pointer(name))
	if waitPing(c, 1500*time.Millisecond) {
		return true
	}
	if exec.Command("/usr/bin/open", "-g", "-j", "--env", "HIVE_START_HIDDEN=1", app).Run() == nil && waitPing(c, 10*time.Second) {
		return true
	}
	fmt.Fprintf(os.Stderr, "hive: %s didn't start the server; starting it here, so macOS may deny its panes the local network and screen recording\n", filepath.Base(app))
	return false
}

// startServerNote is the notification Hive.app starts the server on, for its socket.
func startServerNote(sock string) string { return "xyz.matm.hive.start-server:" + sock }

// appFor is the Hive.app to ask to start the server on sock, or "": the one
// this binary is inside, else one installed in /Applications or ~/Applications.
// Never for a server other than the app's own (tests, a second state dir).
func appFor(exe, sock, defaultSock string, getenv func(string) string, isApp func(string) bool, home string) string {
	if sock != defaultSock {
		return ""
	}
	for _, k := range []string{"HIVE_SERVER", "HIVE_STATE_DIR", "HIVE_CONFIG", "HIVE_SCRIPTS"} {
		if getenv(k) != "" {
			return ""
		}
	}
	if a := appOf(exe); a != "" {
		return a
	}
	for _, a := range []string{"/Applications/Hive.app", filepath.Join(home, "Applications", "Hive.app")} {
		if isApp(a) {
			return a
		}
	}
	return ""
}

// appOf is the .app that holds exe as its server binary, or "".
func appOf(exe string) string {
	res := filepath.Dir(exe)
	app := filepath.Dir(filepath.Dir(res))
	if filepath.Base(res) != "Resources" || filepath.Base(filepath.Dir(res)) != "Contents" || !strings.HasSuffix(app, ".app") {
		return ""
	}
	return app
}

func waitPing(c *client.Client, d time.Duration) bool {
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if c.Ping() {
			return true
		}
	}
	return false
}
