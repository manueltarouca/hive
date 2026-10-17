// Package vt wraps a libghostty terminal for use from many goroutines.
//
// libghostty terminals are single-threaded and not reentrant, and their
// effect callbacks run synchronously inside VTWrite. Term serializes every
// call behind a mutex and splits effects in three:
//
//   - Replies the terminal needs right away (device attributes, size, color
//     scheme, clipboard) are answered inside the callback from cached state.
//   - Bytes the terminal writes back to the program are collected and handed
//     to Hooks.WritePty, in order, after the call returns.
//   - Notifications (title, bell, status...) are queued and fired after the
//     lock is released, so a hook may call back into Term.
package vt

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	ghostty "go.mitchellh.com/libghostty"
)

// MaxClipboardBytes is the largest OSC 52 copy passed on (1 MiB).
const MaxClipboardBytes = 1 << 20

// Hooks receive what the terminal reports. Every field is optional. They run
// after the terminal call that caused them, never with Term locked.
type Hooks struct {
	WritePty      func(data []byte)
	Title         func(title string)
	Pwd           func(pwd string)
	Bell          func()
	ProgramStatus func(ProgramStatus)
	Notification  func(title, body string)
	Progress      func(state string, progress int)
	Clipboard     func(text string)
	Prompt        func(Prompt)
}

// ProgramStatus is one OSC 7501 report.
type ProgramStatus struct {
	ID       string `json:"id,omitempty"`
	State    string `json:"state"`
	Kind     string `json:"kind,omitempty"`
	Progress int    `json:"progress,omitempty"`
	App      string `json:"app,omitempty"`
	Title    string `json:"title,omitempty"`
	Message  string `json:"msg,omitempty"`
}

// Prompt is an OSC 133 shell integration mark.
type Prompt struct {
	Kind     string // prompt_start, input_start, output_start, command_end
	ExitCode *int
	Command  string
}

// FormatOpts choose how Format renders the screen.
type FormatOpts struct {
	Format string // text (default) or vt
	Trim   bool
	Unwrap bool
}

// Term is a libghostty terminal that is safe for concurrent use.
type Term struct {
	// flushMu keeps operations and the delivery of their effects in order,
	// so WritePty chunks reach the program in the order they were produced.
	flushMu sync.Mutex
	mu      sync.Mutex
	t       *ghostty.Terminal
	hooks   Hooks

	// sizeMu guards the size, so reading it never waits for mu: a snapshot
	// of full scrollback holds mu for ~150 ms. Taken last, never held while
	// taking another lock.
	sizeMu       sync.Mutex
	cols, rows   uint16
	cellW, cellH uint32
	pending      []func()
	reply        []byte
	closed       bool
}

// Options for a new terminal.
const DefaultScrollbackBytes = 16 << 20

// New makes a terminal of the given size.
func New(cols, rows uint16, hooks Hooks) (*Term, error) {
	t, err := ghostty.NewTerminal(
		ghostty.WithSize(cols, rows),
		ghostty.WithMaxScrollbackBytes(DefaultScrollbackBytes),
		ghostty.WithContinuationMaxBytes(4096),
	)
	if err != nil {
		return nil, err
	}
	return wrap(t, hooks)
}

// Restore makes a terminal from a snapshot taken with Snapshot.
func Restore(snapshot []byte, hooks Hooks) (*Term, error) {
	d, err := ghostty.NewSnapshotDecoderBytesCopy(snapshot)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	t, err := d.Decode()
	if err != nil {
		return nil, err
	}
	// A decoded terminal starts with libghostty's defaults; apply the same
	// scrollback cap as New.
	limit := uint(DefaultScrollbackBytes)
	if err := t.SetScrollbackMaxBytes(&limit); err != nil {
		t.Close()
		return nil, err
	}
	return wrap(t, hooks)
}

func wrap(t *ghostty.Terminal, hooks Hooks) (*Term, error) {
	cols, err := t.Cols()
	if err != nil {
		t.Close()
		return nil, err
	}
	rows, err := t.Rows()
	if err != nil {
		t.Close()
		return nil, err
	}
	vt := &Term{t: t, hooks: hooks, cols: cols, rows: rows, cellW: 8, cellH: 16}
	vt.install()
	return vt, nil
}

func (v *Term) queue(fn func()) { v.pending = append(v.pending, fn) }

func (v *Term) install() {
	t, h := v.t, v.hooks
	t.SetEffectWritePty(func(_ *ghostty.Terminal, data []byte) {
		v.reply = append(v.reply, data...)
	})
	t.SetEffectDeviceAttributes(func(*ghostty.Terminal) (ghostty.DeviceAttributes, bool) {
		var da ghostty.DeviceAttributes
		// Like Ghostty: VT220 conformance with ANSI color (22).
		da.Primary.ConformanceLevel = 62
		da.Primary.Features[0] = 22
		da.Primary.NumFeatures = 1
		da.Secondary.DeviceType = 1
		da.Secondary.FirmwareVersion = 10
		return da, true
	})
	t.SetEffectSize(func(*ghostty.Terminal) (ghostty.SizeReportSize, bool) {
		v.sizeMu.Lock()
		defer v.sizeMu.Unlock()
		return ghostty.SizeReportSize{Rows: v.rows, Columns: v.cols, CellWidth: v.cellW, CellHeight: v.cellH}, true
	})
	t.SetEffectColorScheme(func(*ghostty.Terminal) (ghostty.ColorScheme, bool) {
		return ghostty.ColorSchemeDark, true
	})
	t.SetEffectXtversion(func(*ghostty.Terminal) string { return "hive" })
	t.SetEffectClipboardWrite(func(_ *ghostty.Terminal, w ghostty.ClipboardWrite) ghostty.ClipboardWriteReply {
		var text string
		for _, c := range w.Contents {
			if strings.HasPrefix(c.MIME, "text/plain") && utf8.Valid(c.Data) {
				text = string(c.Data)
				break
			}
		}
		// Empty and oversized copies are dropped, not truncated: the text goes
		// to every event subscriber, and half a paste could be half a command.
		if h.Clipboard != nil && text != "" && len(text) <= MaxClipboardBytes {
			v.queue(func() { h.Clipboard(text) })
		}
		return ghostty.ClipboardWriteReply{Result: ghostty.ClipboardWriteSuccess}
	})
	if h.Title != nil {
		t.SetEffectTitleChanged(func(t *ghostty.Terminal) {
			s, _ := t.Title()
			v.queue(func() { h.Title(s) })
		})
	}
	if h.Pwd != nil {
		t.SetEffectPwdChanged(func(t *ghostty.Terminal) {
			s, _ := t.Pwd()
			v.queue(func() { h.Pwd(s) })
		})
	}
	if h.Bell != nil {
		t.SetEffectBell(func(*ghostty.Terminal) { v.queue(h.Bell) })
	}
	if h.ProgramStatus != nil {
		t.SetEffectProgramStatus(func(_ *ghostty.Terminal, s ghostty.ProgramStatus) {
			ps := convertStatus(s)
			v.queue(func() { h.ProgramStatus(ps) })
		})
	}
	if h.Notification != nil {
		t.SetEffectDesktopNotification(func(_ *ghostty.Terminal, n ghostty.TerminalDesktopNotification) {
			v.queue(func() { h.Notification(n.Title, n.Body) })
		})
	}
	if h.Progress != nil {
		t.SetEffectProgressReport(func(_ *ghostty.Terminal, r ghostty.TerminalProgressReport) {
			state := progressStates[r.State]
			v.queue(func() { h.Progress(state, int(r.Progress)) })
		})
	}
	if h.Prompt != nil {
		t.SetEffectSemanticPrompt(func(_ *ghostty.Terminal, e ghostty.TerminalSemanticPrompt) {
			p := Prompt{Kind: promptKinds[e.Kind], Command: e.Command}
			if e.HasExitCode {
				code := e.ExitCode
				p.ExitCode = &code
			}
			v.queue(func() { h.Prompt(p) })
		})
	}
}

var progressStates = map[ghostty.TerminalProgressState]string{
	ghostty.TerminalProgressStateRemove:        "remove",
	ghostty.TerminalProgressStateSet:           "set",
	ghostty.TerminalProgressStateError:         "error",
	ghostty.TerminalProgressStateIndeterminate: "indeterminate",
	ghostty.TerminalProgressStatePause:         "pause",
}

var promptKinds = map[ghostty.SemanticPromptKind]string{
	ghostty.SemanticPromptStart:       "prompt_start",
	ghostty.SemanticPromptInputStart:  "input_start",
	ghostty.SemanticPromptOutputStart: "output_start",
	ghostty.SemanticPromptCommandEnd:  "command_end",
}

var statusStates = map[ghostty.ProgramStatusState]string{
	ghostty.ProgramStatusStateIdle:    "idle",
	ghostty.ProgramStatusStateWorking: "working",
	ghostty.ProgramStatusStateDone:    "done",
	ghostty.ProgramStatusStateBlocked: "blocked",
	ghostty.ProgramStatusStateError:   "error",
	ghostty.ProgramStatusStateClear:   "clear",
}

var statusKinds = map[ghostty.ProgramStatusKind]string{
	ghostty.ProgramStatusKindPermission: "permission",
	ghostty.ProgramStatusKindQuestion:   "question",
	ghostty.ProgramStatusKindAuth:       "auth",
}

func convertStatus(s ghostty.ProgramStatus) ProgramStatus {
	p := ProgramStatus{
		ID: s.ID, State: statusStates[s.State], Kind: statusKinds[s.Kind],
		App: s.App, Title: s.Title, Message: s.Message,
	}
	if s.Progress >= 0 {
		p.Progress = int(s.Progress)
	}
	return p
}

// ErrClosed is returned after Close.
var ErrClosed = errors.New("vt: terminal closed")

// do runs fn with the terminal locked, then delivers the effects it caused.
func (v *Term) do(fn func(t *ghostty.Terminal) error) error {
	v.flushMu.Lock()
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		v.flushMu.Unlock()
		return ErrClosed
	}
	err := fn(v.t)
	reply, pending := v.reply, v.pending
	v.reply, v.pending = nil, nil
	v.mu.Unlock()

	// Replies go to the program while flushMu is still held, so they are
	// written in the order the terminal produced them. WritePty must not call
	// back into Term.
	if len(reply) > 0 && v.hooks.WritePty != nil {
		v.hooks.WritePty(reply)
	}
	v.flushMu.Unlock()

	// Notifications run with nothing held, so they may call back into Term.
	for _, f := range pending {
		f()
	}
	return err
}

// Feed processes output from the program.
func (v *Term) Feed(data []byte) {
	_ = v.do(func(t *ghostty.Terminal) error {
		t.VTWrite(data)
		return nil
	})
}

// Resize changes the terminal's size in cells, keeping the cell size.
func (v *Term) Resize(cols, rows uint16) error {
	cw, ch := v.CellSize()
	return v.ResizePx(cols, rows, cw, ch)
}

// ResizePx changes the size in cells and the size of a cell in pixels, which
// programs can ask for (CSI 14 t, SGR-pixel mouse reports).
func (v *Term) ResizePx(cols, rows uint16, cellW, cellH uint32) error {
	if cellW == 0 || cellH == 0 {
		cellW, cellH = 8, 16
	}
	return v.do(func(t *ghostty.Terminal) error {
		if err := t.Resize(cols, rows, cellW, cellH); err != nil {
			return err
		}
		v.sizeMu.Lock()
		v.cols, v.rows, v.cellW, v.cellH = cols, rows, cellW, cellH
		v.sizeMu.Unlock()
		return nil
	})
}

// CellSize returns the size of a cell in pixels.
func (v *Term) CellSize() (w, h uint32) {
	v.sizeMu.Lock()
	defer v.sizeMu.Unlock()
	return v.cellW, v.cellH
}

// Size returns the terminal's size in cells.
func (v *Term) Size() (cols, rows uint16) {
	v.sizeMu.Lock()
	defer v.sizeMu.Unlock()
	return v.cols, v.rows
}

// Reset performs a full reset.
func (v *Term) Reset() {
	_ = v.do(func(t *ghostty.Terminal) error {
		t.Reset()
		return nil
	})
}

// SetScrollbackLimit caps the history kept, in bytes. Lowering it drops old
// lines at once.
func (v *Term) SetScrollbackLimit(n uint) error {
	return v.do(func(t *ghostty.Terminal) error { return t.SetScrollbackMaxBytes(&n) })
}

// Format renders the terminal's contents, scrollback included.
func (v *Term) Format(o FormatOpts) (string, error) {
	format := ghostty.FormatterFormatPlain
	switch o.Format {
	case "", "text":
	case "vt":
		format = ghostty.FormatterFormatVT
	default:
		return "", fmt.Errorf("unknown format %q", o.Format)
	}
	var out string
	err := v.do(func(t *ghostty.Terminal) error {
		f, err := ghostty.NewFormatter(t,
			ghostty.WithFormatterFormat(format),
			ghostty.WithFormatterTrim(o.Trim),
			ghostty.WithFormatterUnwrap(o.Unwrap),
		)
		if err != nil {
			return err
		}
		defer f.Close()
		out, err = f.FormatString()
		return err
	})
	return out, err
}

// Snapshot encodes the terminal's whole state: screens, scrollback, modes,
// title, and any partially received sequence.
func (v *Term) Snapshot() ([]byte, error) {
	var out []byte
	err := v.do(func(t *ghostty.Terminal) error {
		var err error
		out, err = t.Snapshot()
		return err
	})
	return out, err
}

// SnapshotTo streams a snapshot to w.
func (v *Term) SnapshotTo(w io.Writer) error {
	return v.do(func(t *ghostty.Terminal) error {
		_, err := t.SnapshotWriteTo(w)
		return err
	})
}

// Title returns the terminal's title.
func (v *Term) Title() string {
	var s string
	_ = v.do(func(t *ghostty.Terminal) error {
		s, _ = t.Title()
		return nil
	})
	return s
}

// Pwd returns the working directory the program last reported.
func (v *Term) Pwd() string {
	var s string
	_ = v.do(func(t *ghostty.Terminal) error {
		s, _ = t.Pwd()
		return nil
	})
	return s
}

// EncodeKey turns a key name such as "ctrl+c", "enter", or "shift+up" into
// the bytes the program expects, given the terminal's current modes.
func (v *Term) EncodeKey(name string) ([]byte, error) {
	ev, err := parseKey(name)
	if err != nil {
		return nil, err
	}
	var out []byte
	err = v.do(func(t *ghostty.Terminal) error {
		enc, err := ghostty.NewKeyEncoder()
		if err != nil {
			return err
		}
		defer enc.Close()
		enc.SetOptFromTerminal(t)
		ke, err := ghostty.NewKeyEvent()
		if err != nil {
			return err
		}
		defer ke.Close()
		ke.SetAction(ghostty.KeyActionPress)
		ke.SetKey(ev.key)
		ke.SetMods(ev.mods)
		ke.SetUTF8(ev.utf8)
		ke.SetUnshiftedCodepoint(ev.cp)
		out, err = enc.Encode(ke)
		return err
	})
	return out, err
}

// Close frees the terminal.
func (v *Term) Close() {
	v.flushMu.Lock()
	defer v.flushMu.Unlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.closed {
		v.closed = true
		v.t.Close()
	}
}

// AltScreen reports whether the alternate screen is active.
func (v *Term) AltScreen() bool {
	var alt bool
	_ = v.do(func(t *ghostty.Terminal) error {
		s, err := t.ActiveScreen()
		alt = err == nil && s == ghostty.ScreenAlternate
		return nil
	})
	return alt
}
