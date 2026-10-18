package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/manueltarouca/hive/internal/server"
)

// The output probe: how fast output gets from a program to a client. Each
// pane's emitter stamps a line with the time every 10 ms; an attach client
// (the app's protocol) subscribes to every pane and reads the stamps back.
// It ramps the output rate per pane. Degraded: latency p99 above the limit,
// less than 90% of the offered bytes delivered, or a resync (the server
// dropped a client that fell behind).

// stampPrefix starts a timestamp line: it can't occur in the emitter's
// other output.
const stampPrefix = "@@hive-t="

type outputStep struct {
	Panes         int     `json:"panes"`
	RatePerPane   int     `json:"rate_bytes_per_s_per_pane"`
	Latency       Dist    `json:"latency"`
	OfferedMBps   float64 `json:"offered_mb_per_s"`
	DeliveredMBps float64 `json:"delivered_mb_per_s"`
	Delivered     float64 `json:"delivered_ratio"`
	Resyncs       int     `json:"resyncs"`
	BytesPerFrame float64 `json:"bytes_per_frame"`
	CPUCores      float64 `json:"cpu_cores"` // the process's own (getrusage): server and client
	Memory        Memory  `json:"memory"`
	Degraded      bool    `json:"degraded"`
	Why           string  `json:"why,omitempty"`
}

type outputResult struct {
	Scenario string         `json:"scenario"`
	Meta     Meta           `json:"meta"`
	Params   map[string]any `json:"params"`
	Steps    []outputStep   `json:"steps"`
	Knee     int            `json:"knee_rate_bytes_per_s_per_pane"`
	Note     string         `json:"note,omitempty"`
}

func outputProbe(args []string) error {
	fs := flag.NewFlagSet("output", flag.ContinueOnError)
	secs := fs.Int("secs", 6, "seconds measured per step (after 2 s of warm-up)")
	panes := fs.Int("panes", 1, "panes, each writing at the step's rate")
	limit := fs.Duration("limit", 50*time.Millisecond, "latency p99 above this is degraded")
	out := fs.String("out", "bench/results", "where results go")
	only := fs.Int("rate", 0, "run only this rate per pane (with --profile)")
	profile := fs.String("profile", "", "write cpu and heap profiles of the measured step here (prefix)")
	cost := fs.Duration("client-cost", 0, "time the client spends on each frame, as a slower client (the app parsing) would")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	rates := []int{64 << 10, 256 << 10, 1 << 20, 4 << 20, 16 << 20, 64 << 20, 128 << 20}
	if *only > 0 {
		rates = []int{*only}
	}
	m := meta()
	res := outputResult{Scenario: "output", Meta: m,
		Params: map[string]any{"panes": *panes, "secs": *secs, "limit_ms": limit.Milliseconds(), "rates": rates}}
	if *panes > ptyBudget(m) {
		return fmt.Errorf("%d panes is over this machine's pty budget (%d)", *panes, ptyBudget(m))
	}
	degradedRun := 0
	for _, rate := range rates {
		st, err := outputStepRun(exe, *panes, rate, *secs, *limit, *profile, *cost)
		if err != nil {
			return err
		}
		res.Steps = append(res.Steps, st)
		fmt.Printf("panes %3d  rate %6.2f MB/s each | latency p50 %6.1f p99 %7.1f max %7.1f ms | delivered %7.2f of %7.2f MB/s (%3.0f%%) | resyncs %d | %6.0f B/frame | cpu %4.2f cores | rss %5.0f MB (go %4.0f)%s\n",
			*panes, float64(rate)/(1<<20), st.Latency.P50, st.Latency.P99, st.Latency.Max, st.DeliveredMBps, st.OfferedMBps,
			st.Delivered*100, st.Resyncs, st.BytesPerFrame, st.CPUCores, st.Memory.ResidentMB, st.Memory.GoHeapMB, map[bool]string{true: "  DEGRADED: " + st.Why, false: ""}[st.Degraded])
		if st.Degraded {
			if degradedRun++; degradedRun == 2 {
				break
			}
		} else {
			degradedRun = 0
			res.Knee = rate
		}
	}
	res.Meta.LoadAfter = loadAvg()
	name, err := writeResult(*out, "output", res)
	if err == nil {
		fmt.Println("result:", name)
	}
	return err
}

func outputStepRun(exe string, panes, rate, secs int, limit time.Duration, profile string, cost time.Duration) (outputStep, error) {
	ts, err := startServer(server.Config{})
	if err != nil {
		return outputStep{}, err
	}
	defer ts.stop()
	cmd := []string{exe, "emit", "--stamp", "--rate", fmt.Sprint(rate)}
	var created struct {
		SessionID string   `json:"session_id"`
		BlockIDs  []string `json:"block_ids"`
	}
	if err := ts.call("session.create", map[string]any{"command": cmd, "cols": 120, "rows": 40}, &created); err != nil {
		return outputStep{}, err
	}
	blocks := []string{created.BlockIDs[0]}
	for i := 1; i < panes; i++ {
		var w struct {
			BlockIDs []string `json:"block_ids"`
		}
		if err := ts.call("session.new_window", map[string]any{"session_id": created.SessionID, "focus": false,
			"layout": map[string]any{"command": cmd}}, &w); err != nil {
			return outputStep{}, fmt.Errorf("pane %d: %w", i+1, err)
		}
		blocks = append(blocks, w.BlockIDs...)
	}
	r, err := attachReader(ts, blocks, cost)
	if err != nil {
		return outputStep{}, err
	}
	defer r.close()
	time.Sleep(2 * time.Second)
	r.start()
	c0, t0 := cpuTime(), time.Now()
	stopProfile := startProfile(profile)
	time.Sleep(time.Duration(secs) * time.Second)
	lat, bytes, resyncs, took := r.stop()
	r.mu.Lock()
	frames := r.frames
	r.mu.Unlock()
	cores := (cpuTime() - c0).Seconds() / time.Since(t0).Seconds()
	stopProfile()
	st := outputStep{Panes: panes, RatePerPane: rate, Latency: dist(lat), Resyncs: resyncs, CPUCores: cores, Memory: memory()}
	st.OfferedMBps = float64(rate*panes) / (1 << 20)
	st.DeliveredMBps = float64(bytes) / took.Seconds() / (1 << 20)
	st.Delivered = st.DeliveredMBps / st.OfferedMBps
	if frames > 0 {
		st.BytesPerFrame = float64(bytes) / float64(frames)
	}
	switch {
	case resyncs > 0:
		st.Degraded, st.Why = true, fmt.Sprintf("%d resyncs", resyncs)
	case st.Latency.N == 0:
		st.Degraded, st.Why = true, "no stamps arrived"
	case st.Latency.P99 > float64(limit)/float64(time.Millisecond):
		st.Degraded, st.Why = true, "latency"
	case st.Delivered < 0.9:
		st.Degraded, st.Why = true, "throughput"
	}
	return st, nil
}

// startProfile profiles the CPU, lock contention and blocking until the
// returned func, then writes those and a heap profile. With no prefix it
// does nothing.
func startProfile(prefix string) func() {
	if prefix == "" {
		return func() {}
	}
	f, err := os.Create(prefix + ".cpu.pprof")
	if err != nil {
		return func() {}
	}
	_ = pprof.StartCPUProfile(f)
	runtime.SetMutexProfileFraction(1)
	runtime.SetBlockProfileRate(int(time.Millisecond))
	return func() {
		pprof.StopCPUProfile()
		f.Close()
		for _, name := range []string{"heap", "mutex", "block"} {
			if h, err := os.Create(prefix + "." + name + ".pprof"); err == nil {
				_ = pprof.Lookup(name).WriteTo(h, 0)
				h.Close()
			}
		}
		runtime.SetMutexProfileFraction(0)
		runtime.SetBlockProfileRate(0)
	}
}

// reader is an attach client, as the app is: it subscribes to blocks and
// reads their output frames, finding the emitters' stamps.
type reader struct {
	ws      *websocket.Conn
	cancel  context.CancelFunc
	mu      sync.Mutex
	on      bool
	since   time.Time
	lat     []time.Duration
	bytes   int
	resyncs int
	tails   map[string][]byte
	done    chan struct{}
	cost    time.Duration
	frames  int
}

func attachReader(ts *testServer, blocks []string, cost time.Duration) (*reader, error) {
	sock := ts.dir + "/s.sock"
	hc := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	ws, _, err := websocket.Dial(ctx, "ws://hive/v1/attach", &websocket.DialOptions{HTTPClient: hc})
	if err != nil {
		cancel()
		return nil, err
	}
	ws.SetReadLimit(256 << 20)
	r := &reader{ws: ws, cancel: cancel, tails: map[string][]byte{}, done: make(chan struct{}), cost: cost}
	send := func(v any) error {
		b, _ := json.Marshal(v)
		return ws.Write(ctx, websocket.MessageText, b)
	}
	if err := send(map[string]any{"type": "hello", "name": "hivebench", "kind": "bench"}); err != nil {
		cancel()
		return nil, err
	}
	for _, b := range blocks {
		if err := send(map[string]any{"type": "subscribe", "block_id": b}); err != nil {
			cancel()
			return nil, err
		}
	}
	go r.loop(ctx, send)
	return r, nil
}

func (r *reader) loop(ctx context.Context, send func(any) error) {
	defer close(r.done)
	for {
		typ, data, err := r.ws.Read(ctx)
		if err != nil {
			return
		}
		now := time.Now()
		if typ == websocket.MessageText {
			var m struct {
				Type    string `json:"type"`
				BlockID string `json:"block_id"`
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "resync" {
				r.mu.Lock()
				if r.on {
					r.resyncs++
				}
				r.tails[m.BlockID] = nil
				r.mu.Unlock()
				_ = send(map[string]any{"type": "subscribe", "block_id": m.BlockID})
			}
			continue
		}
		if len(data) < 3 || data[0] != 1 { // output frames only
			continue
		}
		n := int(binary.BigEndian.Uint16(data[1:3]))
		if len(data) < 3+n+8 {
			continue
		}
		block, payload := string(data[3:3+n]), data[3+n+8:]
		if r.cost > 0 {
			for t := time.Now(); time.Since(t) < r.cost; {
			}
		}
		r.mu.Lock()
		if r.on {
			r.bytes += len(payload)
			r.frames++
		}
		stamps, tail := scanStamps(r.tails[block], payload)
		r.tails[block] = tail
		for _, ns := range stamps {
			if t := time.Unix(0, ns); r.on && t.After(r.since) {
				r.lat = append(r.lat, now.Sub(t))
			}
		}
		r.mu.Unlock()
	}
}

// start begins a measurement, from zero: the soak measures interval by
// interval, and samples kept from the last one grew its own heap.
func (r *reader) start() {
	r.mu.Lock()
	r.on, r.since = true, time.Now()
	r.lat, r.bytes, r.resyncs, r.frames = nil, 0, 0, 0
	r.mu.Unlock()
}

func (r *reader) stop() ([]time.Duration, int, int, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.on = false
	return r.lat, r.bytes, r.resyncs, time.Since(r.since)
}

func (r *reader) close() {
	r.cancel()
	r.ws.CloseNow()
	<-r.done
}

// scanStamps finds the stamps in a frame's output. A stamp can be split
// across frames: tail is what to put in front of the next frame.
func scanStamps(tail, payload []byte) (stamps []int64, next []byte) {
	const n = len(stampPrefix) + 20 // prefix, 19 digits, ';'
	buf := append(append([]byte(nil), tail...), payload...)
	for {
		i := bytes.Index(buf, []byte(stampPrefix))
		if i < 0 {
			return stamps, append([]byte(nil), buf[max(0, len(buf)-n):]...)
		}
		if len(buf) < i+n {
			return stamps, append([]byte(nil), buf[i:]...)
		}
		if ns, err := strconv.ParseInt(string(buf[i+len(stampPrefix):i+n-1]), 10, 64); err == nil {
			stamps = append(stamps, ns)
		}
		buf = buf[i+n:]
	}
}
