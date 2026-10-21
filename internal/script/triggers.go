package script

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/manueltarouca/hive/internal/block"
	"github.com/manueltarouca/hive/internal/events"
)

// Output triggers: a script names a pattern and hears when a terminal
// prints a line that matches. Matching runs in Go, on lines the assembler
// makes from each block's output; only matches reach the script.

const (
	maxTriggers   = 64          // per script
	triggerRate   = 1000        // matches a minute, per trigger
	dedupeWindow  = time.Second // the same line again is the same match
	idleFlush     = 250 * time.Millisecond
	snapshotEvery = 500 * time.Millisecond
	tapBuffer     = 256
)

// BlockRef names a block and its session.
type BlockRef struct{ ID, SessionID string }

// Taps is what triggers need from the server: blocks, their output, and
// their screen.
type Taps interface {
	Blocks() []BlockRef
	Tap(blockID string) (*block.Sub, func(), error)
	FormatScreen(blockID string) (string, error)
}

type trigger struct {
	id       int
	run      *Run
	fn       goja.Callable
	re       *regexp.Regexp
	name     string
	block    string
	session  string
	screen   string // main, alt or any
	snapshot bool
	once     bool
	interval time.Duration

	last     time.Time
	minute   time.Time
	inMinute int
	dropped  int
	fired    bool
}

type tap struct {
	ref  BlockRef
	stop chan struct{}
}

type triggerSet struct {
	h      *Host
	mu     sync.Mutex
	next   int
	all    map[int]*trigger
	taps   map[string]*tap
	seen   map[string]time.Time // trigger, block, text: when it last matched
	watch  *events.Sub
	closed bool
}

func newTriggerSet(h *Host) *triggerSet {
	return &triggerSet{h: h, all: map[int]*trigger{}, taps: map[string]*tap{}, seen: map[string]time.Time{}}
}

func (ts *triggerSet) taps_() Taps {
	t, _ := ts.h.cfg.Backend.(Taps)
	return t
}

// add registers a trigger and starts reading output, if this is the first.
func (ts *triggerSet) add(t *trigger) int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.next++
	t.id = ts.next
	ts.all[t.id] = t
	if len(ts.all) == 1 && !ts.closed {
		ts.startLocked()
	}
	return t.id
}

func (ts *triggerSet) remove(id int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.all, id)
	if len(ts.all) == 0 {
		ts.stopLocked()
	}
}

func (ts *triggerSet) removeRun(r *Run) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for id, t := range ts.all {
		if t.run == r {
			delete(ts.all, id)
		}
	}
	if len(ts.all) == 0 {
		ts.stopLocked()
	}
}

func (ts *triggerSet) countRun(r *Run) int {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	n := 0
	for _, t := range ts.all {
		if t.run == r {
			n++
		}
	}
	return n
}

// startLocked taps every block, and watches for new ones.
func (ts *triggerSet) startLocked() {
	tp := ts.taps_()
	if tp == nil {
		return
	}
	bus := ts.h.cfg.Backend.Bus()
	ts.watch = bus.Subscribe(events.Filter{All: true}, events.Live, 1024)
	go ts.watchBlocks(ts.watch)
	for _, b := range tp.Blocks() {
		ts.tapLocked(b)
	}
}

func (ts *triggerSet) stopLocked() {
	if ts.watch != nil {
		ts.h.cfg.Backend.Bus().Unsubscribe(ts.watch)
		ts.watch = nil
	}
	for id, t := range ts.taps {
		close(t.stop)
		delete(ts.taps, id)
	}
	ts.seen = map[string]time.Time{}
}

func (ts *triggerSet) close() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.closed = true
	ts.all = map[int]*trigger{}
	ts.stopLocked()
}

// watchBlocks taps blocks as they're created and lets go as they close.
func (ts *triggerSet) watchBlocks(sub *events.Sub) {
	for e := range sub.C {
		ev, ok := unwrap(e)
		if !ok {
			continue
		}
		id, _ := ev.Payload["block_id"].(string)
		switch ev.Name {
		case "block_created":
			ts.mu.Lock()
			if ts.watch == sub && id != "" {
				ts.tapLocked(BlockRef{ID: id, SessionID: ev.SessionID})
			}
			ts.mu.Unlock()
		case "block_closed":
			ts.mu.Lock()
			if t := ts.taps[id]; t != nil {
				close(t.stop)
				delete(ts.taps, id)
			}
			ts.mu.Unlock()
		}
	}
}

func (ts *triggerSet) tapLocked(b BlockRef) {
	if _, ok := ts.taps[b.ID]; ok {
		return
	}
	t := &tap{ref: b, stop: make(chan struct{})}
	ts.taps[b.ID] = t
	go ts.read(t)
}

// read assembles one block's output into lines and offers them.
func (ts *triggerSet) read(t *tap) {
	tp := ts.taps_()
	sub, detach, err := tp.Tap(t.ref.ID)
	if err != nil {
		return
	}
	defer func() { detach() }()
	var a assembler
	idle := time.NewTimer(time.Hour)
	defer idle.Stop()
	var lastSnap time.Time
	prev := map[string]bool{}
	for {
		select {
		case <-t.stop:
			return
		case f := <-sub.C:
			if f.Resync {
				// It fell behind: start again from now. Lines in the gap
				// are lost; scripts can search the scrollback.
				detach()
				if sub, detach, err = tp.Tap(t.ref.ID); err != nil {
					return
				}
				a = assembler{}
				continue
			}
			for _, l := range a.feed(f.Data) {
				ts.offer(t.ref, l, false)
			}
			idle.Reset(idleFlush)
		case <-idle.C:
			if l, ok := a.flush(); ok {
				ts.offer(t.ref, l, false)
			}
			if ts.wantsSnapshot(t.ref) && time.Since(lastSnap) >= snapshotEvery {
				lastSnap = time.Now()
				screen, err := tp.FormatScreen(t.ref.ID)
				if err != nil {
					continue
				}
				now := map[string]bool{}
				for _, text := range strings.Split(screen, "\n") {
					text = strings.TrimRight(text, " ")
					if text == "" {
						continue
					}
					now[text] = true
					if !prev[text] {
						ts.offer(t.ref, line{text: text, alt: a.alt}, true)
					}
				}
				prev = now
			}
		}
	}
}

func (ts *triggerSet) wantsSnapshot(b BlockRef) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, t := range ts.all {
		if t.snapshot && t.applies(b) {
			return true
		}
	}
	return false
}

func (t *trigger) applies(b BlockRef) bool {
	switch {
	case t.block != "":
		return t.block == b.ID
	case t.session != "":
		return t.session == b.SessionID
	}
	return t.run.inScope(event{SessionID: b.SessionID})
}

// offer matches a line against every trigger that covers its block.
func (ts *triggerSet) offer(b BlockRef, l line, fromSnapshot bool) {
	now := time.Now()
	type hit struct {
		t      *trigger
		groups []string
	}
	var hits []hit
	ts.mu.Lock()
	for id, t := range ts.all {
		if t.snapshot != fromSnapshot || !t.applies(b) {
			continue
		}
		if t.screen == "main" && l.alt || t.screen == "alt" && !l.alt {
			continue
		}
		groups := t.re.FindStringSubmatch(l.text)
		if groups == nil {
			continue
		}
		key := strconv.Itoa(id) + "\x00" + b.ID + "\x00" + l.text
		if last, ok := ts.seen[key]; ok && now.Sub(last) < dedupeWindow {
			continue
		}
		ts.seen[key] = now
		if t.interval > 0 && now.Sub(t.last) < t.interval {
			continue
		}
		if now.Sub(t.minute) > time.Minute {
			if t.dropped > 0 {
				t.run.logLine("warn", "trigger "+t.name+": "+itoa(t.dropped)+" matches over the limit were dropped")
			}
			t.minute, t.inMinute, t.dropped = now, 0, 0
		}
		if t.inMinute >= triggerRate {
			t.dropped++
			continue
		}
		t.inMinute++
		t.last = now
		hits = append(hits, hit{t, groups})
		if t.once {
			delete(ts.all, id)
		}
	}
	if len(ts.seen) > 10000 {
		for k, v := range ts.seen {
			if now.Sub(v) > dedupeWindow {
				delete(ts.seen, k)
			}
		}
	}
	ts.mu.Unlock()
	screen := "main"
	if l.alt {
		screen = "alt"
	}
	for _, x := range hits {
		m := map[string]any{"block_id": b.ID, "session_id": b.SessionID, "trigger": x.t.name,
			"match": x.groups[0], "groups": x.groups[1:], "line": l.text, "screen": screen}
		js, _ := json.Marshal(m)
		t, r := x.t, x.t.run
		r.post(func() {
			if r.rt == nil {
				return
			}
			parse, _ := goja.AssertFunction(r.rt.Get("JSON").ToObject(r.rt).Get("parse"))
			v, err := parse(goja.Undefined(), r.rt.ToValue(string(js)))
			if err != nil {
				return
			}
			r.depth = 0
			r.callJS(t.fn, v)
		})
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// jsTrigger registers a trigger for the script.
func (r *Run) jsTrigger(specJSON string, fn goja.Callable) int {
	if !r.permsNow().Has("screen") {
		panic(r.rt.NewTypeError("output triggers need the screen permission, which this script doesn't have"))
	}
	var spec struct {
		Name        string  `json:"name"`
		Pattern     string  `json:"pattern"`
		Flags       string  `json:"flags"`
		Block       string  `json:"block"`
		Session     string  `json:"session"`
		Screen      string  `json:"screen"`
		Mode        string  `json:"mode"`
		Once        bool    `json:"once"`
		MinInterval float64 `json:"min_interval_ms"`
	}
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		panic(r.rt.NewTypeError("trigger: %v", err))
	}
	if spec.Pattern == "" {
		panic(r.rt.NewTypeError("a trigger needs a pattern"))
	}
	pat := spec.Pattern
	if f := strings.Trim(spec.Flags, " "); f != "" {
		if strings.Trim(f, "ims") != "" {
			panic(r.rt.NewTypeError("trigger flags: i, m and s only"))
		}
		pat = "(?" + f + ")" + pat
	}
	re, err := compileRE2(pat)
	if err != nil {
		panic(r.rt.NewTypeError("%v", err))
	}
	switch spec.Screen {
	case "":
		spec.Screen = "main"
	case "main", "alt", "any":
	default:
		panic(r.rt.NewTypeError("trigger screen: main, alt or any"))
	}
	if spec.Mode != "" && spec.Mode != "lines" && spec.Mode != "snapshot" {
		panic(r.rt.NewTypeError("trigger mode: lines or snapshot"))
	}
	if r.host.trig.countRun(r) >= maxTriggers {
		panic(r.rt.NewTypeError("too many triggers (at most %d)", maxTriggers))
	}
	if spec.Name == "" {
		spec.Name = spec.Pattern
	}
	t := &trigger{run: r, fn: fn, re: re, name: spec.Name, block: spec.Block, session: spec.Session,
		screen: spec.Screen, snapshot: spec.Mode == "snapshot", once: spec.Once,
		interval: time.Duration(spec.MinInterval * float64(time.Millisecond))}
	return r.host.trig.add(t)
}

// jsUntrigger cancels one of the script's triggers.
func (r *Run) jsUntrigger(id int) {
	ts := r.host.trig
	ts.mu.Lock()
	t := ts.all[id]
	ts.mu.Unlock()
	if t != nil && t.run == r {
		ts.remove(id)
	}
}
