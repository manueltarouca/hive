package script

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/manueltarouca/hive/internal/api"
)

// Resident scripts live in a folder (~/.config/hive/scripts): NAME.ts,
// NAME.js or NAME/index.ts. The server loads them at start, reloads one
// when its files change, and keeps them running until they're removed,
// disabled or quarantined. lib/ holds shared modules, never loaded alone.

// Script states, as script.list shows them.
const (
	StateRunning     = "running"
	StatePending     = "pending"     // waiting for hive script grant
	StateFailed      = "failed"      // didn't load, or setup threw
	StateDisabled    = "disabled"    // hive script disable
	StateQuarantined = "quarantined" // stopped for overrunning its budget
	StateStopped     = "stopped"     // it ended on its own (h.stop)
)

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Resident limits.
const (
	handlerBudget   = 5 * time.Second
	overrunLimit    = 5
	overrunWindow   = 5 * time.Minute
	stableChecks    = 2 // a change must hold this many polls before reloading
	defaultPollTime = 2 * time.Second
)

// Resident is one script from the folder.
type Resident struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	State       string    `json:"state"`
	Permissions []string  `json:"permissions"`
	Asks        []string  `json:"asks,omitempty" desc:"For a pending script: the permissions its manifest asks for."`
	Description string    `json:"description,omitempty"`
	RunID       string    `json:"run_id,omitempty"`
	Error       string    `json:"error,omitempty"`
	Errors      int       `json:"errors" desc:"Errors its handlers threw since it loaded."`
	Overruns    int       `json:"overruns" desc:"Handlers stopped for running over their budget, in the last 5 minutes."`
	LoadedAt    time.Time `json:"loaded_at,omitzero"`
	// Mode is act, or watch: it may look and tell, and every change it
	// tries is refused.
	Mode    string   `json:"mode" enum:"act,watch"`
	Options []Option `json:"options" desc:"Its switches: what it declares, with the values set."`

	life     *sync.Mutex // one unload-and-start at a time
	sig      string      // the files' sizes and times when last loaded
	pending  string      // a newer signature waiting to hold still
	seen     int
	run      *Run
	overruns []time.Time
	bundle   string
}

type grant struct {
	Permissions []string  `json:"permissions"`
	Hash        string    `json:"hash"`
	At          time.Time `json:"granted_at"`
}

// residents is the host's part that manages the folder.
type residents struct {
	h    *Host
	dir  string
	mu   sync.Mutex
	all  map[string]*Resident
	poll time.Duration
	quit chan struct{}
	off  bool
}

func (rs *residents) budget() time.Duration {
	if b := rs.h.cfg.HandlerBudget; b > 0 {
		return b
	}
	return handlerBudget
}

func permsHash(list []string) string {
	l := append([]string(nil), list...)
	sort.Strings(l)
	sum := sha256.Sum256([]byte(strings.Join(l, ",")))
	return hex.EncodeToString(sum[:8])
}

func (rs *residents) grantsPath() string {
	return filepath.Join(rs.h.cfg.StateDir, "scripts", "grants.json")
}

func (rs *residents) disabledPath() string {
	return filepath.Join(rs.h.cfg.StateDir, "scripts", "disabled.json")
}

func (rs *residents) grants() map[string]grant {
	g := map[string]grant{}
	if b, err := os.ReadFile(rs.grantsPath()); err == nil {
		_ = json.Unmarshal(b, &g)
	}
	return g
}

func (rs *residents) disabled() map[string]bool {
	d := map[string]bool{}
	if b, err := os.ReadFile(rs.disabledPath()); err == nil {
		_ = json.Unmarshal(b, &d)
	}
	return d
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// entries finds the scripts in the folder: name to entry file.
func (rs *residents) entries() map[string]string {
	out := map[string]string{}
	items, err := os.ReadDir(rs.dir)
	if err != nil {
		return out
	}
	for _, it := range items {
		name := it.Name()
		if strings.HasPrefix(name, ".") || name == "lib" || name == "node_modules" {
			continue
		}
		full := filepath.Join(rs.dir, name)
		if st, err := os.Stat(full); err == nil && st.IsDir() {
			for _, idx := range []string{"index.ts", "index.js"} {
				if _, err := os.Stat(filepath.Join(full, idx)); err == nil {
					out[name] = filepath.Join(full, idx)
					break
				}
			}
			continue
		}
		base, ext, _ := strings.Cut(name, ".")
		if (ext == "ts" || ext == "js") && !strings.HasSuffix(name, ".d.ts") {
			out[base] = full
		}
	}
	return out
}

// signature changes when any file a script could import changes: its own
// files and lib/.
func (rs *residents) signature(entry string) string {
	var b strings.Builder
	walk := func(root string) {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, err := d.Info(); err == nil {
				fmt.Fprintf(&b, "%s %d %d\n", p, info.Size(), info.ModTime().UnixNano())
			}
			return nil
		})
	}
	if filepath.Base(entry) == "index.ts" || filepath.Base(entry) == "index.js" {
		walk(filepath.Dir(entry))
	} else {
		walk(entry)
	}
	walk(filepath.Join(rs.dir, "lib"))
	return b.String()
}

// start loads every script and watches the folder.
func (rs *residents) start() {
	rs.check(true)
	go func() {
		t := time.NewTicker(rs.poll)
		defer t.Stop()
		for {
			select {
			case <-rs.quit:
				return
			case <-t.C:
				rs.check(false)
			}
		}
	}()
}

// check compares the folder with what runs: new scripts load, changed
// ones reload once their files hold still, removed ones unload.
func (rs *residents) check(first bool) {
	entries := rs.entries()
	rs.mu.Lock()
	if rs.off {
		rs.mu.Unlock()
		return
	}
	var load []*Resident
	var unload []*Resident
	for name, entry := range entries {
		if !nameRE.MatchString(name) {
			continue
		}
		sig := rs.signature(entry)
		r := rs.all[name]
		if r == nil {
			r = &Resident{Name: name, Path: entry, State: StateFailed, Permissions: []string{}, Options: []Option{},
				Mode: ModeAct, life: &sync.Mutex{}}
			if m := rs.modes()[name]; m == ModeWatch {
				r.Mode = m
			}
			rs.all[name] = r
			r.pending, r.seen = sig, stableChecks
			if !first {
				r.seen = 0 // a new file must hold still too
			}
		}
		r.Path = entry
		if sig == r.sig {
			r.pending, r.seen = "", 0
			continue
		}
		if sig != r.pending {
			r.pending, r.seen = sig, 1
			continue
		}
		r.seen++
		if r.seen >= stableChecks || first {
			load = append(load, r)
		}
	}
	for name, r := range rs.all {
		if _, ok := entries[name]; !ok {
			unload = append(unload, r)
			delete(rs.all, name)
		}
	}
	rs.mu.Unlock()
	for _, r := range unload {
		rs.unload(r, "its file was removed")
	}
	for _, r := range load {
		rs.load(r)
	}
}

// load bundles a script and starts it. A script that doesn't build keeps
// its old version running.
func (rs *residents) load(r *Resident) {
	rs.mu.Lock()
	sig := r.pending
	r.sig, r.pending, r.seen = sig, "", 0
	disabled := rs.disabled()[r.Name]
	path := r.Path // check writes it, under the lock
	rs.mu.Unlock()
	js, err := Bundle(path, "", "")
	if err != nil {
		rs.mu.Lock()
		running := r.run != nil
		r.Error = err.Error()
		if !running {
			r.State = StateFailed
		}
		rs.mu.Unlock()
		rs.h.log.Warn("script did not build", "script", r.Name, "err", err)
		rs.h.cfg.Backend.Bus().Scoped("script_failed", map[string]any{"name": r.Name, "error": err.Error(), "kept_running": running})
		return
	}
	rs.mu.Lock()
	same := js == r.bundle && r.run != nil
	r.bundle = js
	held := r.State == StateQuarantined
	rs.mu.Unlock()
	if same || held {
		return // a quarantined script waits for hive script enable, edited or not
	}
	if disabled {
		rs.unload(r, "disabled")
		rs.mu.Lock()
		r.State, r.Error = StateDisabled, ""
		rs.mu.Unlock()
		return
	}
	rs.restart(r)
}

// restart stops a script's run, if any, and starts a new one from its
// bundle.
func (rs *residents) restart(r *Resident) {
	r.life.Lock()
	defer r.life.Unlock()
	rs.unload(r, "reloaded")
	rs.mu.Lock()
	js := r.bundle
	rs.mu.Unlock()
	_, err := rs.h.Start(RunOptions{
		Name: r.Name, JS: js, Perms: Perms{}, Resident: r, Budget: rs.budget(),
		Identity: ownerIdentity(),
		// Recorded before the loop starts, so a failure it reports at once
		// isn't overwritten here.
		OnStart: func(run *Run) {
			rs.mu.Lock()
			r.run, r.RunID, r.Error, r.Errors, r.overruns, r.LoadedAt = run, run.ID, "", 0, nil, time.Now().UTC()
			r.State = StateRunning
			rs.mu.Unlock()
			rs.h.cfg.Backend.Bus().Scoped("script_loaded", map[string]any{"name": r.Name, "run_id": run.ID})
		},
	})
	if err != nil {
		rs.mu.Lock()
		r.State, r.Error = StateFailed, err.Error()
		rs.mu.Unlock()
	}
}

// runEnded hears that a script's run ended. If it's still the script's
// current run, nobody stopped it: it stopped itself, or failed.
func (rs *residents) runEnded(r *Resident, run *Run, ok bool, msg string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if r.run != run {
		return // unloaded on purpose: its state is already said
	}
	r.run = nil
	switch {
	case r.State == StatePending, r.State == StateFailed, r.State == StateQuarantined:
	case ok:
		r.State, r.Error = StateStopped, ""
	default:
		r.State, r.Error = StateFailed, firstLineOf(msg)
	}
}

func (rs *residents) unload(r *Resident, why string) {
	rs.mu.Lock()
	run := r.run
	r.run = nil
	rs.mu.Unlock()
	if run == nil {
		return
	}
	run.stop(why)
	select {
	case <-run.done:
	case <-time.After(3 * time.Second):
		rs.h.log.Warn("script did not stop", "script", r.Name)
	}
	rs.h.cfg.Backend.Bus().Scoped("script_unloaded", map[string]any{"name": r.Name, "reason": why})
}

// manifest is called by a resident run once its top level ran: it decides
// whether setup may run, with which permissions.
func (rs *residents) manifest(r *Resident, perms []string, description string, opts []Option) (Perms, bool) {
	p, err := ParsePerms(perms...)
	if err != nil {
		return nil, false
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	r.Description = description
	r.Options = rs.applyValues(r.Name, opts)
	g, ok := rs.grants()[r.Name]
	if ok && g.Hash == permsHash(p.List()) {
		r.State, r.Permissions, r.Asks = StateRunning, p.List(), nil
		return p, true
	}
	// Asking for no more than it was granted needs no new grant.
	if ok {
		old, _ := ParsePerms(g.Permissions...)
		narrower := true
		for name := range p {
			if !old[name] {
				narrower = false
			}
		}
		if narrower {
			r.State, r.Permissions, r.Asks = StateRunning, p.List(), nil
			return p, true
		}
	}
	r.State, r.Permissions, r.Asks = StatePending, []string{}, p.List()
	return nil, false
}

// overrun counts a handler stopped by its budget; too many quarantine the
// script.
func (rs *residents) overrun(r *Resident) bool {
	rs.mu.Lock()
	now := time.Now()
	keep := r.overruns[:0]
	for _, t := range r.overruns {
		if now.Sub(t) < overrunWindow {
			keep = append(keep, t)
		}
	}
	r.overruns = append(keep, now)
	r.Overruns = len(r.overruns)
	quarantine := len(r.overruns) >= overrunLimit
	rs.mu.Unlock()
	if quarantine {
		go rs.quarantine(r, fmt.Sprintf("%d handlers ran over %v in %v", overrunLimit, rs.budget(), overrunWindow))
	}
	return quarantine
}

func (rs *residents) quarantine(r *Resident, why string) {
	r.life.Lock()
	defer r.life.Unlock()
	rs.unload(r, "quarantined: "+why)
	rs.mu.Lock()
	r.State, r.Error = StateQuarantined, why
	rs.mu.Unlock()
	rs.h.log.Warn("script quarantined", "script", r.Name, "why", why)
	rs.h.cfg.Backend.Bus().Scoped("script_quarantined", map[string]any{"name": r.Name, "reason": why})
}

func (rs *residents) handlerError(r *Resident) {
	rs.mu.Lock()
	r.Errors++
	rs.mu.Unlock()
}

func (rs *residents) setupFailed(r *Resident, msg string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if r.State != StatePending {
		r.State, r.Error = StateFailed, msg
	}
}

func (rs *residents) list() []Resident {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]Resident, 0, len(rs.all))
	for _, r := range rs.all {
		c := *r
		c.Options = append([]Option{}, r.Options...) // SetOption changes the original's
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (rs *residents) get(name string) (*Resident, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	r := rs.all[name]
	if r == nil {
		return nil, api.NotFound("no script named %s in %s", name, rs.dir)
	}
	return r, nil
}

// Grant allows a script the permissions its manifest asks for, and starts
// it.
func (h *Host) Grant(name string) (Resident, error) {
	rs := h.res
	if rs == nil {
		return Resident{}, api.BadRequest("this server has no scripts folder")
	}
	r, err := rs.get(name)
	if err != nil {
		return Resident{}, err
	}
	rs.mu.Lock()
	known := r.State == StatePending || r.State == StateRunning
	rs.mu.Unlock()
	if !known {
		// It never said what it asks for (it failed, or never ran): run it
		// once to read its manifest.
		rs.restart(r)
		rs.settle(r)
		rs.mu.Lock()
		known = r.State == StatePending || r.State == StateRunning
		rs.mu.Unlock()
		if !known {
			return h.resident(name)
		}
	}
	rs.mu.Lock()
	asks := r.Asks
	if r.State != StatePending {
		asks = r.Permissions
	}
	g := rs.grants()
	g[name] = grant{Permissions: asks, Hash: permsHash(asks), At: time.Now().UTC()}
	err = writeJSON(rs.grantsPath(), g)
	rs.mu.Unlock()
	if err != nil {
		return Resident{}, api.Internal("grants: %v", err)
	}
	rs.restart(r)
	rs.settle(r)
	return h.resident(name)
}

// settle waits, briefly, for a just-started script to read its manifest:
// so grant and enable answer with what it now runs with.
func (rs *residents) settle(r *Resident) {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rs.mu.Lock()
		run := r.run
		decided := run == nil || run.manifestRead() || r.State == StateFailed
		rs.mu.Unlock()
		if decided {
			return
		}
		if run != nil {
			select {
			case <-run.done:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
}

// Revoke takes a script's grant away and stops it.
func (h *Host) Revoke(name string) (Resident, error) {
	rs := h.res
	if rs == nil {
		return Resident{}, api.BadRequest("this server has no scripts folder")
	}
	r, err := rs.get(name)
	if err != nil {
		return Resident{}, err
	}
	rs.mu.Lock()
	g := rs.grants()
	delete(g, name)
	err = writeJSON(rs.grantsPath(), g)
	rs.mu.Unlock()
	if err != nil {
		return Resident{}, api.Internal("grants: %v", err)
	}
	rs.restart(r)
	return h.resident(name)
}

// Enable and Disable turn a script on or off without removing it.
func (h *Host) Enable(name string, on bool) (Resident, error) {
	rs := h.res
	if rs == nil {
		return Resident{}, api.BadRequest("this server has no scripts folder")
	}
	r, err := rs.get(name)
	if err != nil {
		return Resident{}, err
	}
	rs.mu.Lock()
	d := rs.disabled()
	if on {
		delete(d, name)
	} else {
		d[name] = true
	}
	err = writeJSON(rs.disabledPath(), d)
	rs.mu.Unlock()
	if err != nil {
		return Resident{}, api.Internal("disabled: %v", err)
	}
	if on {
		rs.mu.Lock()
		r.State = StateFailed // not quarantined any more; restart says what it is
		rs.mu.Unlock()
		rs.restart(r)
		rs.settle(r)
	} else {
		r.life.Lock()
		rs.unload(r, "disabled")
		r.life.Unlock()
		rs.mu.Lock()
		r.State, r.Error = StateDisabled, ""
		rs.mu.Unlock()
	}
	return h.resident(name)
}

// Reload loads one script again, or all with name "".
func (h *Host) Reload(name string) ([]Resident, error) {
	rs := h.res
	if rs == nil {
		return nil, api.BadRequest("this server has no scripts folder")
	}
	var targets []*Resident
	rs.check(false)
	rs.mu.Lock()
	for n, r := range rs.all {
		if name == "" || n == name {
			r.pending, r.sig = rs.signature(r.Path), ""
			targets = append(targets, r)
		}
	}
	rs.mu.Unlock()
	if name != "" && len(targets) == 0 {
		return nil, api.NotFound("no script named %s in %s", name, rs.dir)
	}
	for _, r := range targets {
		rs.mu.Lock()
		r.bundle = ""
		rs.mu.Unlock()
		if rs.disabled()[r.Name] {
			continue
		}
		rs.load(r)
		rs.settle(r)
	}
	return rs.list(), nil
}

// Residents lists the folder's scripts.
func (h *Host) Residents() []Resident {
	if h.res == nil {
		return []Resident{}
	}
	return h.res.list()
}

func (h *Host) resident(name string) (Resident, error) {
	for _, r := range h.Residents() {
		if r.Name == name {
			return r, nil
		}
	}
	return Resident{}, api.NotFound("no script named %s", name)
}
