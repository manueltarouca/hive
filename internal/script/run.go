package script

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/events"
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

	// Loop-owned state.
	mainDone bool
	pending  int // calls in flight
	timers   map[int]*jsTimer
	subs     map[int]*jsSub
	nextID   int
	history  []event
	lastCall uint64 // bus seq when the latest call started
	teardown goja.Callable
	stopping bool

	// Events from the pump, handed to the loop.
	emu     sync.Mutex
	queue   []event
	dropped int
	evReady chan struct{}

	// Set once, read by others.
	pmu      sync.Mutex
	perms    Perms
	ended    bool
	ok       bool
	result   string
	errMsg   string
	errCode  string
	endedAt  time.Time
	logLines int
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
	bundle, err := r.rt.RunString(r.opts.JS)
	if err != nil {
		return api.BadRequest("script: %v", err)
	}
	main, ok := goja.AssertFunction(r.rt.Get("__main"))
	if !ok {
		return api.Internal("prelude: no __main")
	}
	r.lastCall = r.host.cfg.Backend.Bus().Seq()
	go r.pump(r.lastCall)
	if r.opts.For > 0 {
		time.AfterFunc(r.opts.For, func() { r.stop("its time ran out (--for)") })
	}
	r.post(func() {
		_, err := main(goja.Undefined(), bundle,
			r.rt.ToValue(func(out string) { r.mainDone = true; r.setResult(out) }),
			r.rt.ToValue(func(msg, code string) { r.mainDone = true; r.fail(msg, code) }))
		if err != nil {
			r.mainDone = true
			r.failErr(err)
		}
	})
	return nil
}

// post hands work to the loop. After the run ended it's dropped.
func (r *Run) post(f func()) {
	select {
	case r.tasks <- f:
	case <-r.done:
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
		if r.isEnded() {
			return
		}
		if r.mainDone && r.pending == 0 && len(r.timers) == 0 && len(r.subs) == 0 && len(r.tasks) == 0 {
			r.end()
			return
		}
	}
}

var errBudget = errors.New("budget")

// testHookAfterTask runs as a task's JS returns, before its budget timer is
// disarmed (tests: the timer firing in that window).
var testHookAfterTask func()

// exec runs one piece of JS, interrupted if it overruns the budget.
func (r *Run) exec(f func()) {
	var t *time.Timer
	if b := r.opts.Budget; b > 0 {
		r.imu.Lock()
		gen := r.gen
		r.imu.Unlock()
		t = time.AfterFunc(b, func() {
			r.imu.Lock()
			if r.gen == gen {
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
		if ie.Value() == errBudget {
			r.fail(fmt.Sprintf("a handler ran longer than %v", r.opts.Budget), "timeout")
		}
		return
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
		t := time.AfterFunc(stopGrace, func() { r.rt.Interrupt("teardown took too long") })
		_, _ = r.teardown(goja.Undefined())
		t.Stop()
	}
	for _, t := range r.timers {
		t.t.Stop()
	}
	close(r.done)
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
}

func (r *Run) logLine(level, text string) {
	if len(text) > logCap {
		text = text[:logCap] + "…"
	}
	r.host.cfg.Backend.Bus().Scoped("script_log", map[string]any{"run_id": r.ID, "name": r.Name, "level": level, "text": text})
}

// ---- the native functions the prelude builds on ----

func (r *Run) install() error {
	rt := r.rt
	o := rt.NewObject()
	ctxJSON, _ := json.Marshal(map[string]any{
		"session_id": r.cx.SessionID, "block_id": r.cx.BlockID, "client_id": r.cx.ClientID,
		"origin": "cli", "principal": r.identity.Principal, "server": "local", "run_id": r.ID, "depth": 0,
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
	set("teardown", func(fn goja.Callable) { r.teardown = fn })
	return rt.Set("__host", o)
}

var errStopped = errors.New("h.stop")

func (r *Run) jsCall(method, argsJSON string) *goja.Promise {
	p, resolve, reject := r.rt.NewPromise()
	r.pending++
	r.lastCall = r.host.cfg.Backend.Bus().Seq()
	go func() {
		out, err := r.host.call(r, method, argsJSON)
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

func (r *Run) jsSubscribe(name, filterJSON string, fn goja.Callable) int {
	if len(r.subs) >= maxSubs {
		panic(r.rt.NewTypeError("too many event handlers (at most %d)", maxSubs))
	}
	var filter map[string]any
	if err := json.Unmarshal([]byte(filterJSON), &filter); err != nil {
		panic(r.rt.NewTypeError("filter: %v", err))
	}
	r.nextID++
	id := r.nextID
	s := &jsSub{name: name, filter: filter, fn: fn}
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
		r.post(func() {
			for _, ev := range replay {
				if r.subs[id] == s {
					r.dispatch(s, ev)
				}
			}
		})
	}
	return id
}

func (r *Run) jsManifest(manifestJSON string) {
	var m struct {
		Name        string   `json:"name"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &m); err != nil {
		panic(r.rt.NewTypeError("manifest: %v", err))
	}
	p, err := ParsePerms(m.Permissions...)
	if err != nil {
		panic(r.rt.NewTypeError("manifest: %v", err))
	}
	r.pmu.Lock()
	r.perms = p.Intersect(r.opts.Limit)
	if m.Name != "" && r.opts.Name == "script" {
		r.Name = m.Name
	}
	r.pmu.Unlock()
}

// ---- events ----

// pump copies bus events into the run's queue. It never runs script code,
// so it keeps up with the bus; if the bus cuts it anyway, it subscribes
// again from the last event it saw.
func (r *Run) pump(since uint64) {
	bus := r.host.cfg.Backend.Bus()
	last := int64(since)
	for r.ctx.Err() == nil {
		sub := bus.Subscribe(events.Filter{All: true}, last, 1024)
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
	for _, ev := range q {
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
	parse, _ := goja.AssertFunction(r.rt.Get("JSON").ToObject(r.rt).Get("parse"))
	payload, err := parse(goja.Undefined(), r.rt.ToValue(ev.JSON))
	if err != nil {
		return
	}
	meta := r.rt.NewObject()
	_ = meta.Set("event", ev.Name)
	_ = meta.Set("seq", ev.Seq)
	r.callJS(s.fn, payload, meta)
}
