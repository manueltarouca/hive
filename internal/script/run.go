package script

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/events"
	"github.com/manueltarouca/hive/internal/keymap"
	"github.com/manueltarouca/hive/internal/remote"
)

//go:embed prelude.js
var preludeSrc string

var prelude = sync.OnceValues(func() (*goja.Program, error) {
	return goja.Compile("prelude.js", preludeSrc, true)
})

// Limits for one run.
const (
	maxTimers  = 1024
	maxSubs    = 256
	historyLen = 512         // recent events, for waits started just after a call
	queueLen   = 4096        // events waiting for the script
	resultCap  = 64 << 10    // result carried by script_ended
	logCap     = 16 << 10    // one log line
	stopGrace  = time.Second // a stopping run's teardown
	readyWait  = 30 * time.Second
	loadBudget = 10 * time.Second // a one-shot's top level, before its first await
	maxHold    = 2 * time.Second  // events wait for a script's change at most this long
)

// Run is one running script: a goja runtime owned by one goroutine (its
// loop). Other goroutines hand it work with post; only interrupting it is
// done from outside.
type Run struct {
	ID, Name string
	Started  time.Time

	host     *Host
	opts     RunOptions
	cx       Context
	identity remote.Identity
	ctx      context.Context
	cancel   context.CancelFunc

	rt    *goja.Runtime
	tasks chan func()
	done  chan struct{}

	// Interrupts: a budget timer may only interrupt the task it was armed
	// for (goja's interrupt flag stays set until cleared).
	imu sync.Mutex
	gen uint64

	// Time spent running JS, per second over the last minute.
	cmu sync.Mutex
	cpu []cpuSlot

	// Loop-owned state.
	mainDone    bool
	pending     int // calls in flight
	timers      map[int]*jsTimer
	subs        map[int]*jsSub
	nextID      int
	history     []event
	lastCall    uint64 // bus seq when the latest call started
	teardown    goja.Callable
	local       []func() // work the loop queued for itself
	heldSince   time.Time
	loading     bool // the script's top level is running
	forTimer    *time.Timer
	children    []*Run // runs it started, stopped with it
	gotManifest bool
	hasActions  bool
	bindings    []keymap.Binding // what this script binds (residents only); under pmu
	keyFns      int
	modes       map[string]bool // modes this script declared: exclusive or not; under pmu
	depth       int // the cause depth of the handler running now
	store       *store

	// Events from the pump, handed to the loop.
	emu     sync.Mutex
	queue   []event
	dropped int
	evReady chan struct{}

	// Set once, read by others.
	pmu          sync.Mutex
	perms        Perms
	ended        bool
	ok           bool
	result       string
	errMsg       string
	errCode      string
	endedAt      time.Time
	logLines     int
	manifestDone bool
}

type jsTimer struct {
	t      *time.Timer
	repeat bool
	ms     int64
	fn     goja.Callable
}

type jsSub struct {
	name   string
	filter map[string]any
	fn     goja.Callable
	self   bool // also hear events this script's own calls made
}

// RunInfo describes a run.
type RunInfo struct {
	RunID       string    `json:"run_id"`
	Name        string    `json:"name"`
	State       string    `json:"state" desc:"running or ended"`
	Permissions []string  `json:"permissions"`
	Started     time.Time `json:"started_at"`
	Ended       time.Time `json:"ended_at,omitzero"`
	OK          bool      `json:"ok,omitempty"`
	Result      any       `json:"result,omitempty"`
	Error       string    `json:"error,omitempty"`
	Code        string    `json:"code,omitempty"`
}

func newRun(h *Host, o RunOptions) *Run {
	ctx, cancel := context.WithCancel(remote.WithIdentity(context.Background(), o.Identity))
	return &Run{
		ID: newRunID(), Name: o.Name, Started: time.Now().UTC(), host: h, opts: o, cx: o.Context,
		identity: o.Identity, ctx: ctx, cancel: cancel,
		tasks: make(chan func(), 1024), done: make(chan struct{}),
		timers: map[int]*jsTimer{}, subs: map[int]*jsSub{},
		evReady: make(chan struct{}, 1),
		perms:   o.Perms.Intersect(o.Limit),
	}
}

func (r *Run) permsNow() Perms {
	r.pmu.Lock()
	defer r.pmu.Unlock()
	return r.perms
}

// Info describes the run; full includes the whole result.
func (r *Run) Info(full bool) RunInfo {
	r.pmu.Lock()
	defer r.pmu.Unlock()
	in := RunInfo{RunID: r.ID, Name: r.Name, State: "running", Permissions: r.perms.List(), Started: r.Started}
	if r.ended {
		in.State, in.Ended, in.OK, in.Error, in.Code = "ended", r.endedAt, r.ok, r.errMsg, r.errCode
		if r.result != "" && (full || len(r.result) <= resultCap) {
			in.Result = json.RawMessage(r.result)
		}
	}
	return in
}

// start builds the runtime and loads the bundle, on the caller's
// goroutine: the loop hasn't started, so nothing else touches it yet.
func (r *Run) start() error {
	prog, err := prelude()
	if err != nil {
		return api.Internal("prelude: %v", err)
	}
	r.rt = goja.New()
	if err := r.install(); err != nil {
		return err
	}
	if _, err := r.rt.RunProgram(prog); err != nil {
		return api.Internal("prelude: %v", err)
	}
	main, ok := goja.AssertFunction(r.rt.Get("__main"))
	if !ok {
		return api.Internal("prelude: no __main")
	}
	r.lastCall = r.host.cfg.Backend.Bus().Seq()
	go r.pump(r.lastCall)
	if r.opts.For > 0 {
		r.forTimer = time.AfterFunc(r.opts.For, func() { r.stop("its time ran out (--for)") })
	}
	// The script's own code runs on the loop, under a budget, so stop and
	// the budget reach even a top level that never returns.
	r.loading = true
	r.post(func() {
		r.loading = false
		bundle, err := r.rt.RunString(r.opts.JS)
		if err != nil {
			r.mainDone = true
			if r.opts.Resident != nil {
				r.residentFailed(jsError(err))
				return
			}
			r.failErr(err)
			return
		}
		if _, ok := goja.AssertFunction(bundle); !ok {
			r.mainDone = true
			r.fail("the code isn't a bundle: run it with hive script run", "bad_request")
			return
		}
		_, err = main(goja.Undefined(), bundle,
			r.rt.ToValue(func(out string) {
				r.mainDone = true
				r.setResult(out)
				if r.opts.Resident != nil && !r.gotManifest {
					r.residentFailed("a script in the scripts folder needs export default hive.script({ name, permissions, setup })")
				}
			}),
			r.rt.ToValue(func(msg, code string) {
				r.mainDone = true
				if r.opts.Resident != nil {
					r.residentFailed(msg)
					return
				}
				r.fail(msg, code)
			}))
		if err != nil {
			r.mainDone = true
			if r.opts.Resident != nil {
				r.residentFailed(jsError(err))
				return
			}
			r.failErr(err)
		}
	})
	return nil
}

// post hands work to the loop from another goroutine. After the run
// ended or stopped it's dropped.
func (r *Run) post(f func()) {
	select {
	case r.tasks <- f:
	case <-r.done:
	case <-r.ctx.Done():
	}
}

func (r *Run) loop() {
	defer r.finish()
	for {
		if r.ctx.Err() != nil {
			return
		}
		select {
		case f := <-r.tasks:
			r.exec(f)
		case <-r.evReady:
			r.deliver()
		case <-r.ctx.Done():
			return
		}
		// Work the loop gave itself: run here, never through the channel,
		// which the loop alone drains.
		for len(r.local) > 0 && r.ctx.Err() == nil {
			f := r.local[0]
			r.local = r.local[1:]
			r.exec(f)
		}
		if r.isEnded() {
			return
		}
		if r.opts.Resident == nil && r.mainDone && r.pending == 0 && len(r.timers) == 0 && len(r.subs) == 0 && !r.hasActions &&
			len(r.tasks) == 0 && r.host.trig.countRun(r) == 0 {
			r.end()
			return
		}
	}
}

var errBudget = errors.New("budget")

// testHookAfterTask runs as a task's JS returns, before its budget timer is
// disarmed (tests: the timer firing in that window).
var testHookAfterTask func()

type cpuSlot struct {
	at time.Time
	d  time.Duration
}

func (r *Run) addCPU(d time.Duration) {
	r.cmu.Lock()
	defer r.cmu.Unlock()
	now := time.Now().Truncate(time.Second)
	if n := len(r.cpu); n > 0 && r.cpu[n-1].at.Equal(now) {
		r.cpu[n-1].d += d
	} else {
		r.cpu = append(r.cpu, cpuSlot{now, d})
	}
	for len(r.cpu) > 0 && time.Since(r.cpu[0].at) > cpuWindow {
		r.cpu = r.cpu[1:]
	}
}

func (r *Run) recentCPU() time.Duration {
	r.cmu.Lock()
	defer r.cmu.Unlock()
	var total time.Duration
	for _, s := range r.cpu {
		if time.Since(s.at) <= cpuWindow {
			total += s.d
		}
	}
	return total
}

// exec runs one piece of JS, interrupted if it overruns the budget.
func (r *Run) exec(f func()) {
	began := time.Now()
	defer func() { r.addCPU(time.Since(began)) }()
	var t *time.Timer
	b := r.opts.Budget
	if r.loading && b == 0 {
		b = loadBudget
	}
	if b > 0 {
		r.imu.Lock()
		gen := r.gen
		r.imu.Unlock()
		t = time.AfterFunc(b, func() {
			r.imu.Lock()
			if r.gen == gen && r.rt != nil {
				r.rt.Interrupt(errBudget)
			}
			r.imu.Unlock()
		})
	}
	defer func() {
		if testHookAfterTask != nil {
			testHookAfterTask()
		}
		r.imu.Lock()
		r.gen++
		if t != nil {
			t.Stop()
		}
		if r.ctx.Err() == nil {
			r.rt.ClearInterrupt()
		}
		r.imu.Unlock()
		if p := recover(); p != nil {
			r.fail(fmt.Sprintf("internal error: %v", p), "internal")
		}
	}()
	f()
}

// callJS calls a script function from the loop; errors are logged, and an
// interrupt by the budget ends a one-shot.
func (r *Run) callJS(fn goja.Callable, args ...goja.Value) {
	if _, err := fn(goja.Undefined(), args...); err != nil {
		r.handlerErr(err)
	}
}

func (r *Run) handlerErr(err error) {
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		if ie.Value() != errBudget {
			return
		}
		if res := r.opts.Resident; res != nil {
			// A resident script keeps running; too many overruns
			// quarantine it.
			r.logLine("error", fmt.Sprintf("a handler ran longer than %v and was stopped", r.opts.Budget))
			r.host.res.overrun(res)
			return
		}
		r.fail(fmt.Sprintf("a handler ran longer than %v", r.opts.Budget), "timeout")
		return
	}
	if res := r.opts.Resident; res != nil {
		r.host.res.handlerError(res)
	}
	r.logLine("error", jsError(err))
}

func jsError(err error) string {
	var ex *goja.Exception
	if errors.As(err, &ex) {
		return strings.TrimSpace(ex.String())
	}
	return err.Error()
}

// stop ends the run from any goroutine: cancel its calls, interrupt its JS.
func (r *Run) stop(why string) {
	r.pmu.Lock()
	if !r.ended && r.errMsg == "" {
		r.errMsg, r.errCode = why, "script_unloaded"
	}
	r.pmu.Unlock()
	r.cancel()
	r.imu.Lock()
	if r.rt != nil {
		r.rt.Interrupt(errors.New(why))
	}
	r.imu.Unlock()
}

func (r *Run) isEnded() bool {
	r.pmu.Lock()
	defer r.pmu.Unlock()
	return r.ended
}

func (r *Run) setResult(out string) {
	r.pmu.Lock()
	r.result = out
	r.pmu.Unlock()
}

func (r *Run) fail(msg, code string) {
	r.pmu.Lock()
	if r.errMsg == "" {
		r.errMsg, r.errCode = msg, code
	}
	r.pmu.Unlock()
	r.end()
}

func (r *Run) failErr(err error) {
	var ie *goja.InterruptedError
	if errors.As(err, &ie) {
		if ie.Value() == errBudget {
			r.fail(fmt.Sprintf("the script ran longer than %v without a break", r.opts.Budget), "timeout")
		}
		r.end()
		return
	}
	r.fail(jsError(err), "error")
}

// end marks the run over; the loop exits and finish cleans up.
func (r *Run) end() {
	r.pmu.Lock()
	if !r.ended {
		r.ended = true
		r.ok = r.errMsg == ""
		r.endedAt = time.Now().UTC()
	}
	r.pmu.Unlock()
	r.cancel()
}

// finish runs on the loop goroutine as it exits.
func (r *Run) finish() {
	r.end()
	if r.teardown != nil {
		r.rt.ClearInterrupt()
		rt := r.rt
		t := time.AfterFunc(stopGrace, func() { rt.Interrupt("teardown took too long") })
		_, _ = r.teardown(goja.Undefined())
		t.Stop()
	}
	for _, t := range r.timers {
		t.t.Stop()
	}
	// Ended runs are remembered for script.list; their runtime, and all
	// the script's memory, isn't.
	r.imu.Lock()
	r.rt = nil
	r.imu.Unlock()
	r.timers, r.subs, r.history, r.teardown = nil, nil, nil, nil
	r.host.dropActions(r)
	r.host.trig.removeRun(r)
	r.pmu.Lock()
	had := len(r.bindings) > 0
	r.bindings = nil
	r.pmu.Unlock()
	if had {
		r.host.keymapChanged()
	}
	if r.forTimer != nil {
		r.forTimer.Stop()
	}
	r.pmu.Lock()
	kids := r.children
	r.pmu.Unlock()
	for _, k := range kids {
		k.stop("the script that started it stopped")
	}
	close(r.done)
	if res := r.opts.Resident; res != nil && r.host.res != nil {
		in := r.Info(false)
		r.host.res.runEnded(res, r, in.OK, in.Error)
	}
	r.host.ended(r)
	in := r.Info(false)
	payload := map[string]any{"run_id": r.ID, "name": r.Name, "ok": in.OK}
	if in.Error != "" {
		payload["error"], payload["code"] = in.Error, in.Code
	}
	if in.Result != nil {
		payload["result"] = in.Result
	} else if r.result != "" {
		payload["result_truncated"] = true
	}
	r.host.cfg.Backend.Bus().Scoped("script_ended", payload)
	if f := r.host.cfg.OnEnded; f != nil {
		f(r.Name)
	}
}

// residentFailed: a resident script's load or setup failed; it stops and
// script.list says why.
func (r *Run) residentFailed(msg string) {
	r.logLine("error", msg)
	r.host.res.setupFailed(r.opts.Resident, firstLineOf(msg))
	r.fail(msg, "error")
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func (r *Run) logLine(level, text string) {
	if len(text) > logCap {
		text = text[:logCap] + "…"
	}
	if r.opts.Resident != nil {
		r.host.appendLog(r.Name, level, text)
	}
	r.host.cfg.Backend.Bus().Scoped("script_log", map[string]any{"run_id": r.ID, "name": r.Name, "level": level, "text": text})
}

// ---- the native functions the prelude builds on ----

func (r *Run) install() error {
	rt := r.rt
	o := rt.NewObject()
	ctxJSON, _ := json.Marshal(map[string]any{
		"session_id": r.cx.SessionID, "block_id": r.cx.BlockID, "client_id": r.cx.ClientID,
		"origin": "cli", "principal": r.identity.Principal, "server": "local", "run_id": r.ID, "depth": 0, "script": r.Name,
	})
	args := string(r.opts.Args)
	if args == "" {
		args = "{}"
	}
	methods := []string{}
	for _, m := range r.host.cfg.Backend.Registry().List() {
		methods = append(methods, m.Name)
	}
	methodsJSON, _ := json.Marshal(methods)
	set := func(name string, v any) {
		if err := o.Set(name, v); err != nil {
			panic(err)
		}
	}
	set("ctx", string(ctxJSON))
	set("args", args)
	set("methods", func() string { return string(methodsJSON) })
	set("call", r.jsCall)
	set("log", func(level, text string) { r.logLine(level, text) })
	set("timer", r.jsTimer)
	set("cancel", func(id int) { r.cancelTimer(id) })
	set("active", func(id int) bool { _, ok := r.timers[id]; return ok })
	set("subscribe", r.jsSubscribe)
	set("unsubscribe", func(id int) { delete(r.subs, id) })
	set("stop", func(code int) {
		if code != 0 {
			r.fail(fmt.Sprintf("the script stopped with code %d", code), "error")
		} else {
			r.end()
		}
		// An interrupt, not an exception: the script can't catch it.
		r.imu.Lock()
		rt.Interrupt(errStopped)
		r.imu.Unlock()
	})
	set("manifest", r.jsManifest)
	set("registerAction", r.jsRegisterAction)
	set("bind", r.jsBind)
	set("mode", func(name string, exclusive bool) {
		r.pmu.Lock()
		if r.modes == nil {
			r.modes = map[string]bool{}
		}
		r.modes[name] = exclusive
		r.pmu.Unlock()
		r.host.keymapChanged()
	})
	set("unbind", r.jsUnbind)
	set("trigger", r.jsTrigger)
	set("untrigger", r.jsUntrigger)
	set("home", func() string { d, _ := os.UserHomeDir(); return d })
	set("re2", func(pattern, text string) bool {
		re, err := compileRE2(pattern)
		if err != nil {
			panic(r.rt.NewTypeError("%v", err))
		}
		return re.MatchString(text)
	})
	set("storeGet", func(key string) string { return r.st().get(key) })
	set("storeSet", func(key, value string) {
		if err := r.st().set(key, value); err != nil {
			panic(r.rt.NewTypeError("%v", err))
		}
	})
	set("storeDelete", func(key string) {
		if err := r.st().delete(key); err != nil {
			panic(r.rt.NewTypeError("%v", err))
		}
	})
	set("storeKeys", func(prefix string) string {
		b, _ := json.Marshal(r.st().keys(prefix))
		return string(b)
	})
	set("teardown", func(fn goja.Callable) { r.teardown = fn })
	return rt.Set("__host", o)
}

var errStopped = errors.New("h.stop")

func (r *Run) jsCall(method, argsJSON string) *goja.Promise {
	p, resolve, reject := r.rt.NewPromise()
	r.pending++
	r.lastCall = r.host.cfg.Backend.Bus().Seq()
	depth := r.depth
	go func() {
		out, err := r.host.call(r, method, argsJSON, depth)
		r.post(func() {
			r.pending--
			if err != nil {
				b, _ := json.Marshal(asAPI(err))
				_ = reject(string(b))
				return
			}
			_ = resolve(out)
		})
	}()
	return p
}

func (r *Run) jsTimer(ms int64, repeat bool, fn goja.Callable) int {
	if len(r.timers) >= maxTimers {
		panic(r.rt.NewTypeError("too many timers (at most %d)", maxTimers))
	}
	if ms < 1 {
		ms = 1
	}
	r.nextID++
	id := r.nextID
	jt := &jsTimer{repeat: repeat, ms: ms, fn: fn}
	r.timers[id] = jt
	r.armTimer(id, jt)
	return id
}

func (r *Run) armTimer(id int, jt *jsTimer) {
	jt.t = time.AfterFunc(time.Duration(jt.ms)*time.Millisecond, func() {
		r.post(func() {
			if r.timers[id] != jt {
				return // cancelled
			}
			if jt.repeat {
				r.armTimer(id, jt)
			} else {
				delete(r.timers, id)
			}
			r.depth = 0
			r.callJS(jt.fn)
		})
	})
}

func (r *Run) cancelTimer(id int) {
	if jt := r.timers[id]; jt != nil {
		jt.t.Stop()
		delete(r.timers, id)
	}
}

func (r *Run) jsSubscribe(name, filterJSON string, fn goja.Callable, self bool) int {
	if len(r.subs) >= maxSubs {
		panic(r.rt.NewTypeError("too many event handlers (at most %d)", maxSubs))
	}
	var filter map[string]any
	if err := json.Unmarshal([]byte(filterJSON), &filter); err != nil {
		panic(r.rt.NewTypeError("filter: %v", err))
	}
	r.nextID++
	id := r.nextID
	s := &jsSub{name: name, filter: filter, fn: fn, self: self}
	r.subs[id] = s
	// Events since the latest call started may already be here: a split's
	// program can exit before the script starts waiting for it.
	since := r.lastCall
	var replay []event
	for _, ev := range r.history {
		if ev.Seq > since {
			replay = append(replay, ev)
		}
	}
	if len(replay) > 0 {
		r.local = append(r.local, func() {
			for _, ev := range replay {
				if r.subs[id] == s {
					r.dispatch(s, ev)
				}
			}
		})
	}
	return id
}

// manifestRead reports whether the script has declared itself yet.
func (r *Run) manifestRead() bool {
	r.pmu.Lock()
	defer r.pmu.Unlock()
	return r.manifestDone
}

// jsManifest reads a hive.script manifest and says whether setup may run.
func (r *Run) jsManifest(manifestJSON string) bool {
	var m struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &m); err != nil {
		panic(r.rt.NewTypeError("manifest: %v", err))
	}
	p, err := ParsePerms(m.Permissions...)
	if err != nil {
		panic(r.rt.NewTypeError("manifest: %v", err))
	}
	r.gotManifest = true
	defer func() { r.pmu.Lock(); r.manifestDone = true; r.pmu.Unlock() }()
	if res := r.opts.Resident; res != nil {
		granted, ok := r.host.res.manifest(res, p.List(), m.Description)
		if !ok {
			r.logLine("warn", fmt.Sprintf("waiting for permission: hive script grant %s (it asks for %s)", r.Name, strings.Join(p.List(), ", ")))
			r.end()
			return false
		}
		r.pmu.Lock()
		r.perms = granted
		r.pmu.Unlock()
		return true
	}
	r.pmu.Lock()
	r.perms = p.Intersect(r.opts.Limit)
	r.pmu.Unlock()
	return true
}

// ---- events ----

// scriptEvents are the scoped events scripts hear: other scripts starting,
// ending, loading and failing. Not their log lines.
var scriptEvents = []string{"script_started", "script_ended", "script_loaded", "script_unloaded", "script_failed", "script_quarantined"}

// pump copies bus events into the run's queue. It never runs script code,
// so it keeps up with the bus; if the bus cuts it anyway, it subscribes
// again from the last event it saw.
func (r *Run) pump(since uint64) {
	bus := r.host.cfg.Backend.Bus()
	last := int64(since)
	for r.ctx.Err() == nil {
		sub := bus.Subscribe(events.Filter{All: true, Scoped: scriptEvents}, last, 1024)
		gap := sub.Gap
	read:
		for {
			select {
			case e, ok := <-sub.C:
				if !ok {
					break read
				}
				last = int64(e.Seq)
				if ev, ok := unwrap(e); ok {
					r.enqueue(ev)
				}
			case <-r.ctx.Done():
				bus.Unsubscribe(sub)
				return
			}
		}
		if sub.Dropped() || gap {
			r.enqueue(event{Name: "resync_required", Payload: map[string]any{}, JSON: "{}"})
		}
	}
}

func (r *Run) enqueue(ev event) {
	r.emu.Lock()
	if len(r.queue) >= queueLen {
		r.queue = r.queue[1:]
		r.dropped++
	}
	r.queue = append(r.queue, ev)
	r.emu.Unlock()
	select {
	case r.evReady <- struct{}{}:
	default:
	}
}

// deliver hands queued events to the script's handlers, on the loop.
func (r *Run) deliver() {
	r.emu.Lock()
	q, dropped := r.queue, r.dropped
	r.queue, r.dropped = nil, 0
	r.emu.Unlock()
	if dropped > 0 {
		q = append([]event{{Name: "resync_required", Payload: map[string]any{"dropped": float64(dropped)},
			JSON: fmt.Sprintf(`{"dropped":%d}`, dropped)}}, q...)
	}
	hold, holding := r.host.causes.holdAfter()
	if !holding {
		r.heldSince = time.Time{}
	} else if r.heldSince.IsZero() {
		r.heldSince = time.Now()
	} else if time.Since(r.heldSince) > maxHold {
		holding = false // a slow change doesn't stall everyone: deliver, untagged
	}
	for i, ev := range q {
		if holding && ev.Seq > hold {
			// A script's change is still running: wait for it to end, so
			// its events can be told apart.
			r.emu.Lock()
			r.queue = append(q[i:len(q):len(q)], r.queue...)
			r.emu.Unlock()
			// Look again once it can't be held any longer.
			time.AfterFunc(maxHold, func() {
				select {
				case r.evReady <- struct{}{}:
				default:
				}
			})
			return
		}
		if !r.inScope(ev) {
			continue
		}
		if ev.Seq > 0 {
			r.history = append(r.history, ev)
			if len(r.history) > historyLen {
				r.history = r.history[len(r.history)-historyLen:]
			}
		}
		for id, s := range r.subs {
			r.exec(func() {
				if r.subs[id] == s {
					r.dispatch(s, ev)
				}
			})
		}
		if r.isEnded() {
			return
		}
	}
}

// inScope: a one-shot hears its own session (and server events) unless it
// asked for all.
func (r *Run) inScope(ev event) bool {
	return r.opts.All || r.cx.SessionID == "" || ev.SessionID == "" || ev.SessionID == r.cx.SessionID
}

func (r *Run) dispatch(s *jsSub, ev event) {
	if s.name != "*" && s.name != ev.Name {
		return
	}
	if !matches(s.filter, ev.Payload) {
		return
	}
	depth := 0
	if w, ok := r.host.causes.of(ev); ok {
		if w.run == r.ID && !s.self {
			return
		}
		depth = w.depth + 1
	}
	r.depth = depth
	parse, _ := goja.AssertFunction(r.rt.Get("JSON").ToObject(r.rt).Get("parse"))
	payload, err := parse(goja.Undefined(), r.rt.ToValue(ev.JSON))
	if err != nil {
		return
	}
	meta := r.rt.NewObject()
	_ = meta.Set("event", ev.Name)
	_ = meta.Set("seq", ev.Seq)
	_ = meta.Set("depth", depth)
	r.callJS(s.fn, payload, meta)
}

// st is the script's storage, opened on first use.
func (r *Run) st() *store {
	if r.store == nil {
		dir := r.host.cfg.StateDir
		if dir == "" {
			dir = os.TempDir()
		}
		name := r.Name
		if r.opts.Resident == nil {
			name = "oneshot/" + name // never a resident's storage
		}
		r.store = openStore(dir, name)
	}
	return r.store
}

// jsBind binds a key (in a mode, as "resize/left") to an action. Only
// scripts in the folder bind keys: a one-shot's would vanish with it.
func (r *Run) jsBind(key, action, argsJSON string) {
	if r.opts.Resident == nil {
		r.logLine("warn", "h.bind: only scripts in the scripts folder bind keys; "+key+" isn't bound")
		return
	}
	if !r.permsNow().Has("ui") {
		panic(r.rt.NewTypeError("binding keys needs the ui permission, which this script doesn't have"))
	}
	line := key + "=" + action
	if argsJSON != "" && argsJSON != "{}" {
		line += ":" + argsJSON
	}
	b, err := keymap.ParseBinding(line)
	if err != nil {
		panic(r.rt.NewTypeError("h.bind: %v", err))
	}
	b.Source = "script:" + r.Name
	r.pmu.Lock() // the host reads bindings from other goroutines
	defer r.pmu.Unlock()
	for i, old := range r.bindings {
		if old.Key == b.Key && old.Mode == b.Mode {
			r.bindings = append(r.bindings[:i], r.bindings[i+1:]...)
			break
		}
	}
	r.bindings = append(r.bindings, b)
	r.host.keymapChanged()
}

// jsUnbind removes this script's binding of a key; it returns how many went.
func (r *Run) jsUnbind(key string) int {
	mode, k, ok := strings.Cut(key, "/")
	if !ok {
		mode, k = "", key
	}
	parsed, err := keymap.Parse(k)
	if err != nil {
		panic(r.rt.NewTypeError("h.unbind: %v", err))
	}
	r.pmu.Lock()
	defer r.pmu.Unlock()
	n := 0
	keep := []keymap.Binding{}
	for _, b := range r.bindings {
		if b.Key == parsed.String() && b.Mode == mode {
			n++
			continue
		}
		keep = append(keep, b)
	}
	r.bindings = keep
	if n > 0 {
		r.host.keymapChanged()
	}
	return n
}
