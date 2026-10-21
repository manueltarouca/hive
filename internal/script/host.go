package script

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/events"
	"github.com/manueltarouca/hive/internal/remote"
)

// Backend is what the host needs from the server.
type Backend interface {
	Registry() *api.Registry
	Bus() *events.Bus
	// BlockSession is the session a block is in.
	BlockSession(blockID string) (string, bool)
}

// Config for a Host.
type Config struct {
	Backend  Backend
	StateDir string // audit log and, later, scripts' storage
	Logger   *slog.Logger
}

// Host runs scripts.
type Host struct {
	cfg   Config
	log   *slog.Logger
	audit *auditLog

	mu       sync.Mutex
	runs     map[string]*Run
	finished []*Run // the latest, for script.result and script.list
	closed   bool
}

// keepFinished is how many ended runs are remembered.
const keepFinished = 64

// New makes a host.
func New(cfg Config) *Host {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	h := &Host{cfg: cfg, log: log, runs: map[string]*Run{}}
	if cfg.StateDir != "" {
		h.audit = &auditLog{path: filepath.Join(cfg.StateDir, "scripts", "audit.jsonl")}
	}
	return h
}

// Context is what a script runs in: the pane and session it started from.
type Context struct {
	SessionID string `json:"session_id,omitempty"`
	BlockID   string `json:"block_id,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
}

// RunOptions starts a one-shot.
type RunOptions struct {
	Name string
	// JS is a bundle (see Bundle).
	JS   string
	Args json.RawMessage
	// Perms the run starts with; a hive.script manifest replaces them,
	// within Limit.
	Perms Perms
	// Limit caps what the run may ever hold: the caller's own permissions
	// when a script starts another. Nil is no cap.
	Limit    Perms
	Context  Context
	Identity remote.Identity
	// For ends the run after this long (0: no limit).
	For time.Duration
	// All lets the run hear every session's events, not only its own.
	All bool
	// Budget caps how long one piece of JS may run before it is
	// interrupted (0: no cap).
	Budget time.Duration
}

func newRunID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "run:" + hex.EncodeToString(b)
}

// Start runs a one-shot. It returns once the bundle is loaded, or with the
// error that stopped it from loading.
func (h *Host) Start(o RunOptions) (*Run, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, api.Conflict("the server is stopping")
	}
	h.mu.Unlock()
	if o.Name == "" {
		o.Name = "script"
	}
	r := newRun(h, o)
	if err := r.start(); err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.runs[r.ID] = r
	h.mu.Unlock()
	h.cfg.Backend.Bus().Scoped("script_started", map[string]any{"run_id": r.ID, "name": r.Name})
	go r.loop()
	return r, nil
}

// Stop ends a run.
func (h *Host) Stop(runID string) error {
	h.mu.Lock()
	r := h.runs[runID]
	h.mu.Unlock()
	if r == nil {
		return api.NotFound("no running script %s", runID)
	}
	r.stop("stopped")
	return nil
}

// Wait waits for a run to end.
func (h *Host) Wait(ctx context.Context, runID string) (RunInfo, error) {
	h.mu.Lock()
	r := h.runs[runID]
	if r == nil {
		for _, f := range h.finished {
			if f.ID == runID {
				r = f
			}
		}
	}
	h.mu.Unlock()
	if r == nil {
		return RunInfo{}, api.NotFound("no script run %s", runID)
	}
	select {
	case <-r.done:
		return r.Info(true), nil
	case <-ctx.Done():
		return RunInfo{}, ctx.Err()
	}
}

// List describes running and recently ended runs.
func (h *Host) List() []RunInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []RunInfo
	for _, r := range h.runs {
		out = append(out, r.Info(false))
	}
	for _, r := range h.finished {
		out = append(out, r.Info(false))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

// Close stops every run, for the server's shutdown.
func (h *Host) Close() {
	h.mu.Lock()
	h.closed = true
	var rs []*Run
	for _, r := range h.runs {
		rs = append(rs, r)
	}
	h.mu.Unlock()
	for _, r := range rs {
		r.stop("the server stopped")
	}
	for _, r := range rs {
		select {
		case <-r.done:
		case <-time.After(2 * time.Second):
			h.log.Warn("script did not stop", "run", r.ID)
		}
	}
}

func (h *Host) ended(r *Run) {
	h.mu.Lock()
	delete(h.runs, r.ID)
	h.finished = append(h.finished, r)
	if len(h.finished) > keepFinished {
		h.finished = h.finished[len(h.finished)-keepFinished:]
	}
	h.mu.Unlock()
}

// maxPayload caps a call's arguments and result.
const maxPayload = 4 << 20

// call runs one method for a script: fill IDs, check the permission, call,
// audit.
func (h *Host) call(r *Run, method, argsJSON string) (string, error) {
	reg := h.cfg.Backend.Registry()
	if len(argsJSON) > maxPayload {
		return "", api.BadRequest("arguments over %d bytes", maxPayload)
	}
	m, ok := reg.Describe(method)
	if !ok {
		return "", &api.Error{Status: 404, Code: "method_not_found", Message: fmt.Sprintf("no method named %q", method)}
	}
	raw, err := h.fill(r, m, []byte(argsJSON))
	if err != nil {
		return "", err
	}
	need, err := reg.PermissionOf(method, raw)
	if err != nil {
		return "", err
	}
	perms := r.permsNow()
	if !perms.Has(need) {
		return "", api.PermissionDenied("%s needs the %s permission, which this script doesn't have", method, need)
	}
	ctx := WithPerms(r.ctx, perms)
	out, err := reg.Call(ctx, method, raw)
	if audited[need] {
		res := "ok"
		if err != nil {
			res = errCode(err)
		}
		h.audit.write(Entry{Time: time.Now().UTC(), Principal: "script:" + r.Name, OnBehalfOf: r.identity.Principal,
			RunID: r.ID, Method: method, Permission: need, Args: auditArgs(raw), Result: res})
	}
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", api.Internal("result: %v", err)
	}
	if len(b) > maxPayload {
		return "", api.BadRequest("result over %d bytes", maxPayload)
	}
	if string(b) == "null" {
		return "", nil
	}
	return string(b), nil
}

// fill adds IDs a call left out, from the block it names or the script's
// context: session_id from block_id's own session, else from the context;
// block_id from the context. agent.report is never filled: its session_id
// is the agent's own.
func (h *Host) fill(r *Run, m *api.Method, raw []byte) ([]byte, error) {
	if m.Name == "agent.report" {
		return raw, nil
	}
	props, _ := m.InputSchema["properties"].(map[string]any)
	_, wantsSession := props["session_id"]
	_, wantsBlock := props["block_id"]
	if !wantsSession && !wantsBlock {
		return raw, nil
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil || args == nil {
		return raw, nil // the registry reports it
	}
	empty := func(k string) bool { v, ok := args[k]; return !ok || v == "" || v == nil }
	changed := false
	if wantsBlock && empty("block_id") && r.cx.BlockID != "" {
		args["block_id"], changed = r.cx.BlockID, true
	}
	if wantsSession && empty("session_id") {
		sid := r.cx.SessionID
		if b, ok := args["block_id"].(string); ok && b != "" {
			if s, ok := h.cfg.Backend.BlockSession(b); ok {
				sid = s
			}
		}
		if sid != "" {
			args["session_id"], changed = sid, true
		}
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(args)
}

func errCode(err error) string {
	var e *api.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "internal"
}

// asAPI makes any error an api.Error for a script.
func asAPI(err error) *api.Error {
	var e *api.Error
	if errors.As(err, &e) {
		return e
	}
	if errors.Is(err, context.Canceled) {
		return api.ScriptUnloaded("the script stopped")
	}
	return api.Internal("%v", err)
}
