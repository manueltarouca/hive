package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/manueltarouca/hive/internal/agents"
	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/paths"
	"github.com/manueltarouca/hive/internal/server"
)

const helpText = `hive — a terminal multiplexer for agent-heavy work (the reference app-compatible CLI)

Sessions
  ls                         list sessions
  new [LABEL] [--cwd DIR] [-- CMD...]
                             create a session
  session inspect            show the current session
  close [--session|--window] close the block, window, or session

Blocks
  run   [opts] [-- CMD...]   open a terminal in a new window
  split [opts] [-- CMD...]   open a terminal beside the current one
        opts: --split right|left|above|below  --ratio PCT  --cwd DIR
              --label TEXT  --focus=false  --keep-open  --wait
  block inspect              show the current block
  label TEXT                 label the block (--session/--window: those)
  zoom [--on|--off]          zoom the block
  focus [left|right|up|down] focus the block, or its neighbor

Input and output
  send TEXT | -              type text (stdin with - or no TEXT)
  send-key KEY...            press keys: enter, ctrl+c, up, ...
  capture [--trim] [--unwrap] [--format text|vt]
  wait [--for DUR] [TARGET]  wait for a block's program to exit
  events [--all] [--since N] stream events

Agents (hive)
  agents                     list blocks running coding agents
  agent install claude       add hive's hooks to Claude Code
  agent uninstall claude     remove them
  agent hook HARNESS         called by an agent's hooks

API
  api list [-q]              every server method
  api describe METHOD        a method's arguments and result
  api call METHOD [JSON|@FILE|-]

Server
  server run [--fresh]       run in the foreground
  server start | stop | status
  version

Targets: -s/--session -w/--window -b/--block -C/--client -S/--server, --json.
A target is an ID, a label, a position from 1, or the end of an ID.
Inside a hive terminal, HIVE_SESSION and HIVE_BLOCK are the defaults.
`

func dispatch(g globals, args []string) error {
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(helpText)
		return nil
	case "version", "--version":
		fmt.Println("hive", server.Version)
		return nil
	case "server":
		return cmdServer(g, rest)
	case "ls":
		return cmdLs(g, rest)
	case "new":
		return cmdNew(g, rest)
	case "run":
		return cmdRunSplit(g, rest, false)
	case "split":
		return cmdRunSplit(g, rest, true)
	case "send":
		return cmdSend(g, rest)
	case "send-key":
		return cmdSendKey(g, rest)
	case "capture":
		return cmdCapture(g, rest)
	case "wait":
		return cmdWait(g, rest)
	case "zoom":
		return cmdZoom(g, rest)
	case "focus":
		return cmdFocus(g, rest)
	case "close", "kill":
		return cmdClose(g, rest)
	case "label":
		return cmdLabel(g, rest)
	case "events":
		return cmdEvents(g, rest)
	case "api":
		return cmdAPI(g, rest)
	case "session":
		if len(rest) > 0 && rest[0] == "inspect" {
			return cmdInspect(g, "session")
		}
		return usagef("usage: hive session inspect")
	case "block":
		if len(rest) > 0 && rest[0] == "inspect" {
			return cmdInspect(g, "block")
		}
		return usagef("usage: hive block inspect")
	case "agents":
		return cmdAgents(g)
	case "agent":
		return cmdAgent(g, rest)
	}
	return usagef("unknown command %q; run hive help", cmd)
}

// ---- context and targets ----

type cx struct {
	g   globals
	c   *client.Client
	ctx context.Context
}

func open(g globals) (*cx, error) {
	c, err := connect(g)
	if err != nil {
		return nil, err
	}
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return &cx{g: g, c: c, ctx: ctx}, nil
}

func (x *cx) call(method string, params any, out any) error {
	return x.c.Call(x.ctx, method, params, out)
}

func (x *cx) resolve(kind, target, sessionID, windowID string) (string, string, error) {
	var out struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
	}
	err := x.call("target.resolve", map[string]any{"kind": kind, "target": target, "session_id": sessionID, "window_id": windowID}, &out)
	return out.ID, out.SessionID, err
}

// sessionID finds the session a command acts on.
func (x *cx) sessionID() (string, error) {
	if x.g.session != "" {
		id, _, err := x.resolve("session", x.g.session, "", "")
		return id, err
	}
	if x.g.block != "" {
		_, sid, err := x.resolve("block", x.g.block, "", "")
		return sid, err
	}
	if s := os.Getenv("HIVE_SESSION"); s != "" {
		return s, nil
	}
	return "", usagef("no session: pass -s, or run inside a hive terminal")
}

type viewWindow struct {
	ID    string `json:"window_id"`
	Label string `json:"label"`
	Focus string `json:"focused_block_id"`
}

type view struct {
	SessionID string                     `json:"session_id"`
	Label     string                     `json:"label"`
	Active    string                     `json:"active_window_id"`
	Windows   []viewWindow               `json:"windows"`
	Blocks    map[string]json.RawMessage `json:"blocks"`
}

func (x *cx) view(sid string) (*view, error) {
	var v view
	err := x.call("session.view", map[string]any{"session_id": sid}, &v)
	return &v, err
}

// windowID finds the window a command acts on: -w, or the active one.
func (x *cx) windowID(sid string) (string, error) {
	if x.g.window != "" {
		id, _, err := x.resolve("window", x.g.window, sid, "")
		return id, err
	}
	v, err := x.view(sid)
	if err != nil {
		return "", err
	}
	return v.Active, nil
}

// blockID finds the block a command acts on: -b; else the focused block of
// -s/-w; else the terminal the command runs in.
func (x *cx) blockID() (sid, bid string, err error) {
	if x.g.block != "" {
		var scope string
		if x.g.session != "" {
			if scope, err = x.sessionID(); err != nil {
				return "", "", err
			}
		}
		bid, sid, err = x.resolve("block", x.g.block, scope, "")
		return sid, bid, err
	}
	if x.g.session == "" && x.g.window == "" {
		if b := os.Getenv("HIVE_BLOCK"); b != "" {
			return os.Getenv("HIVE_SESSION"), b, nil
		}
	}
	if sid, err = x.sessionID(); err != nil {
		return "", "", err
	}
	v, err := x.view(sid)
	if err != nil {
		return "", "", err
	}
	wid := v.Active
	if x.g.window != "" {
		if wid, _, err = x.resolve("window", x.g.window, sid, ""); err != nil {
			return "", "", err
		}
	}
	for _, w := range v.Windows {
		if w.ID == wid && w.Focus != "" {
			return sid, w.Focus, nil
		}
	}
	return "", "", usagef("no block to act on in session %s", short(sid))
}

func flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return usagef("%s: %v", fs.Name(), err)
	}
	return nil
}

// ---- server ----

func cmdServer(g globals, args []string) error {
	if len(args) == 0 {
		return usagef("usage: hive server run|start|stop|status")
	}
	switch args[0] {
	case "run":
		fs := flags("server run")
		fresh := fs.Bool("fresh", false, "")
		sock := fs.String("socket", socketFor(g), "")
		state := fs.String("state-dir", paths.State(), "")
		if err := parse(fs, args[1:]); err != nil {
			return err
		}
		exe, _ := os.Executable()
		srv, err := server.New(server.Config{Socket: *sock, StateDir: *state, Fresh: *fresh, HivePath: exe,
			Logger: newLogger()})
		if err != nil {
			return err
		}
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
		go func() { <-sig; srv.Shutdown() }()
		return srv.Serve()
	case "start":
		_, err := connect(g)
		if err == nil {
			fmt.Println("server running on", socketFor(g))
		}
		return err
	case "stop":
		c := client.New(socketFor(g))
		if !c.Ping() {
			fmt.Println("no server running")
			return nil
		}
		if err := c.Call(context.Background(), "server.shutdown", nil, nil); err != nil {
			return err
		}
		deadline := time.Now().Add(10 * time.Second)
		for c.Ping() && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		fmt.Println("server stopped; sessions will be restored at the next start")
		return nil
	case "status":
		c := client.New(socketFor(g))
		if !c.Ping() {
			fmt.Println("no server running on", socketFor(g))
			return exitError{exitUnreachable}
		}
		raw, err := c.CallRaw(context.Background(), "server.version", nil)
		if err != nil {
			return err
		}
		return printRaw(raw)
	}
	return usagef("unknown server command %q", args[0])
}

// ---- sessions ----

func cmdLs(g globals, args []string) error {
	x, err := open(g)
	if err != nil {
		return err
	}
	var out struct {
		Sessions []struct {
			ID      string    `json:"session_id"`
			Label   string    `json:"label"`
			Windows int       `json:"windows"`
			Blocks  int       `json:"blocks"`
			Agents  int       `json:"agents"`
			Created time.Time `json:"created_at"`
		} `json:"sessions"`
	}
	raw, err := x.c.CallRaw(x.ctx, "session.list", nil)
	if err != nil {
		return err
	}
	if g.json {
		return printRaw(raw)
	}
	_ = json.Unmarshal(raw, &out)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "LABEL\tWINDOWS\tBLOCKS\tAGENTS\tAGE\tID")
	cur := os.Getenv("HIVE_SESSION")
	for _, s := range out.Sessions {
		label := s.Label
		if label == "" {
			label = "-"
		}
		if s.ID == cur {
			label += " *"
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%s\t%s\n", label, s.Windows, s.Blocks, s.Agents, age(s.Created), s.ID[len(s.ID)-6:])
	}
	return tw.Flush()
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func splitCommand(args []string) (flagArgs, command []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

func cmdNew(g globals, args []string) error {
	flagArgs, command := splitCommand(args)
	fs := flags("new")
	cwd := fs.String("cwd", "", "")
	if err := parse(fs, reorder(flagArgs)); err != nil {
		return err
	}
	label := strings.Join(fs.Args(), " ")
	x, err := open(g)
	if err != nil {
		return err
	}
	if *cwd == "" {
		*cwd, _ = os.Getwd()
	}
	raw, err := x.c.CallRaw(x.ctx, "session.create", map[string]any{"label": label, "cwd": absPath(*cwd), "command": command})
	if err != nil {
		return err
	}
	if g.json {
		return printRaw(raw)
	}
	var out struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(raw, &out)
	fmt.Println(out.SessionID)
	return nil
}

func absPath(p string) string {
	if p == "" || strings.HasPrefix(p, "~") {
		return p
	}
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return a
}

// reorder moves flags ahead of positional arguments, so "new work --cwd x"
// works like the reference app's.
func reorder(args []string) []string {
	var fl, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			fl = append(fl, a)
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && !boolFlag(a) {
				fl = append(fl, args[i+1])
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(fl, pos...)
}

func boolFlag(a string) bool {
	switch strings.TrimLeft(a, "-") {
	case "keep-open", "wait", "trim", "unwrap", "on", "off", "all", "q", "dry-run", "fresh", "session", "window":
		return true
	}
	return false
}

func cmdRunSplit(g globals, args []string, split bool) error {
	flagArgs, command := splitCommand(args)
	fs := flags("run")
	side := fs.String("split", "right", "")
	ratio := fs.Float64("ratio", 50, "")
	cwd := fs.String("cwd", "", "")
	label := fs.String("label", "", "")
	focus := fs.Bool("focus", true, "")
	keep := fs.Bool("keep-open", false, "")
	wait := fs.Bool("wait", false, "")
	if err := parse(fs, reorder(flagArgs)); err != nil {
		return err
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	if *cwd == "" {
		*cwd, _ = os.Getwd()
	}
	layout := map[string]any{"command": command, "cwd": absPath(*cwd), "label": *label, "keep_open": *keep || *wait}
	var raw json.RawMessage
	if split {
		sid, bid, err := x.blockID()
		if err != nil {
			return err
		}
		dir, sideArg := "horizontal", "after"
		switch *side {
		case "right":
		case "left":
			sideArg = "before"
		case "below", "down":
			dir = "vertical"
		case "above", "up":
			dir, sideArg = "vertical", "before"
		default:
			return usagef("--split must be right, left, above, or below")
		}
		raw, err = x.c.CallRaw(x.ctx, "session.new_split", map[string]any{
			"session_id": sid, "block_id": bid, "direction": dir, "side": sideArg,
			"ratio": *ratio / 100, "focus": *focus, "layout": layout,
		})
		if err != nil {
			return err
		}
	} else {
		sid, err := x.sessionID()
		if err != nil {
			return err
		}
		raw, err = x.c.CallRaw(x.ctx, "session.new_window", map[string]any{
			"session_id": sid, "layout": layout, "focus": *focus, "label": *label,
		})
		if err != nil {
			return err
		}
	}
	var out struct {
		BlockIDs []string `json:"block_ids"`
	}
	_ = json.Unmarshal(raw, &out)
	if *wait && len(out.BlockIDs) > 0 {
		code, err := waitBlock(x, out.BlockIDs[0], 0)
		if err != nil {
			return err
		}
		if code != 0 {
			return exitError{code}
		}
		return nil
	}
	if g.json {
		return printRaw(raw)
	}
	if len(out.BlockIDs) > 0 {
		fmt.Println(out.BlockIDs[0])
	}
	return nil
}

// ---- input and output ----

func (x *cx) terminal(method string, bid string, args any, out any) error {
	return x.call("hive.terminal."+method, map[string]any{"block_id": bid, "args": args}, out)
}

func cmdSend(g globals, args []string) error {
	x, err := open(g)
	if err != nil {
		return err
	}
	_, bid, err := x.blockID()
	if err != nil {
		return err
	}
	var data string
	if len(args) == 0 || (len(args) == 1 && args[0] == "-") {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		data = string(b)
	} else {
		data = strings.Join(args, " ")
	}
	return x.terminal("write", bid, map[string]any{"data": data}, nil)
}

func cmdSendKey(g globals, args []string) error {
	if len(args) == 0 {
		return usagef("usage: hive send-key KEY...")
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	_, bid, err := x.blockID()
	if err != nil {
		return err
	}
	return x.terminal("send_key", bid, map[string]any{"keys": args}, nil)
}

func cmdCapture(g globals, args []string) error {
	fs := flags("capture")
	trim := fs.Bool("trim", false, "")
	unwrap := fs.Bool("unwrap", false, "")
	format := fs.String("format", "text", "")
	if err := parse(fs, args); err != nil {
		return err
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	_, bid, err := x.blockID()
	if err != nil {
		return err
	}
	var out struct {
		Content string `json:"content"`
	}
	if err := x.terminal("format", bid, map[string]any{"format": *format, "trim": *trim, "unwrap": *unwrap}, &out); err != nil {
		return err
	}
	if g.json {
		return printJSON(os.Stdout, out)
	}
	fmt.Print(out.Content)
	if !strings.HasSuffix(out.Content, "\n") {
		fmt.Println()
	}
	return nil
}

// waitBlock waits for a block's program to exit and returns its status.
func waitBlock(x *cx, bid string, limit time.Duration) (int, error) {
	start := time.Now()
	for {
		step := 50.0
		if limit > 0 {
			left := limit - time.Since(start)
			if left <= 0 {
				return 0, exitError{exitFail}
			}
			step = min(step, left.Seconds())
		}
		var out struct {
			Exited   bool `json:"exited"`
			ExitCode int  `json:"exit_code"`
		}
		if err := x.terminal("wait", bid, map[string]any{"timeout": step}, &out); err != nil {
			var ce *client.Error
			if errors.As(err, &ce) && ce.Code == "not_found" {
				return 0, nil // closed already
			}
			return 0, err
		}
		if out.Exited {
			return out.ExitCode, nil
		}
		if x.ctx.Err() != nil {
			return 0, x.ctx.Err()
		}
	}
}

func cmdWait(g globals, args []string) error {
	fs := flags("wait")
	limit := fs.Duration("for", 0, "")
	if err := parse(fs, reorder(args)); err != nil {
		return err
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	if t := fs.Arg(0); t != "" {
		switch {
		case strings.HasPrefix(t, "session:"), strings.HasPrefix(t, "window:"):
			return waitGone(x, t, *limit)
		}
		bid, _, err := x.resolve("block", t, "", "")
		if err != nil {
			return err
		}
		code, err := waitBlock(x, bid, *limit)
		if err != nil {
			return err
		}
		if code != 0 {
			return exitError{code}
		}
		return nil
	}
	_, bid, err := x.blockID()
	if err != nil {
		return err
	}
	code, err := waitBlock(x, bid, *limit)
	if err == nil && code != 0 {
		return exitError{code}
	}
	return err
}

// waitGone waits for a session or window to close.
func waitGone(x *cx, target string, limit time.Duration) error {
	start := time.Now()
	for {
		var gone bool
		if strings.HasPrefix(target, "session:") {
			_, err := x.view(target)
			var ce *client.Error
			gone = errors.As(err, &ce) && ce.Code == "not_found"
		} else {
			var out struct {
				Sessions []struct {
					ID string `json:"session_id"`
				} `json:"sessions"`
			}
			if err := x.call("session.list", nil, &out); err != nil {
				return err
			}
			gone = true
			for _, s := range out.Sessions {
				v, err := x.view(s.ID)
				if err != nil {
					continue
				}
				for _, w := range v.Windows {
					if w.ID == target {
						gone = false
					}
				}
			}
		}
		if gone {
			return nil
		}
		if limit > 0 && time.Since(start) > limit {
			return exitError{exitFail}
		}
		select {
		case <-x.ctx.Done():
			return x.ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// ---- layout ----

func cmdZoom(g globals, args []string) error {
	fs := flags("zoom")
	on := fs.Bool("on", false, "")
	off := fs.Bool("off", false, "")
	if err := parse(fs, args); err != nil {
		return err
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	sid, bid, err := x.blockID()
	if err != nil {
		return err
	}
	p := map[string]any{"session_id": sid, "block_id": bid}
	if *on {
		p["zoom"] = true
	} else if *off {
		p["zoom"] = false
	}
	return x.call("session.zoom_block", p, nil)
}

func cmdFocus(g globals, args []string) error {
	x, err := open(g)
	if err != nil {
		return err
	}
	sid, bid, err := x.blockID()
	if err != nil {
		return err
	}
	if len(args) > 0 {
		var out struct {
			BlockID string `json:"block_id"`
		}
		if err := x.call("session.focus_direction", map[string]any{"session_id": sid, "block_id": bid, "direction": args[0]}, &out); err != nil {
			return err
		}
		if out.BlockID == "" {
			return exitError{exitTarget}
		}
		fmt.Println(out.BlockID)
		return nil
	}
	return x.call("session.focus_block", map[string]any{"session_id": sid, "block_id": bid}, nil)
}

func cmdClose(g globals, args []string) error {
	fs := flags("close")
	wholeSession := fs.Bool("session", false, "")
	wholeWindow := fs.Bool("window", false, "")
	if err := parse(fs, args); err != nil {
		return err
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	switch {
	case *wholeSession:
		sid, err := x.sessionID()
		if err != nil {
			return err
		}
		return x.call("session.close", map[string]any{"session_id": sid}, nil)
	case *wholeWindow:
		sid, err := x.sessionID()
		if err != nil {
			return err
		}
		wid, err := x.windowID(sid)
		if err != nil {
			return err
		}
		return x.call("session.close_window", map[string]any{"session_id": sid, "window_id": wid}, nil)
	}
	sid, bid, err := x.blockID()
	if err != nil {
		return err
	}
	return x.call("block.close", map[string]any{"session_id": sid, "block_id": bid}, nil)
}

func cmdLabel(g globals, args []string) error {
	fs := flags("label")
	ofSession := fs.Bool("session", false, "")
	ofWindow := fs.Bool("window", false, "")
	if err := parse(fs, reorder(args)); err != nil {
		return err
	}
	text := strings.Join(fs.Args(), " ")
	if text == "" {
		return usagef("usage: hive label [--session|--window] TEXT")
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	switch {
	case *ofSession:
		sid, err := x.sessionID()
		if err != nil {
			return err
		}
		return x.call("session.set_label", map[string]any{"session_id": sid, "label": text}, nil)
	case *ofWindow:
		sid, err := x.sessionID()
		if err != nil {
			return err
		}
		wid, err := x.windowID(sid)
		if err != nil {
			return err
		}
		return x.call("session.set_window_label", map[string]any{"session_id": sid, "window_id": wid, "label": text}, nil)
	}
	sid, bid, err := x.blockID()
	if err != nil {
		return err
	}
	return x.call("session.set_block_label", map[string]any{"session_id": sid, "block_id": bid, "label": text}, nil)
}

func cmdInspect(g globals, what string) error {
	x, err := open(g)
	if err != nil {
		return err
	}
	if what == "session" {
		sid, err := x.sessionID()
		if err != nil {
			return err
		}
		raw, err := x.c.CallRaw(x.ctx, "session.view", map[string]any{"session_id": sid})
		if err != nil {
			return err
		}
		return printRaw(raw)
	}
	sid, bid, err := x.blockID()
	if err != nil {
		return err
	}
	raw, err := x.c.CallRaw(x.ctx, "session.describe_block", map[string]any{"session_id": sid, "block_id": bid})
	if err != nil {
		return err
	}
	return printRaw(raw)
}

// ---- events ----

func cmdEvents(g globals, args []string) error {
	fs := flags("events")
	all := fs.Bool("all", false, "")
	since := fs.Int64("since", -1, "")
	if err := parse(fs, args); err != nil {
		return err
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	sid := ""
	if g.session != "" {
		if sid, err = x.sessionID(); err != nil {
			return err
		}
	}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	err = x.c.Events(x.ctx, sid, *all, *since, func(e client.Event) bool {
		if e.Type == "subscribed" || e.Type == "heartbeat" {
			return true
		}
		if g.json {
			w.Write(e.Raw)
			w.WriteByte('\n')
		} else {
			fmt.Fprintln(w, describeEvent(e))
		}
		w.Flush()
		return true
	})
	if errors.Is(x.ctx.Err(), context.Canceled) {
		return nil
	}
	return err
}

func describeEvent(e client.Event) string {
	ts := e.Time.Local().Format("15:04:05")
	var p map[string]any
	_ = json.Unmarshal(e.Payload, &p)
	if e.Type != "session_event" {
		return fmt.Sprintf("%s  %-22s %s", ts, e.Type, compact(p))
	}
	sid, _ := p["session_id"].(string)
	inner, _ := p["event"].(map[string]any)
	typ, _ := inner["type"].(string)
	ip, _ := inner["payload"].(map[string]any)
	if typ == "block_event" {
		bid, _ := ip["block_id"].(string)
		name, _ := ip["name"].(string)
		return fmt.Sprintf("%s  %-22s %s  %s %s", ts, "block_event", short(bid), name, compact(ip["payload"]))
	}
	return fmt.Sprintf("%s  %-22s %s  %s", ts, typ, short(sid), compact(ip))
}

func compact(v any) string {
	b, _ := json.Marshal(v)
	s := string(b)
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

// ---- api ----

func cmdAPI(g globals, args []string) error {
	if len(args) == 0 {
		return usagef("usage: hive api list|describe|call")
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		fs := flags("api list")
		quiet := fs.Bool("q", false, "")
		if err := parse(fs, args[1:]); err != nil {
			return err
		}
		raw, err := x.c.CallRaw(x.ctx, "api.list", map[string]any{"full": g.json})
		if err != nil {
			return err
		}
		if g.json {
			return printRaw(raw)
		}
		var ms []struct{ Name, Description string }
		_ = json.Unmarshal(raw, &ms)
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		if !*quiet {
			fmt.Fprintln(tw, "METHOD\tDESCRIPTION")
		}
		for _, m := range ms {
			if *quiet {
				fmt.Println(m.Name)
				continue
			}
			fmt.Fprintf(tw, "%s\t%s\n", m.Name, firstSentence(m.Description))
		}
		return tw.Flush()
	case "describe":
		if len(args) < 2 {
			return usagef("usage: hive api describe METHOD")
		}
		raw, err := x.c.CallRaw(x.ctx, "api.describe", map[string]any{"method": args[1]})
		if err != nil {
			return err
		}
		if g.json {
			return printRaw(raw)
		}
		var m struct {
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			InputSchema  json.RawMessage `json:"input_schema"`
			OutputSchema json.RawMessage `json:"output_schema"`
		}
		_ = json.Unmarshal(raw, &m)
		fmt.Printf("%s\n\nMethod: %s\n\nInput schema:\n%s\n\nOutput schema:\n%s\n", m.Description, m.Name, indent(m.InputSchema), indent(m.OutputSchema))
		return nil
	case "call":
		if len(args) < 2 {
			return usagef("usage: hive api call METHOD [JSON|@FILE|-]")
		}
		params := map[string]any{}
		if len(args) > 2 {
			var data []byte
			switch a := args[2]; {
			case a == "-":
				data, err = io.ReadAll(os.Stdin)
			case strings.HasPrefix(a, "@"):
				data, err = os.ReadFile(a[1:])
			default:
				data = []byte(a)
			}
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &params); err != nil {
				return usagef("arguments must be a JSON object: %v", err)
			}
		}
		if g.session != "" && strings.HasPrefix(args[1], "session.") {
			if _, ok := params["session_id"]; !ok {
				sid, err := x.sessionID()
				if err != nil {
					return err
				}
				params["session_id"] = sid
			}
		}
		raw, err := x.c.CallRaw(x.ctx, args[1], params)
		if err != nil {
			return err
		}
		return printRaw(raw)
	}
	return usagef("unknown api command %q", args[0])
}

func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		return s[:i+1]
	}
	return s
}

func indent(raw json.RawMessage) string {
	var v any
	_ = json.Unmarshal(raw, &v)
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

// ---- agents ----

func cmdAgents(g globals) error {
	x, err := open(g)
	if err != nil {
		return err
	}
	raw, err := x.c.CallRaw(x.ctx, "agent.list", nil)
	if err != nil {
		return err
	}
	if g.json {
		return printRaw(raw)
	}
	var out struct {
		Agents []struct {
			BlockID      string `json:"block_id"`
			SessionLabel string `json:"session_label"`
			SessionID    string `json:"session_id"`
			Label        string `json:"label"`
			Agent        struct {
				Harness   string    `json:"harness"`
				SessionID string    `json:"session_id"`
				State     string    `json:"state"`
				Message   string    `json:"message"`
				Cwd       string    `json:"cwd"`
				UpdatedAt time.Time `json:"updated_at"`
			} `json:"agent"`
		} `json:"agents"`
	}
	_ = json.Unmarshal(raw, &out)
	order := map[string]int{"blocked": 0, "done": 1, "working": 2, "idle": 3}
	sort.SliceStable(out.Agents, func(i, j int) bool {
		return order[out.Agents[i].Agent.State] < order[out.Agents[j].Agent.State]
	})
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATE\tHARNESS\tSESSION\tBLOCK\tDIR\tSINCE\tMESSAGE")
	for _, a := range out.Agents {
		sess := a.SessionLabel
		if sess == "" {
			sess = short(a.SessionID)
		}
		blk := a.Label
		if blk == "" {
			blk = short(a.BlockID)
		}
		msg := a.Agent.Message
		if len(msg) > 60 {
			msg = msg[:57] + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", a.Agent.State, a.Agent.Harness, sess, blk,
			tildePath(a.Agent.Cwd), age(a.Agent.UpdatedAt), msg)
	}
	return tw.Flush()
}

func tildePath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func cmdAgent(g globals, args []string) error {
	if len(args) < 2 {
		return usagef("usage: hive agent install|uninstall|hook HARNESS")
	}
	sub, harness := args[0], args[1]
	switch sub {
	case "hook":
		return agentHook(g, harness)
	case "install", "uninstall":
		if harness != "claude" {
			return usagef("hive installs hooks for claude only; other harnesses are resumed without hooks")
		}
		fs := flags("agent " + sub)
		dry := fs.Bool("dry-run", false, "")
		dir := fs.String("config-dir", claudeDir(), "")
		if err := parse(fs, args[2:]); err != nil {
			return err
		}
		var changes []string
		var err error
		if sub == "install" {
			exe, _ := os.Executable()
			changes, err = agents.InstallClaude(*dir, exe, *dry)
		} else {
			changes, err = agents.UninstallClaude(*dir, *dry)
		}
		if err != nil {
			return err
		}
		prefix := ""
		if *dry {
			prefix = "(dry run) "
		}
		if len(changes) == 0 {
			fmt.Println(prefix + "unchanged: " + filepath.Join(*dir, "settings.json"))
		}
		for _, c := range changes {
			fmt.Println(prefix + c + " in " + filepath.Join(*dir, "settings.json"))
		}
		if sub == "install" && !*dry {
			fmt.Println("Claude Code only runs hooks in folders you trust. Start new claude sessions inside hive to use them.")
		}
		return nil
	}
	return usagef("unknown agent command %q", sub)
}

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// agentHook is run by an agent's hook with the hook's JSON on stdin. It never
// fails the agent: errors go to stderr and it exits 0.
func agentHook(g globals, harness string) error {
	bid := os.Getenv("HIVE_BLOCK")
	if bid == "" {
		return nil // not running inside hive
	}
	data, _ := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	r, err := agents.ParseHook(harness, data)
	if errors.Is(err, agents.ErrNoChange) {
		return nil
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "hive:", err)
		return nil
	}
	c := client.New(socketFor(g))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = c.Call(ctx, "agent.report", map[string]any{
		"block_id": bid, "harness": r.Harness, "event": r.Event, "state": r.State,
		"session_id": r.SessionID, "cwd": r.Cwd, "transcript_path": r.TranscriptPath, "message": r.Message,
	}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hive:", err)
	}
	return nil
}
