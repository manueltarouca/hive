// Package server is the hive server: it owns sessions and blocks, serves the
// API over a Unix socket, publishes events, and keeps everything on disk so
// sessions come back after a restart.
//
// Lock order: Server.mu, then a block's locks. A block never takes Server.mu
// while holding its own locks; its events reach the server through a queue
// drained by a goroutine of their own. Blocks are never closed under
// Server.mu: closing waits for the program to exit, and its exit event needs
// Server.mu.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/manueltarouca/hive/internal/agents"
	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/block"
	"github.com/manueltarouca/hive/internal/config"
	"github.com/manueltarouca/hive/internal/events"
	"github.com/manueltarouca/hive/internal/id"
	"github.com/manueltarouca/hive/internal/persist"
	"github.com/manueltarouca/hive/internal/remote"
	"github.com/manueltarouca/hive/internal/session"
	"github.com/manueltarouca/hive/internal/vt"
)

// Version of hive, as semver. Release builds set it at link time from the
// git tag (see the Makefile); other builds are 0.0.0-dev.
var Version = "0.0.0-dev"

// Config for a server.
type Config struct {
	Socket   string
	StateDir string
	// Fresh ignores saved state.
	Fresh bool
	// HivePath is the hive binary, put on PATH in every terminal.
	HivePath string
	Logger   *slog.Logger
	// FlushDelay is how long after a change state is written. Default 1s.
	FlushDelay time.Duration
	// ResumeArgv builds the command that resumes an agent. Default
	// agents.ResumeArgv.
	ResumeArgv func(harness, sessionID string) []string
	// ConfigPath is the settings file; empty means defaults and no file
	// (tests). ConfigPoll is how often it's checked for edits (default 2s).
	ConfigPath string
	ConfigPoll time.Duration
	// OnFlush, when set, hears about every save (benchmarks).
	OnFlush func(FlushStats)
	// Remote, when set, adds listeners beyond the socket.
	Remote *RemoteConfig
	// beforeSnapshot, when set, runs before each snapshot is written (tests).
	beforeSnapshot func(blockID string)
	// beforeStateSave, when set, runs before state.json is written (tests).
	beforeStateSave func()
	// How often attached clients are pinged, and how long a pong may take
	// (tests; 0: 20 s and 15 s).
	attachPingEvery, attachPongWait time.Duration
	// beforeAttachMessage, when set, runs before an attach message is
	// handled (tests: a handler that blocks, as a pty write can on Linux).
	beforeAttachMessage func(typ string)
}

// FlushStats describes one save: how long it took, from waiting for the
// previous one to state.json written, and how many terminals it snapshotted.
type FlushStats struct {
	Took      time.Duration
	Wait      time.Duration // waiting for other saves' locks
	Snapshots int
}

type exitRec struct {
	code int
	at   time.Time
}

type rec struct {
	b       *block.Block
	st      *persist.BlockState
	snapSeq uint64 // output position of the last snapshot
	owner   string // client with size authority
	closing bool
}

// Client is a program connected to the server, such as the Mac app.
type Client struct {
	ID          string    `json:"client_id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Platform    string    `json:"platform,omitempty"`
	Version     string    `json:"version,omitempty"`
	Principal   string    `json:"principal" desc:"Who the connection speaks for: peer:UID on the unix socket."`
	ConnectedAt time.Time `json:"connected_at"`
}

// Server is a running hive server.
type Server struct {
	// Attached clients, ended by Shutdown.
	attachMu sync.Mutex
	attaches map[*attachConn]struct{}

	// Remote listeners (guarded by mu) and built-in Tailscale.
	listeners    []listenerInfo
	node         *remote.Node
	nodeStatus   *remote.NodeStatus
	removeOnStop []string

	// conf is the settings (guarded by mu), confStamp the file's identity
	// when it was read.
	conf      config.Config
	confDiags []config.Diagnostic
	confStamp fileStamp
	confMu    sync.Mutex // serializes reloadConfig

	cfg   Config
	log   *slog.Logger
	reg   *api.Registry
	bus   *events.Bus
	store *persist.Store

	mu       sync.Mutex
	sessions []*session.Session
	blocks   map[string]*rec
	clients  map[string]*Client
	// exits remembers how recently closed blocks ended, so a wait that comes
	// after the block is gone still gets the status.
	exits   map[string]exitRec
	started time.Time
	// serverID names this install (its state directory) for clients telling servers apart.
	serverID string
	// unlockState lets go of the state directory, at the end of Shutdown.
	unlockState func()

	dirty    chan struct{}
	flushReq chan chan error
	// stateSave asks stateLoop to write state.json now (agent.report).
	stateSave chan struct{}
	// sealed, under stateMu: Shutdown's final save is done, and no later
	// save may write (blocks are closing, so their state would be lost).
	sealed   bool
	quit     chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once

	ln  net.Listener
	srv *http.Server
}

// New makes a server and restores its saved state.
func New(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.FlushDelay == 0 {
		cfg.FlushDelay = time.Second
	}
	if cfg.ResumeArgv == nil {
		cfg.ResumeArgv = agents.ResumeArgv
	}
	if cfg.ConfigPoll == 0 {
		cfg.ConfigPoll = 2 * time.Second
	}
	// First: a second server on this state directory would restore the same
	// sessions, start their programs again, and write over this one's state.
	unlock, err := lockStateDir(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	s := &Server{attaches: map[*attachConn]struct{}{}, unlockState: unlock,
		cfg: cfg, log: cfg.Logger, reg: api.New(), bus: events.New(4096),
		store:  persist.New(cfg.StateDir),
		blocks: map[string]*rec{}, clients: map[string]*Client{}, exits: map[string]exitRec{}, started: time.Now(),
		dirty: make(chan struct{}, 1), flushReq: make(chan chan error), stateSave: make(chan struct{}, 1),
		quit: make(chan struct{}), stopped: make(chan struct{}),
	}
	s.serverID = loadServerID(cfg.StateDir, s.log)
	s.conf, s.confDiags, s.confStamp = s.readConfig()
	for _, d := range s.confDiags {
		s.log.Warn("config", "path", cfg.ConfigPath, "problem", d.String())
	}
	if !s.conf.ResumeAgents {
		cfg.ResumeArgv = func(string, string) []string { return nil } // read at start: resume-agents = false
		s.cfg.ResumeArgv = cfg.ResumeArgv
	}
	s.registerMethods()
	if cfg.ConfigPath != "" {
		go s.watchConfig()
	}
	// Built here, not in Serve, so a Shutdown racing a starting Serve sees it.
	s.srv = &http.Server{Handler: s.handler(), ReadHeaderTimeout: 10 * time.Second, ConnContext: remote.ConnContext}
	if !cfg.Fresh {
		if err := s.restore(); err != nil {
			s.log.Error("restore failed; starting empty", "err", err)
		}
	}
	return s, nil
}

// Registry exposes the API, for tests and in-process callers.
func (s *Server) Registry() *api.Registry { return s.reg }

// Bus exposes the event bus.
func (s *Server) Bus() *events.Bus { return s.bus }

// Serve listens on the socket and serves until Shutdown.
func (s *Server) Serve() error {
	if err := os.MkdirAll(filepath.Dir(s.cfg.Socket), 0o700); err != nil {
		return err
	}
	if c, err := net.DialTimeout("unix", s.cfg.Socket, 200*time.Millisecond); err == nil {
		c.Close()
		return fmt.Errorf("a server is already listening on %s", s.cfg.Socket)
	}
	_ = os.Remove(s.cfg.Socket)
	// Owner-only from the moment it exists, not after a chmod.
	old := syscall.Umask(0o077)
	ln, err := net.Listen("unix", s.cfg.Socket)
	syscall.Umask(old)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.cfg.Socket, 0o600); err != nil {
		ln.Close()
		return fmt.Errorf("socket permissions: %w", err)
	}
	s.ln = ln
	if err := s.serveRemote(); err != nil {
		ln.Close()
		s.srv.Close() // the extra listeners already being served
		s.stopRemote()
		return err
	}
	go s.persistLoop()
	go s.stateLoop()
	s.log.Info("listening", "socket", s.cfg.Socket, "state", s.cfg.StateDir)
	err = s.srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		<-s.stopped
		return nil
	}
	return err
}

// Shutdown saves everything and stops. Programs are ended; their sessions are
// restored at the next start.
func (s *Server) Shutdown() {
	s.stopOnce.Do(func() {
		close(s.quit)
		go func() {
			defer close(s.stopped)
			defer s.unlockState()
			if err := s.flush(true); err != nil {
				s.log.Error("final save failed", "err", err)
			}
			stateMu.Lock()
			s.sealed = true
			stateMu.Unlock()
			s.mu.Lock()
			var all []*rec
			for _, r := range s.blocks {
				r.closing = true
				all = append(all, r)
			}
			s.mu.Unlock()
			var wg sync.WaitGroup
			for _, r := range all {
				wg.Go(func() { r.b.Close() })
			}
			wg.Wait()
			if s.srv != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = s.srv.Shutdown(ctx)
				cancel()
			}
			s.attachMu.Lock()
			for c := range s.attaches {
				c.cancel()
				c.ws.CloseNow()
			}
			s.attachMu.Unlock()
			_ = os.Remove(s.cfg.Socket)
			s.stopRemote()
		}()
	})
}

// Done is closed once the server has stopped.
func (s *Server) Done() <-chan struct{} { return s.stopped }

// ---- blocks ----

// LayoutSpec describes blocks to create: one block, or a split of two specs.
type LayoutSpec struct {
	Type      string      `json:"type,omitempty" enum:"block,split" desc:"block (default) or split"`
	Direction string      `json:"direction,omitempty" enum:"horizontal,vertical" desc:"For a split: horizontal puts a left of b, vertical puts a above b."`
	Ratio     float64     `json:"ratio,omitempty" desc:"For a split: a's share, 0 to 1. Default 0.5."`
	A         *LayoutSpec `json:"a,omitempty"`
	B         *LayoutSpec `json:"b,omitempty"`
	Command   []string    `json:"command,omitempty" desc:"Program and arguments. Default: the login shell."`
	Cwd       string      `json:"cwd,omitempty" desc:"Absolute directory, or ~/… Default: the server account's home."`
	Label     string      `json:"label,omitempty"`
	KeepOpen  bool        `json:"keep_open,omitempty" desc:"Keep the block after its program exits."`
}

func (s *Server) env(sessionID, blockID string) []string {
	env := []string{"HIVE_SERVER=" + s.cfg.Socket, "HIVE_SESSION=" + sessionID, "HIVE_BLOCK=" + blockID,
		"TERM_PROGRAM_VERSION=" + Version}
	if s.cfg.HivePath != "" {
		dir := filepath.Dir(s.cfg.HivePath)
		env = append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return env
}

// startBlock launches a block. Call with s.mu held.
func (s *Server) startBlock(sessionID, blockID string, st *persist.BlockState, opts block.Options) (*rec, error) {
	opts.Env = append(opts.Env, s.env(sessionID, blockID)...)
	if opts.Cwd == "" {
		// The account's home, not the server's own cwd (the daemon runs
		// in /): a remote client has no path of its own to give.
		opts.Cwd = "/"
		if h, err := os.UserHomeDir(); err == nil {
			if fi, err := os.Stat(h); err == nil && fi.IsDir() {
				opts.Cwd = h
			}
		}
	}
	if opts.Cwd != "" {
		if fi, err := os.Stat(opts.Cwd); err != nil || !fi.IsDir() {
			return nil, api.BadRequest("cwd %q is not a directory", opts.Cwd)
		}
	}
	opts.ScrollbackBytes = s.conf.ScrollbackLimit
	b, err := block.Start(blockID, opts, func(e block.Event) { s.onBlockEvent(sessionID, e) })
	if err != nil {
		return nil, api.BadRequest("start %v: %v", opts.Command, err)
	}
	st.SessionID, st.Cols, st.Rows = sessionID, opts.Cols, opts.Rows
	if st.Created.IsZero() {
		st.Created = time.Now().UTC()
	}
	r := &rec{b: b, st: st}
	s.blocks[blockID] = r
	s.bus.Session(sessionID, "block_created", map[string]any{"block_id": blockID})
	return r, nil
}

// buildLayout creates the blocks of spec, sized to fit cols x rows. On error
// the blocks it created are returned for the caller to close.
func (s *Server) buildLayout(sessionID string, spec *LayoutSpec, cols, rows uint16) (*session.Node, []string, []string, error) {
	if spec == nil {
		spec = &LayoutSpec{}
	}
	if spec.Type == "split" {
		if spec.A == nil || spec.B == nil {
			return nil, nil, nil, api.BadRequest("a split needs both a and b")
		}
		ratio := spec.Ratio
		if ratio <= 0 || ratio >= 1 {
			ratio = 0.5
		}
		dir := spec.Direction
		if dir == "" {
			dir = "horizontal"
		}
		ac, ar, bc, br := splitSize(dir, ratio, cols, rows)
		a, ab, as, err := s.buildLayout(sessionID, spec.A, ac, ar)
		if err != nil {
			return nil, ab, nil, err
		}
		b, bb, bs, err := s.buildLayout(sessionID, spec.B, bc, br)
		blocks := append(ab, bb...)
		if err != nil {
			return nil, blocks, nil, err
		}
		sid := id.New(id.Split)
		return &session.Node{Type: "split", SplitID: sid, Direction: dir, Ratio: ratio, A: a, B: b},
			blocks, append(append([]string{sid}, as...), bs...), nil
	}
	bid := id.New(id.Block)
	st := &persist.BlockState{Label: spec.Label, Command: spec.Command, Cwd: spec.Cwd, KeepOpen: spec.KeepOpen}
	if _, err := s.startBlock(sessionID, bid, st, block.Options{
		Command: spec.Command, Cwd: expandHome(spec.Cwd), KeepOpen: spec.KeepOpen, Cols: cols, Rows: rows,
	}); err != nil {
		return nil, nil, nil, err
	}
	return &session.Node{Type: "block", BlockID: bid}, []string{bid}, nil, nil
}

func splitSize(dir string, ratio float64, cols, rows uint16) (ac, ar, bc, br uint16) {
	clamp := func(v float64) uint16 { return uint16(max(1, v)) }
	if dir == "horizontal" {
		a := clamp(float64(cols)*ratio - 0.5)
		return a, rows, clamp(float64(cols) - float64(a) - 1), rows
	}
	a := clamp(float64(rows)*ratio - 0.5)
	return cols, a, cols, clamp(float64(rows) - float64(a) - 1)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return p
}

// abandon closes blocks that were started but never placed.
func (s *Server) abandon(ids []string) {
	var rs []*rec
	for _, bid := range ids {
		if r := s.blocks[bid]; r != nil {
			r.closing = true
			rs = append(rs, r)
			delete(s.blocks, bid)
		}
	}
	go func() {
		for _, r := range rs {
			r.b.Close()
		}
	}()
}

func (s *Server) onBlockEvent(sessionID string, e block.Event) {
	s.mu.Lock()
	r := s.blocks[e.BlockID]
	if r == nil || (r.closing && e.Name != "child_exited") {
		s.mu.Unlock()
		return
	}
	closing := r.closing
	switch e.Name {
	case "title_changed":
		r.st.Title, _ = e.Payload["title"].(string)
	case "pwd_changed":
		r.st.Pwd, _ = e.Payload["pwd"].(string)
	case "process_changed":
		if p, ok := e.Payload["foreground"].(block.Process); ok {
			r.st.Foreground = &p
			// The shell is back in front: the agent exited, maybe killed
			// before its SessionEnd hook. Its tools never take the terminal
			// (they run without one), so this means it's gone. A report from
			// it later brings it back.
			if a := r.st.Agent; a != nil && a.State != "ended" && p.AtShell() {
				a.Before, a.EndedBy = a.State, "shell"
				a.State, a.Message, a.UpdatedAt = "ended", "", time.Now().UTC()
				s.bus.Block(r.st.SessionID, e.BlockID, "agent_changed", map[string]any{
					"harness": a.Harness, "session_id": a.SessionID, "state": a.State, "message": "", "event": "shell_returned",
				})
			} else if a != nil && a.EndedBy == "shell" && p.Program == a.Harness {
				// The same agent is in front again: a tool's interactive shell
				// had the terminal, or the agent was suspended and resumed.
				a.State, a.EndedBy, a.UpdatedAt = a.Before, "", time.Now().UTC()
				if a.State == "" {
					a.State = "idle"
				}
				s.bus.Block(r.st.SessionID, e.BlockID, "agent_changed", map[string]any{
					"harness": a.Harness, "session_id": a.SessionID, "state": a.State, "message": a.Message, "event": "agent_returned",
				})
			}
		}
	case "program_status_changed":
		if rec, ok := e.Payload["record"].(vt.ProgramStatus); ok {
			if r.st.Status == nil {
				r.st.Status = map[string]vt.ProgramStatus{}
			}
			key := rec.ID
			if rec.State == "clear" {
				for k := range r.st.Status {
					if key == "" || k == key || strings.HasPrefix(k, key+"/") {
						delete(r.st.Status, k)
					}
				}
			} else {
				r.st.Status[key] = rec
			}
			// The agent's own report of its state wins over the last hook:
			// an interrupt runs no hook, but Claude Code reports going idle.
			if a := r.st.Agent; a != nil && a.State != "ended" {
				if st, ok := agents.StateFromStatus(a.Harness, rec.App, rec.State); ok && st != a.State {
					a.State, a.Message, a.UpdatedAt = st, rec.Message, time.Now().UTC()
					s.bus.Block(r.st.SessionID, e.BlockID, "agent_changed", map[string]any{
						"harness": a.Harness, "session_id": a.SessionID, "state": a.State, "message": a.Message, "event": "program_status",
					})
				}
			}
		}
	case "prompt":
		// A new prompt ends working and blocked records (OSC 7501 lifetime).
		if e.Payload["kind"] == "prompt_start" {
			for k, v := range r.st.Status {
				if v.State == "working" || v.State == "blocked" {
					delete(r.st.Status, k)
				}
			}
		}
	case "child_exited":
		r.st.Exited = true
		if c, ok := e.Payload["exit_code"].(int); ok {
			r.st.ExitCode = c
		}
		s.rememberExit(e.BlockID, r.st.ExitCode)
		for k, v := range r.st.Status {
			if v.State == "working" || v.State == "blocked" {
				delete(r.st.Status, k)
			}
		}
	}
	keepOpen := r.st.KeepOpen
	s.bus.Block(sessionID, e.BlockID, e.Name, e.Payload)
	s.mu.Unlock()
	s.markDirty()

	if e.Name == "child_exited" && !closing {
		s.snapshotBlock(e.BlockID)
		if !keepOpen {
			go s.closeBlock(e.BlockID)
		}
	}
}

// closeBlock removes a block from its layout and ends it. The block stays
// known until its program is gone, so its child_exited event is published.
func (s *Server) closeBlock(blockID string) error {
	s.mu.Lock()
	r := s.blocks[blockID]
	if r == nil || r.closing {
		s.mu.Unlock()
		return api.NotFound("no block %s", blockID)
	}
	r.closing = true
	s.detachFromLayout(r.st.SessionID, blockID)
	s.mu.Unlock()
	r.b.Close()
	<-r.b.EventsDone()
	s.mu.Lock()
	delete(s.blocks, blockID)
	s.mu.Unlock()
	s.store.RemoveSnapshot(blockID)
	s.markDirty()
	return nil
}

// rememberExit records a block's exit status. Call with s.mu held.
func (s *Server) rememberExit(blockID string, code int) {
	s.exits[blockID] = exitRec{code: code, at: time.Now()}
	if len(s.exits) > 1024 {
		for bid, e := range s.exits {
			if time.Since(e.at) > 10*time.Minute {
				delete(s.exits, bid)
			}
		}
	}
}

// detachFromLayout takes a block out of its window, closing the window and
// the session when they are left empty. Call with s.mu held.
func (s *Server) detachFromLayout(sessionID, blockID string) {
	ss := s.session(sessionID)
	if ss == nil {
		return
	}
	s.bus.Session(sessionID, "block_closed", map[string]any{"block_id": blockID})
	w := ss.WindowOf(blockID)
	if w == nil {
		return
	}
	if w.Remove(blockID) {
		ss.RemoveWindow(w.ID)
		s.bus.Session(sessionID, "window_closed", map[string]any{"window_id": w.ID})
	}
	s.changed(ss)
	if len(ss.Windows) == 0 {
		s.removeSession(sessionID)
	}
}

func (s *Server) removeSession(sessionID string) {
	for i, ss := range s.sessions {
		if ss.ID == sessionID {
			s.sessions = append(s.sessions[:i], s.sessions[i+1:]...)
			s.bus.Server("session_destroyed", map[string]any{"session_id": sessionID})
			return
		}
	}
}

func (s *Server) session(sessionID string) *session.Session {
	for _, ss := range s.sessions {
		if ss.ID == sessionID {
			return ss
		}
	}
	return nil
}

// changed bumps a session's revision. Call with s.mu held.
func (s *Server) changed(ss *session.Session) {
	ss.Revision++
	s.bus.Session(ss.ID, "session_view_changed", map[string]any{"revision": ss.Revision})
	s.markDirty()
}

// ---- persistence ----

func (s *Server) markDirty() {
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

// saveStateSoon writes state.json right away, in the background: callers
// such as agent.report don't wait for the disk. Requests made during a save
// coalesce into one more save.
func (s *Server) saveStateSoon() {
	select {
	case s.stateSave <- struct{}{}:
	default:
	}
}

func (s *Server) stateLoop() {
	for {
		select {
		case <-s.quit:
			return
		case <-s.stateSave:
			if err := s.flushState(); err != nil {
				s.log.Error("save failed", "err", err)
			}
		}
	}
}

func (s *Server) persistLoop() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	var timer <-chan time.Time
	for {
		select {
		case <-s.quit:
			return
		case <-s.dirty:
			if timer == nil {
				timer = time.After(s.cfg.FlushDelay)
			}
		case <-timer:
			timer = nil
			if err := s.flush(false); err != nil {
				s.log.Error("save failed", "err", err)
			}
		case <-tick.C:
			if err := s.flush(false); err != nil {
				s.log.Error("save failed", "err", err)
			}
		case done := <-s.flushReq:
			done <- s.flush(false)
		}
	}
}

// Saves take two locks. snapMu serializes snapshot writes and the GC of
// snapshot files, so a GC never removes a snapshot's temp file mid-write.
// stateMu serializes state.json. agent.report takes only stateMu: with many
// full panes the snapshots take seconds, and a report mustn't wait for them.
var snapMu, stateMu sync.Mutex

// flush snapshots every block whose output changed and writes state.json.
// With all, every block is snapshotted.
func (s *Server) flush(all bool) error { return s.save(all, true) }

// flushState saves sessions, layouts and agents, but no terminal snapshots:
// for agent.report (in the background, see saveStateSoon), while snapshots
// (scrollback) can wait for the next timed save.
func (s *Server) flushState() error { return s.save(false, false) }

func (s *Server) save(all, snapshots bool) error {
	start := time.Now()
	var wait time.Duration
	snaps := 0
	if s.cfg.OnFlush != nil {
		defer func() { s.cfg.OnFlush(FlushStats{Took: time.Since(start), Wait: wait, Snapshots: snaps}) }()
	}
	if snapshots {
		snapMu.Lock()
		defer snapMu.Unlock()
		wait = time.Since(start)
		snaps = s.saveSnapshots(all)
	}
	// The state is read after the snapshots, never before: a report saved
	// meanwhile must not be overwritten by older state.
	t := time.Now()
	stateMu.Lock()
	wait += time.Since(t)
	if s.sealed {
		stateMu.Unlock()
		return nil
	}
	if s.cfg.beforeStateSave != nil {
		s.cfg.beforeStateSave()
	}
	live, err := s.saveState()
	stateMu.Unlock()
	if err != nil {
		return err
	}
	if snapshots {
		s.store.GC(live)
	}
	return nil
}

// saveSnapshots writes the snapshot of every block whose output changed
// (every block, with all), under snapMu, and says how many it wrote.
func (s *Server) saveSnapshots(all bool) int {
	type snapJob struct {
		id  string
		r   *rec
		seq uint64
	}
	var jobs []snapJob
	s.mu.Lock()
	for bid, r := range s.blocks {
		if r.closing {
			continue
		}
		if seq := r.b.Seq(); all || seq != r.snapSeq {
			jobs = append(jobs, snapJob{bid, r, seq})
		}
	}
	s.mu.Unlock()
	n := 0
	for _, j := range jobs {
		if s.cfg.beforeSnapshot != nil {
			s.cfg.beforeSnapshot(j.id)
		}
		if err := s.store.SaveSnapshot(j.id, j.r.b.SnapshotTo); err != nil {
			s.log.Warn("snapshot failed", "block", j.id, "err", err)
			continue
		}
		s.mu.Lock()
		j.r.snapSeq = j.seq
		s.mu.Unlock()
		n++
	}
	return n
}

// saveState writes state.json, under stateMu, and returns its blocks.
func (s *Server) saveState() (map[string]bool, error) {
	s.mu.Lock()
	sessions, err := json.Marshal(s.sessions)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	// Block states are encoded while s.mu is held: events and agent reports
	// change their maps and pointers in place.
	states := map[string]*persist.BlockState{}
	for bid, r := range s.blocks {
		if r.closing {
			continue
		}
		r.st.Cols, r.st.Rows = r.b.Size()
		states[bid] = r.st
	}
	blocksJSON, err := json.Marshal(states)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	st := &persist.State{}
	if err := json.Unmarshal(sessions, &st.Sessions); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(blocksJSON, &st.Blocks); err != nil {
		return nil, err
	}
	if err := s.store.SaveState(st); err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for bid := range st.Blocks {
		live[bid] = true
	}
	return live, nil
}

// Flush writes state now and waits for it.
func (s *Server) Flush() error { return s.flush(false) }

func (s *Server) snapshotBlock(blockID string) {
	// Under snapMu, so a flush's GC never removes this snapshot's temp file.
	snapMu.Lock()
	defer snapMu.Unlock()
	s.mu.Lock()
	r := s.blocks[blockID]
	s.mu.Unlock()
	if r == nil {
		return
	}
	seq := r.b.Seq()
	if err := s.store.SaveSnapshot(blockID, r.b.SnapshotTo); err == nil {
		s.mu.Lock()
		r.snapSeq = seq
		s.mu.Unlock()
	}
}

// restorePreamble returns a restored terminal to a sane state for a new
// program and marks where the old output ends.
func restorePreamble(was string, at time.Time) []byte {
	var b strings.Builder
	// The block leaves the alternate screen first, if it was on it.
	b.WriteString("\x1b[!p") // soft reset: modes, margins, SGR
	b.WriteString("\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2004l\x1b[=0;1u")
	b.WriteString("\r\n\x1b[2m── restored " + at.Local().Format("2006-01-02 15:04"))
	if was != "" {
		b.WriteString(" (was " + was + ")")
	}
	b.WriteString(" ──\x1b[0m\r\n")
	return []byte(b.String())
}

// restoreCwd picks the directory a restored block starts in.
func restoreCwd(st *persist.BlockState) string {
	isDir := func(p string) bool {
		fi, err := os.Stat(p)
		return err == nil && fi.IsDir()
	}
	var cands []string
	if st.Agent != nil {
		cands = append(cands, st.Agent.Cwd)
	}
	if st.Pwd != "" {
		// OSC 7 names a host; a directory on another machine (an ssh
		// session) is no use here.
		if u, err := url.Parse(st.Pwd); err == nil && u.Scheme == "file" && localHost(u.Host) {
			cands = append(cands, u.Path)
		}
	}
	if st.Foreground != nil {
		cands = append(cands, st.Foreground.Cwd)
	}
	cands = append(cands, expandHome(st.Cwd))
	for _, c := range cands {
		if c != "" && isDir(c) {
			return c
		}
	}
	home, _ := os.UserHomeDir()
	return home
}

// restoreCommand picks what a restored block runs: the agent's resume
// command, or a shell. Other programs are not re-run. An agent whose pane
// was back at a shell had exited without saying so (killed before its
// SessionEnd hook): resuming it would reopen a session the user closed.
func restoreCommand(st *persist.BlockState, resume func(string, string) []string) (argv []string, was string) {
	if a := st.Agent; a != nil && a.State != "ended" && (st.Foreground == nil || !st.Foreground.AtShell()) {
		if argv := resume(a.Harness, a.SessionID); argv != nil {
			return argv, a.Harness + " " + a.SessionID
		}
	}
	if len(st.Command) > 0 {
		return nil, strings.Join(st.Command, " ")
	}
	return nil, ""
}

func (s *Server) restore() error {
	saved, err := s.store.LoadState()
	if err != nil || saved == nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	at := saved.SavedAt
	for _, ss := range saved.Sessions {
		for _, w := range append([]*session.Window(nil), ss.Windows...) {
			for _, bid := range w.Blocks() {
				st := saved.Blocks[bid]
				if st == nil {
					st = &persist.BlockState{}
				}
				argv, was := restoreCommand(st, s.cfg.ResumeArgv)
				snap, _ := s.store.LoadSnapshot(bid)
				opts := block.Options{
					Command: argv, Cwd: restoreCwd(st), Cols: max(st.Cols, 2), Rows: max(st.Rows, 2),
					Restore: snap, Preamble: restorePreamble(was, at), KeepOpen: st.KeepOpen,
				}
				// A resumed agent keeps its pane even if the resume fails, so the
				// error stays visible instead of the layout collapsing.
				if argv != nil {
					opts.KeepOpen, st.KeepOpen = true, true
				}
				// The new program is a shell or an agent, never the old command.
				st.Command, st.Exited, st.ExitCode = argv, false, 0
				st.Status = nil
				if st.Agent != nil && st.Agent.State != "ended" {
					if argv == nil {
						st.Agent.State = "ended" // not resumed: it had exited
					} else {
						st.Agent.State = "idle"
					}
				}
				if _, err := s.startBlock(ss.ID, bid, st, opts); err != nil {
					s.log.Warn("could not restore block", "block", bid, "err", err)
					if w.Remove(bid) {
						ss.RemoveWindow(w.ID)
					}
				}
			}
		}
		if len(ss.Windows) > 0 {
			s.sessions = append(s.sessions, ss)
		}
	}
	s.log.Info("restored", "sessions", len(s.sessions), "blocks", len(s.blocks))
	s.markDirty()
	return nil
}

// ---- helpers for methods ----

func (s *Server) sessionItems() []session.Item {
	items := make([]session.Item, len(s.sessions))
	for i, ss := range s.sessions {
		items[i] = session.Item{ID: ss.ID, Label: ss.Label}
	}
	return items
}

func (s *Server) needSession(sessionID string) (*session.Session, error) {
	ss := s.session(sessionID)
	if ss == nil {
		return nil, api.NotFound("no session %s", sessionID)
	}
	return ss, nil
}

func (s *Server) needBlock(blockID string) (*rec, error) {
	r := s.blocks[blockID]
	if r == nil || r.closing {
		return nil, api.NotFound("no block %s", blockID)
	}
	return r, nil
}

func (s *Server) blockInfo(bid string, r *rec) map[string]any {
	cols, rows := r.b.Size()
	code, exited := r.b.ExitStatus()
	info := map[string]any{
		"block_id": bid, "session_id": r.st.SessionID, "label": r.st.Label,
		"creator_name": events.Creator, "cols": cols, "rows": rows,
		"title": r.st.Title, "pwd": r.st.Pwd, "cwd": r.st.Cwd, "command": r.st.Command,
		"keep_open": r.st.KeepOpen, "exited": exited, "created_at": r.st.Created,
	}
	if exited {
		info["exit_code"] = code
	}
	if r.st.Foreground != nil {
		info["foreground"] = r.st.Foreground
	}
	if r.st.Agent != nil {
		info["agent"] = r.st.Agent
	}
	if len(r.st.Status) > 0 {
		info["program_status"] = sortedStatus(r.st.Status)
	}
	if r.owner != "" {
		info["size_owner"] = r.owner
	}
	if ss := s.session(r.st.SessionID); ss != nil {
		if w := ss.WindowOf(bid); w != nil {
			info["window_id"] = w.ID
			info["rect"] = w.Rects()[bid]
			info["focused"] = w.Focus == bid
		}
	}
	return info
}

func sortedStatus(m map[string]vt.ProgramStatus) []vt.ProgramStatus {
	out := make([]vt.ProgramStatus, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func localHost(h string) bool {
	if h == "" || h == "localhost" {
		return true
	}
	name, _ := os.Hostname()
	short, _, _ := strings.Cut(name, ".")
	hs, _, _ := strings.Cut(h, ".")
	return strings.EqualFold(h, name) || strings.EqualFold(hs, short)
}

// copyJSON returns a JSON copy of v, so a result built under s.mu can be
// encoded after the lock is released.
func copyJSON(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	return json.RawMessage(b), err
}

func platform() string { return runtime.GOOS + "_" + runtime.GOARCH }

// loadServerID reads the state directory's server-id, making one the first
// time. Without a writable state directory it lasts until the server stops.
func loadServerID(dir string, log *slog.Logger) string {
	p := filepath.Join(dir, "server-id")
	if b, err := os.ReadFile(p); err == nil {
		if v := strings.TrimSpace(string(b)); len(v) >= 16 {
			return v
		}
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	v := hex.EncodeToString(buf)
	if err := os.MkdirAll(dir, 0o700); err == nil {
		err = os.WriteFile(p, []byte(v+"\n"), 0o600)
		if err != nil {
			log.Warn("server-id not saved", "err", err)
		}
	}
	return v
}

// stateLockWait is how long a new server waits for the state directory.
var stateLockWait = 3 * time.Second

// lockStateDir holds the state directory for this server, or says which
// server already does.
func lockStateDir(dir string) (func(), error) {
	if dir == "" {
		return func() {}, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "server.lock")
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	// A server stopping lets go as it exits: `hive server stop` then start
	// may come a moment before that.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	for end := time.Now().Add(stateLockWait); err != nil && time.Now().Before(end); {
		time.Sleep(50 * time.Millisecond)
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	}
	if err != nil {
		b, _ := os.ReadFile(p)
		f.Close()
		who := strings.TrimSpace(string(b))
		if who == "" {
			who = "unknown"
		}
		return nil, fmt.Errorf("another hive server (pid %s) uses %s: stop it first, or give this one its own HIVE_STATE_DIR", who, dir)
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(fmt.Sprintf("%d\n", os.Getpid())), 0)
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
