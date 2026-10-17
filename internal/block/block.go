// Package block runs one terminal: a program on a pseudo-terminal, a headless
// libghostty terminal fed by its output, and the subscribers attached to it.
package block

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/manueltarouca/hive/internal/vt"
	"golang.org/x/sys/unix"
)

// Options say what to run and how.
type Options struct {
	Command  []string `json:"command,omitempty"` // empty: the user's login shell
	Cwd      string   `json:"cwd,omitempty"`
	Env      []string `json:"-"`
	Cols     uint16   `json:"-"`
	Rows     uint16   `json:"-"`
	KeepOpen bool     `json:"keep_open,omitempty"`
	// Restore, when set, is a snapshot the terminal starts from.
	Restore []byte `json:"-"`
	// Preamble is written to the terminal before the program starts, such as
	// a "restored" banner.
	Preamble []byte `json:"-"`
	// InitialInput is typed into the program once it starts.
	InitialInput string `json:"initial_input,omitempty"`
}

// Event is something the block reports, named as in the events reference:
// child_exited, title_changed, pwd_changed, bell, process_changed,
// desktop_notification, progress_report, clipboard_written,
// program_status_changed, prompt.
type Event struct {
	BlockID string
	Name    string
	Payload map[string]any
}

// Frame is a piece of a block's output stream, or a resync request.
type Frame struct {
	BlockID string
	Seq     uint64 // byte offset of Data[0] in the block's output
	Data    []byte
	Resync  bool // the subscriber fell behind; re-attach for a new snapshot
	// Resize marks where in the stream the terminal was resized. A mirror
	// applies it before the bytes that follow.
	Resize       bool
	Cols, Rows   uint16
	CellW, CellH uint16
}

// Sub is one attached subscriber.
type Sub struct {
	C       chan Frame
	overrun bool
}

// Process describes a process on the terminal.
type Process struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
	Cwd  string `json:"cwd,omitempty"`
	// Program names what runs, the way a person would (see programOf).
	Program string `json:"program,omitempty"`
}

// Block is a running terminal.
type Block struct {
	ID   string
	opts Options
	emit func(Event)

	term *vt.Term
	ptmx *os.File
	cmd  *exec.Cmd

	// pubMu orders output: feeding the terminal, advancing seq, and
	// publishing to subscribers happen together, so a snapshot taken under it
	// is exactly at seq.
	pubMu sync.Mutex
	seq   uint64
	subs  map[*Sub]struct{}

	mu        sync.Mutex
	exited    bool
	exitCode  int
	closed    bool
	lastFg    Process
	startedAt time.Time

	done chan struct{}
	stop chan struct{}

	// Events leave through a queue drained by one goroutine, so emit never
	// runs under pubMu (the server's emit takes the server lock, and the
	// server takes pubMu while holding it) and events keep their order.
	// Replies the terminal sends to the program (device attributes, cursor
	// reports...) go through one writer goroutine, in order, so a program
	// that never reads its input can't stall the output path.
	replies chan []byte

	evMu    sync.Mutex
	evQueue []Event
	evWake  chan struct{}
	evDone  chan struct{}
}

// Start launches a block. emit receives its events; it must not block.
func Start(blockID string, opts Options, emit func(Event)) (*Block, error) {
	if opts.Cols == 0 {
		opts.Cols = 80
	}
	if opts.Rows == 0 {
		opts.Rows = 24
	}
	b := &Block{ID: blockID, opts: opts, emit: emit, subs: map[*Sub]struct{}{},
		done: make(chan struct{}), stop: make(chan struct{}), startedAt: time.Now(),
		evWake: make(chan struct{}, 1), evDone: make(chan struct{}), replies: make(chan []byte, 64)}
	go b.eventLoop()

	hooks := b.hooks()
	var err error
	restored := false
	if len(opts.Restore) > 0 {
		// A snapshot that can't be decoded, such as one from an older
		// libghostty, costs the scrollback but never the pane.
		if b.term, err = vt.Restore(opts.Restore, hooks); err == nil {
			restored = true
			err = b.term.Resize(opts.Cols, opts.Rows)
		}
	}
	if !restored {
		b.term, err = vt.New(opts.Cols, opts.Rows, hooks)
		if err == nil && len(opts.Restore) > 0 {
			b.term.Feed([]byte("\x1b[2m(scrollback could not be restored)\x1b[0m\r\n"))
		}
	}
	if err != nil {
		return nil, fmt.Errorf("terminal: %w", err)
	}
	// A restored terminal may be on the alternate screen of a program that is
	// gone. Leave it with 1049l only then: 1049l also restores the cursor
	// saved on entry, and with nothing saved it would jump to the top.
	if restored && b.term.AltScreen() {
		b.term.Feed([]byte("\x1b[?1049l"))
	}
	if len(opts.Preamble) > 0 {
		b.term.Feed(opts.Preamble)
	}

	argv := opts.Command
	if len(argv) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		argv = []string{shell, "-l"}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = opts.Cwd
	cmd.Env = append(baseEnv(), opts.Env...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: opts.Cols, Rows: opts.Rows})
	if err != nil {
		b.term.Close()
		return nil, err
	}
	b.cmd, b.ptmx = cmd, ptmx

	go b.readLoop()
	go b.replyLoop()
	go b.waitLoop()
	go b.processLoop()
	if opts.InitialInput != "" {
		go func() {
			time.Sleep(100 * time.Millisecond)
			_ = b.Write([]byte(opts.InitialInput))
		}()
	}
	return b, nil
}

// agentSessionVars belong to the coding agent that started the server, not
// to the user. Passed on, they make a nested claude think it runs inside that
// agent, and they leak its messaging token. Configuration variables the user
// sets (CLAUDE_CODE_SUBAGENT_MODEL and the like) are kept.
var agentSessionVars = map[string]bool{
	"CLAUDECODE": true, "CLAUDE_PID": true, "CLAUDE_CODE_ENTRYPOINT": true,
	"CLAUDE_CODE_SESSION_ID": true, "CLAUDE_CODE_CHILD_SESSION": true,
	"CLAUDE_CODE_SESSION_ATTENDED": true, "CLAUDE_CODE_EXECPATH": true,
	"CLAUDE_CODE_MESSAGING_SOCKET": true, "CLAUDE_CODE_MESSAGING_TOKEN": true,
}

func baseEnv() []string {
	env := []string{}
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); agentSessionVars[name] {
			continue
		}
		switch {
		case len(kv) > 5 && kv[:5] == "TERM=",
			len(kv) > 10 && kv[:10] == "COLORTERM=",
			len(kv) > 13 && kv[:13] == "TERM_PROGRAM=",
			len(kv) > 5 && kv[:5] == "TMUX=":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=hive")
}

func (b *Block) send(name string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	b.evMu.Lock()
	b.evQueue = append(b.evQueue, Event{BlockID: b.ID, Name: name, Payload: payload})
	b.evMu.Unlock()
	select {
	case b.evWake <- struct{}{}:
	default:
	}
}

// eventLoop delivers queued events in order. It ends after child_exited has
// been delivered and the queue is empty.
func (b *Block) eventLoop() {
	defer close(b.evDone)
	for {
		b.evMu.Lock()
		q := b.evQueue
		b.evQueue = nil
		b.evMu.Unlock()
		exited := false
		for _, e := range q {
			b.emit(e)
			exited = exited || e.Name == "child_exited"
		}
		if exited {
			return
		}
		<-b.evWake
	}
}

func (b *Block) owner() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lastFg.PID == 0 {
		return nil
	}
	return map[string]any{"pid": b.lastFg.PID, "name": b.lastFg.Name}
}

func (b *Block) hooks() vt.Hooks {
	return vt.Hooks{
		WritePty: func(data []byte) {
			select {
			case b.replies <- append([]byte(nil), data...):
			default: // the program isn't reading; drop the reply
			}
		},
		Title: func(s string) { b.send("title_changed", map[string]any{"title": s, "owner": b.owner()}) },
		Pwd:   func(s string) { b.send("pwd_changed", map[string]any{"pwd": s, "owner": b.owner()}) },
		Bell:  func() { b.send("bell", nil) },
		ProgramStatus: func(s vt.ProgramStatus) {
			b.send("program_status_changed", map[string]any{"id": s.ID, "record": s, "owner": b.owner()})
		},
		Notification: func(title, body string) {
			b.send("desktop_notification", map[string]any{"title": title, "body": body})
		},
		Progress: func(state string, progress int) {
			b.send("progress_report", map[string]any{"state": state, "progress": progress})
		},
		Clipboard: func(text string) { b.send("clipboard_written", map[string]any{"text": text}) },
		Prompt: func(p vt.Prompt) {
			payload := map[string]any{"kind": p.Kind}
			if p.ExitCode != nil {
				payload["exit_code"] = *p.ExitCode
			}
			b.send("prompt", payload)
		},
	}
}

func (b *Block) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := b.ptmx.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			b.pubMu.Lock()
			b.term.Feed(data)
			f := Frame{BlockID: b.ID, Seq: b.seq, Data: data}
			b.seq += uint64(n)
			for s := range b.subs {
				publish(s, f)
			}
			b.pubMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// publish never blocks. A subscriber that can't keep up loses its queued
// frames and gets one Resync instead, since a gap would corrupt its mirror.
func publish(s *Sub, f Frame) {
	if s.overrun {
		return
	}
	select {
	case s.C <- f:
		return
	default:
	}
	s.overrun = true
	for {
		select {
		case <-s.C:
			continue
		default:
		}
		break
	}
	select {
	case s.C <- Frame{BlockID: f.BlockID, Resync: true}:
	default:
	}
}

func (b *Block) replyLoop() {
	for {
		select {
		case <-b.stop:
			return
		case <-b.done:
			return
		case data := <-b.replies:
			_, _ = b.ptmx.Write(data)
		}
	}
}

// PID returns the program's process ID.
func (b *Block) PID() int {
	if b.cmd == nil || b.cmd.Process == nil {
		return 0
	}
	return b.cmd.Process.Pid
}

func (b *Block) waitLoop() {
	err := b.cmd.Wait()
	code := 0
	payload := map[string]any{}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				payload["signal"] = ws.Signal().String()
			}
		} else {
			code = -1
		}
		payload["error"] = err.Error()
	}
	// Let the read loop drain what the program printed last.
	time.Sleep(50 * time.Millisecond)
	payload["exit_code"] = code
	b.mu.Lock()
	b.exited, b.exitCode = true, code
	b.mu.Unlock()
	b.send("child_exited", payload)
	// Done means exited and reported: wait for the event to be delivered.
	<-b.evDone
	close(b.done)
}

func (b *Block) processLoop() {
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-b.done:
			return
		case <-b.stop:
			return
		case <-t.C:
		}
		p, err := b.Foreground()
		if err != nil {
			continue
		}
		b.mu.Lock()
		changed := p.PID != b.lastFg.PID || p.Name != b.lastFg.Name || p.Program != b.lastFg.Program
		b.lastFg = p
		b.mu.Unlock()
		if changed {
			b.send("process_changed", map[string]any{"foreground": p})
		}
	}
}

// Foreground returns the process in the terminal's foreground.
func (b *Block) Foreground() (Process, error) {
	// SyscallConn, not Fd: Fd would put the pty into blocking mode.
	rc, err := b.ptmx.SyscallConn()
	if err != nil {
		return Process{}, err
	}
	var pgid int
	var ioErr error
	if err := rc.Control(func(fd uintptr) { pgid, ioErr = unix.IoctlGetInt(int(fd), unix.TIOCGPGRP) }); err != nil {
		return Process{}, err
	}
	if ioErr != nil {
		return Process{}, ioErr
	}
	return describe(pgid)
}

// Done is closed when the program has exited and child_exited has been
// delivered.
func (b *Block) Done() <-chan struct{} { return b.done }

// ExitStatus reports the program's exit code, once it has exited.
func (b *Block) ExitStatus() (code int, exited bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exitCode, b.exited
}

// Options returns what the block was started with.
func (b *Block) Options() Options { return b.opts }

// Write sends bytes to the program as if they were typed.
func (b *Block) Write(p []byte) error {
	_, err := b.ptmx.Write(p)
	return err
}

// SendKey presses a key, encoded for the terminal's current modes.
func (b *Block) SendKey(name string) error {
	data, err := b.term.EncodeKey(name)
	if err != nil {
		return err
	}
	return b.Write(data)
}

// Resize changes the size of the terminal and the pseudo-terminal, keeping
// the cell size.
func (b *Block) Resize(cols, rows uint16) error {
	cw, ch := b.term.CellSize()
	return b.ResizePx(cols, rows, cw, ch)
}

// ResizePx changes the size in cells and the cell size in pixels. The
// resize is published in the output stream at its position, before the
// program learns of it, so mirrors reflow at the same point.
func (b *Block) ResizePx(cols, rows uint16, cellW, cellH uint32) error {
	if cols == 0 || rows == 0 {
		return errors.New("size must be at least 1x1")
	}
	if cellW == 0 || cellH == 0 {
		cellW, cellH = b.term.CellSize()
	}
	b.pubMu.Lock()
	defer b.pubMu.Unlock()
	if err := b.term.ResizePx(cols, rows, cellW, cellH); err != nil {
		return err
	}
	f := Frame{BlockID: b.ID, Seq: b.seq, Resize: true, Cols: cols, Rows: rows, CellW: uint16(cellW), CellH: uint16(cellH)}
	for s := range b.subs {
		publish(s, f)
	}
	return pty.Setsize(b.ptmx, &pty.Winsize{Cols: cols, Rows: rows,
		X: uint16(uint32(cols) * cellW), Y: uint16(uint32(rows) * cellH)})
}

// CellSize returns the size of a cell in pixels.
func (b *Block) CellSize() (w, h uint32) { return b.term.CellSize() }

// Size returns the terminal's size in cells.
func (b *Block) Size() (cols, rows uint16) { return b.term.Size() }

// Format renders the screen and scrollback.
func (b *Block) Format(format string, trim, unwrap bool) (string, error) {
	return b.term.Format(vt.FormatOpts{Format: format, Trim: trim, Unwrap: unwrap})
}

// Title and Pwd return what the program last reported.
func (b *Block) Title() string { return b.term.Title() }
func (b *Block) Pwd() string   { return b.term.Pwd() }

// Snapshot encodes the terminal's state.
func (b *Block) Snapshot() ([]byte, error) {
	b.pubMu.Lock()
	defer b.pubMu.Unlock()
	return b.term.Snapshot()
}

// Attach subscribes to the block's output. It returns a snapshot of the
// terminal and the seq at which it was taken; every frame on sub.C follows
// it without a gap until a Resync.
func (b *Block) Attach(buffer int) (*Sub, []byte, uint64, error) {
	if buffer < 1 {
		buffer = 1
	}
	b.pubMu.Lock()
	defer b.pubMu.Unlock()
	snap, err := b.term.Snapshot()
	if err != nil {
		return nil, nil, 0, err
	}
	s := &Sub{C: make(chan Frame, buffer)}
	b.subs[s] = struct{}{}
	return s, snap, b.seq, nil
}

// Detach removes a subscriber.
func (b *Block) Detach(s *Sub) {
	b.pubMu.Lock()
	delete(b.subs, s)
	b.pubMu.Unlock()
}

// Close ends the program and frees the terminal.
func (b *Block) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.mu.Unlock()
	close(b.stop)
	if b.cmd.Process != nil {
		_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGHUP)
		_ = b.cmd.Process.Signal(syscall.SIGHUP)
	}
	select {
	case <-b.done:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-b.cmd.Process.Pid, syscall.SIGKILL)
		_ = b.cmd.Process.Kill()
		<-b.done
	}
	_ = b.ptmx.Close()
	b.term.Close()
}

// Seq returns how many bytes the program has printed.
func (b *Block) Seq() uint64 {
	b.pubMu.Lock()
	defer b.pubMu.Unlock()
	return b.seq
}

// SnapshotTo streams a snapshot to w.
func (b *Block) SnapshotTo(w io.Writer) error {
	b.pubMu.Lock()
	defer b.pubMu.Unlock()
	return b.term.SnapshotTo(w)
}

// EventsDone is closed once every event, child_exited last, has been
// delivered.
func (b *Block) EventsDone() <-chan struct{} { return b.evDone }
