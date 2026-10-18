package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/server"
)

// The storm probe: a program writing OSC sequences (titles, pwd, program
// status, bells, clipboard, notifications) as fast as it likes. Each one is
// an event: the server takes its lock, publishes it, and marks state dirty.
// It ramps sequences a second per pane, for each kind, while an events
// subscriber counts what arrives and an API caller times session.list.
// Degraded: API p99 over the limit, the subscriber dropped (resync), under
// 90% of the events delivered, or the Go heap growing over 64 MB (an event
// queue that doesn't keep up grows without bound).

var oscEvents = map[string]string{
	"title": "title_changed", "pwd": "pwd_changed", "status": "program_status_changed",
	"bell": "bell", "clipboard": "clipboard_written", "notify": "desktop_notification",
}

type stormStep struct {
	Kind         string  `json:"kind"`
	Panes        int     `json:"panes"`
	RatePerPane  int     `json:"osc_per_s_per_pane"`
	Delivered    float64 `json:"events_delivered_ratio"`
	EventsPerSec float64 `json:"events_per_s"`
	Resyncs      int     `json:"subscriber_resyncs"`
	API          Dist    `json:"api_session_list"`
	Saves        int     `json:"saves"`
	CPUCores     float64 `json:"cpu_cores"`
	HeapGrowthMB float64 `json:"go_heap_growth_mb"`
	Memory       Memory  `json:"memory"`
	Degraded     bool    `json:"degraded"`
	Why          string  `json:"why,omitempty"`
}

type stormResult struct {
	Scenario string         `json:"scenario"`
	Meta     Meta           `json:"meta"`
	Params   map[string]any `json:"params"`
	Steps    []stormStep    `json:"steps"`
	Knee     map[string]int `json:"knee_osc_per_s_per_pane"`
}

func stormProbe(args []string) error {
	fs := flag.NewFlagSet("storm", flag.ContinueOnError)
	secs := fs.Int("secs", 6, "seconds measured per step (after 2 s of warm-up)")
	panes := fs.Int("panes", 1, "panes, each storming at the step's rate")
	kinds := fs.String("kinds", "title,pwd,status,bell,clipboard,notify", "OSC kinds, comma-separated")
	limit := fs.Duration("limit", 50*time.Millisecond, "API p99 above this is degraded")
	only := fs.Int("rate", 0, "run only this rate (with --profile)")
	profile := fs.String("profile", "", "write profiles of the measured step here (prefix)")
	out := fs.String("out", "bench/results", "where results go")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	rates := []int{100, 1000, 10000, 100000}
	if *only > 0 {
		rates = []int{*only}
	}
	res := stormResult{Scenario: "storm", Meta: meta(), Knee: map[string]int{},
		Params: map[string]any{"panes": *panes, "secs": *secs, "rates": rates, "limit_ms": limit.Milliseconds()}}
	for kind := range strings.SplitSeq(*kinds, ",") {
		if oscEvents[kind] == "" {
			return fmt.Errorf("unknown kind %q", kind)
		}
		degradedRun := 0
		for _, rate := range rates {
			prof := *profile
			if prof != "" {
				prof += "-" + kind
			}
			st, err := stormStepRun(exe, kind, *panes, rate, *secs, *limit, prof)
			if err != nil {
				return err
			}
			res.Steps = append(res.Steps, st)
			fmt.Printf("%-9s panes %2d  %6d/s each | delivered %5.1f%% (%7.0f events/s) | resyncs %d | api p50 %5.1f p99 %6.1f ms | saves %2d | cpu %4.2f cores | heap +%5.1f MB%s\n",
				kind, *panes, rate, st.Delivered*100, st.EventsPerSec, st.Resyncs, st.API.P50, st.API.P99, st.Saves, st.CPUCores,
				st.HeapGrowthMB, map[bool]string{true: "  DEGRADED: " + st.Why, false: ""}[st.Degraded])
			if st.Degraded {
				if degradedRun++; degradedRun == 2 {
					break
				}
			} else {
				degradedRun = 0
				res.Knee[kind] = rate
			}
		}
	}
	res.Meta.LoadAfter = loadAvg()
	name, err := writeResult(*out, "storm", res)
	if err == nil {
		fmt.Println("result:", name)
	}
	return err
}

func heapMB() float64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapAlloc) / (1 << 20)
}

func stormStepRun(exe, kind string, panes, rate, secs int, limit time.Duration, profile string) (stormStep, error) {
	var mu sync.Mutex
	saves, measuring := 0, false
	ts, err := startServer(server.Config{OnFlush: func(server.FlushStats) {
		mu.Lock()
		if measuring {
			saves++
		}
		mu.Unlock()
	}})
	if err != nil {
		return stormStep{}, err
	}
	defer ts.stop()
	cmd := []string{exe, "emit", "--rate", "0", "--osc", kind, "--osc-rate", fmt.Sprint(rate)}
	var created struct {
		SessionID string `json:"session_id"`
	}
	if err := ts.call("session.create", map[string]any{"command": cmd, "cols": 120, "rows": 40}, &created); err != nil {
		return stormStep{}, err
	}
	for i := 1; i < panes; i++ {
		if err := ts.call("session.new_window", map[string]any{"session_id": created.SessionID, "focus": false,
			"layout": map[string]any{"command": cmd}}, nil); err != nil {
			return stormStep{}, fmt.Errorf("pane %d: %w", i+1, err)
		}
	}
	// An events subscriber, as `hive events` or an integration would be.
	want := oscEvents[kind]
	var got, resyncs int
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	evDone := make(chan struct{})
	go func() {
		defer close(evDone)
		c := client.New(ts.dir + "/s.sock")
		for ctx.Err() == nil {
			err := c.Events(ctx, created.SessionID, true, -1, func(e client.Event) bool {
				mu.Lock()
				defer mu.Unlock()
				if !measuring {
					return true
				}
				switch e.Type {
				case "session_event": // {"event":{"type":"block_event","payload":{"name":…}}}
					var p struct {
						Event struct {
							Type    string `json:"type"`
							Payload struct {
								Name string `json:"name"`
							} `json:"payload"`
						} `json:"event"`
					}
					if json.Unmarshal(e.Payload, &p) == nil && p.Event.Type == "block_event" && p.Event.Payload.Name == want {
						got++
					}
				case "resync_required":
					resyncs++
				}
				return true
			})
			if err != nil {
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()
	time.Sleep(2 * time.Second)
	h0 := heapMB()
	mu.Lock()
	measuring = true
	mu.Unlock()
	stopProfile := startProfile(profile)
	c0, t0 := cpuTime(), time.Now()
	var api []time.Duration
	tick := time.NewTicker(20 * time.Millisecond)
	for end := t0.Add(time.Duration(secs) * time.Second); time.Now().Before(end); {
		<-tick.C
		t := time.Now()
		if err := ts.call("session.list", map[string]any{}, nil); err != nil {
			return stormStep{}, err
		}
		api = append(api, time.Since(t))
	}
	tick.Stop()
	wall := time.Since(t0)
	cores := (cpuTime() - c0).Seconds() / wall.Seconds()
	stopProfile()
	mu.Lock()
	measuring = false
	n, rs, sv := got, resyncs, saves
	mu.Unlock()
	cancel()
	<-evDone
	growth := heapMB() - h0
	st := stormStep{Kind: kind, Panes: panes, RatePerPane: rate, Resyncs: rs, API: dist(api), Saves: sv, CPUCores: cores,
		HeapGrowthMB: growth, Memory: memory()}
	st.EventsPerSec = float64(n) / wall.Seconds()
	st.Delivered = st.EventsPerSec / float64(rate*panes)
	switch {
	case rs > 0:
		st.Degraded, st.Why = true, fmt.Sprintf("%d subscriber resyncs", rs)
	case st.API.P99 > float64(limit)/float64(time.Millisecond):
		st.Degraded, st.Why = true, "api latency"
	case st.Delivered < 0.9:
		st.Degraded, st.Why = true, "events lost or late"
	case growth > 64:
		st.Degraded, st.Why = true, "heap growing"
	}
	return st, nil
}
