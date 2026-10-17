// Command hive is the hive server and its CLI, in one binary. Its commands,
// flags, and exit codes are stable so that scripts keep working.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/paths"
)

// Exit statuses.
const (
	exitOK          = 0
	exitFail        = 1
	exitUsage       = 2
	exitTarget      = 3
	exitUnreachable = 4
	exitInterrupted = 130
)

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error { return usageError{fmt.Sprintf(format, args...)} }

type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

// globals are the target flags every command accepts.
type globals struct {
	session, window, block, clientT, server string
	json                                    bool
	explicitServer                          bool // -S was passed
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	g, rest, err := parseGlobals(argv)
	if err == nil && len(rest) == 0 {
		rest = []string{"help"}
	}
	if err == nil {
		err = dispatch(g, rest)
	}
	if err == nil {
		return exitOK
	}
	var ee exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	var ue usageError
	var ce *client.Error
	var un *client.UnreachableError
	switch {
	case errors.As(err, &ue):
		return exitUsage
	case errors.As(err, &un):
		return exitUnreachable
	case errors.As(err, &ce) && (ce.Code == "not_found" || ce.Code == "ambiguous"):
		return exitTarget
	case errors.Is(err, context.Canceled):
		return exitInterrupted
	}
	return exitFail
}

// parseGlobals pulls the target flags out of argv, wherever they appear
// before a "--".
func parseGlobals(argv []string) (globals, []string, error) {
	g := globals{server: os.Getenv("HIVE_SERVER"), clientT: os.Getenv("HIVE_CLIENT")}
	var rest []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			rest = append(rest, argv[i:]...)
			break
		}
		name, val, hasVal := strings.Cut(a, "=")
		var dst *string
		switch name {
		case "-s", "--session":
			dst = &g.session
		case "-w", "--window":
			dst = &g.window
		case "-b", "--block":
			dst = &g.block
		case "-C", "--client":
			dst = &g.clientT
		case "-S", "--server":
			dst = &g.server
			g.explicitServer = true
		case "--json":
			g.json = true
			continue
		default:
			rest = append(rest, a)
			continue
		}
		if !hasVal {
			if i+1 >= len(argv) {
				return g, nil, usagef("%s needs a value", name)
			}
			i++
			val = argv[i]
		}
		*dst = val
	}
	return g, rest, nil
}

func socketFor(g globals) string {
	if g.server != "" {
		return g.server
	}
	return paths.Socket()
}

// connect returns a client, starting a server first if none is running.
func connect(g globals) (*client.Client, error) {
	sock := socketFor(g)
	c := client.New(sock)
	if c.Ping() {
		return c, nil
	}
	// Any command starts a personal server, unless a server was
	// named explicitly.
	if g.explicitServer || !strings.HasPrefix(sock, "/") {
		return nil, &client.UnreachableError{Err: fmt.Errorf("no server on %s", sock)}
	}
	if err := startDaemon(sock); err != nil {
		return nil, &client.UnreachableError{Err: err}
	}
	return c, nil
}

func startDaemon(sock string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	state := paths.State()
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(state, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "server", "run", "--socket", sock)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Dir = "/"
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	c := client.New(sock)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c.Ping() {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return fmt.Errorf("server did not start; see %s", filepath.Join(state, "server.log"))
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	if fi, err := os.Stdout.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}

func printRaw(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		_, err := os.Stdout.Write(append(raw, '\n'))
		return err
	}
	return printJSON(os.Stdout, v)
}

func short(id string) string {
	if len(id) <= 6 {
		return id
	}
	return "…" + id[len(id)-6:]
}
