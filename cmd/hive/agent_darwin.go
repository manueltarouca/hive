package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/paths"
)

// macOS stops what an app started when that app closes, unless the app may
// run in the background: a server `hive` started in a terminal died with that
// terminal. Hive.app registers the server as its background item (launchd
// job xyz.matm.hive.server, in Login Items & Extensions); `hive` asks launchd
// to start that job rather than starting the server itself.
const agentLabel = "xyz.matm.hive.server"

func agentJob() string {
	label := agentLabel
	if l := os.Getenv("HIVE_AGENT_LABEL"); l != "" {
		label = l // the throwaway test bundle's job
	}
	return fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
}

// agentServes says whether this start is the background item's: the
// machine's own server, with no options only `hive server start` can pass.
func agentServes(sock string, extra, env []string) bool {
	if extra != nil || env != nil {
		return false
	}
	if os.Getenv("HIVE_AGENT_LABEL") != "" {
		return true
	}
	if sock != paths.Socket() || sock != paths.LaunchdSocket() {
		return false // another TMPDIR than launchd's: not the item's socket
	}
	for _, k := range []string{"HIVE_SERVER", "HIVE_STATE_DIR", "HIVE_CONFIG", "HIVE_SCRIPTS", "XDG_RUNTIME_DIR", "XDG_STATE_HOME", "XDG_CONFIG_HOME"} {
		if os.Getenv(k) != "" {
			return false // another server than the item's
		}
	}
	return true
}

// startAgent asks launchd to start the background item. False: no item (Hive
// not opened yet on this Mac), or it didn't answer.
func startAgent(sock string) (started, exists bool) {
	if err := exec.Command("/bin/launchctl", "kickstart", agentJob()).Run(); err != nil {
		return false, false
	}
	c := client.New(sock)
	for i, end := 0, time.Now().Add(10*time.Second); time.Now().Before(end); i++ {
		if c.Ping() {
			return true, true
		}
		if i%10 == 9 {
			out, _ := exec.Command("/bin/launchctl", "print", agentJob()).Output()
			switch {
			case strings.Contains(string(out), "spawn failed"):
				// Rebuilt or updated since Hive registered it: macOS won't
				// start it until Hive is opened again.
				return false, true
			case !strings.Contains(string(out), "\tpid = "):
				// A server just stopped (`hive server stop` and at once a
				// command) was still exiting when asked: ask again.
				_ = exec.Command("/bin/launchctl", "kickstart", agentJob()).Run()
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false, true
}

// agentHint: a server started here stops when this terminal app closes.
func agentHint(exists bool) string {
	if exists {
		return "hive: Hive's background server didn't start (open Hive once after an update), so this terminal started one; it stops when you close this terminal app"
	}
	return "hive: started the server from this terminal; it stops when you close this terminal app. Open Hive once (and keep Hive on in Login Items & Extensions), and it runs in the background"
}

// prepareAgent runs first in the background item's server: its output goes
// to server.log, panes look like a terminal's, and commands find what the
// user's login shell finds (launchd's PATH is the system's only).
func prepareAgent(state string) error {
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(state, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_ = syscall.Dup2(int(f.Fd()), 1)
	_ = syscall.Dup2(int(f.Fd()), 2)
	f.Close()
	os.Setenv("XPC_SERVICE_NAME", "0")
	// Claude Code's computer use checks the grants of the app panes say they're in.
	os.Setenv("__CFBundleIdentifier", "xyz.matm.hive")
	if p, err := loginPath(); err == nil && p != "" {
		os.Setenv("PATH", p)
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "time=%s level=WARN msg=\"login shell PATH\" err=%q\n", time.Now().UTC().Format(time.RFC3339Nano), err)
	}
	return nil
}

// loginPath is PATH as the user's interactive login shell sets it. Read with
// printenv, the same in every shell (fish joins "$PATH" with spaces), and
// bounded: dotfiles may prompt, or leave a helper holding the output open.
func loginPath() (string, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	return shellPath(shell, 5*time.Second)
}

const pathMark = "\x1ehive-path\x1e"

func shellPath(shell string, limit time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", "/usr/bin/printenv HIVE_PATH_MARK; /usr/bin/printenv PATH; /usr/bin/printenv HIVE_PATH_MARK")
	cmd.Dir = os.Getenv("HOME")
	cmd.Env = append(os.Environ(), "HIVE_PATH_MARK="+pathMark)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 500 * time.Millisecond
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	parts := strings.Split(out.String(), pathMark+"\n")
	if len(parts) < 3 {
		if err == nil {
			err = errors.New("no PATH in the shell's output")
		}
		return "", err
	}
	return strings.TrimSuffix(parts[len(parts)-2], "\n"), nil
}

// agentServing: the server is up; from here a fatal error dies by signal
// (not exit 2), so launchd sees a crash and starts it again. Before this, a
// failure stays down (exit 0) rather than restart forever.
func agentServing() { debug.SetTraceback("crash") }
