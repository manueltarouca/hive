package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/server"
)

// The soak: hours of a steady, moderate load, watching for anything that
// only grows. Busy panes streaming to attach clients, agent reports, title
// changes, an events subscriber, and churn: a window opened and closed
// every 10 s, a client reconnecting every 30 s. Samples every interval;
// after a warm-up (scrollback filling), fits a line to each series.
// Degraded: Go heap growing over 10 MB an hour, goroutines or open files
// growing, or an interval's output latency p99 over the limit.

type soakSample struct {
	Minute     float64 `json:"minute"`
	ResidentMB float64 `json:"resident_mb"`
	HeapMB     float64 `json:"go_heap_mb"`
	Goroutines int     `json:"goroutines"`
	OpenFiles  int     `json:"open_files"`
	Latency    Dist    `json:"latency"`
	Resyncs    int     `json:"resyncs"`
}

type soakResult struct {
	Scenario string             `json:"scenario"`
	Meta     Meta               `json:"meta"`
	Params   map[string]any     `json:"params"`
	Samples  []soakSample       `json:"samples"`
	PerHour  map[string]float64 `json:"growth_per_hour_after_warmup"`
	Degraded bool               `json:"degraded"`
	Why      string             `json:"why,omitempty"`
}

func openFiles() int { return len(globN("/dev/fd/*")) }

// slope is the least-squares growth per hour of y over minutes x.
func slope(x, y []float64) float64 {
	n := float64(len(x))
	if n < 2 {
		return 0
	}
	var sx, sy, sxy, sxx float64
	for i := range x {
		sx, sy, sxy, sxx = sx+x[i], sy+y[i], sxy+x[i]*y[i], sxx+x[i]*x[i]
	}
	d := n*sxx - sx*sx
	if d == 0 {
		return 0
	}
	return (n*sxy - sx*sy) / d * 60
}

func soakProbe(args []string) error {
	fs := flag.NewFlagSet("soak", flag.ContinueOnError)
	minutes := fs.Float64("minutes", 30, "how long")
	every := fs.Duration("every", 30*time.Second, "sample interval")
	warmup := fs.Float64("warmup", 5, "minutes before fitting growth (scrollback fills)")
	panes := fs.Int("panes", 8, "busy panes")
	rate := fs.Int("rate", 100<<10, "bytes a second each pane writes")
	limit := fs.Duration("limit", 50*time.Millisecond, "an interval's latency p99 above this is degraded")
	out := fs.String("out", "bench/results", "where results go")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	res := soakResult{Scenario: "soak", Meta: meta(), PerHour: map[string]float64{},
		Params: map[string]any{"minutes": *minutes, "every_s": every.Seconds(), "warmup_min": *warmup, "panes": *panes, "rate_bytes_per_s": *rate}}
	ts, err := startServer(server.Config{})
	if err != nil {
		return err
	}
	defer ts.stop()
	cmd := []string{exe, "emit", "--stamp", "--rate", fmt.Sprint(*rate), "--osc", "title", "--osc-rate", "1"}
	var created struct {
		SessionID string   `json:"session_id"`
		BlockIDs  []string `json:"block_ids"`
	}
	if err := ts.call("session.create", map[string]any{"command": cmd, "cols": 120, "rows": 40}, &created); err != nil {
		return err
	}
	blocks := []string{created.BlockIDs[0]}
	for i := 1; i < *panes; i++ {
		var w struct {
			BlockIDs []string `json:"block_ids"`
		}
		if err := ts.call("session.new_window", map[string]any{"session_id": created.SessionID, "focus": false,
			"layout": map[string]any{"command": cmd}}, &w); err != nil {
			return err
		}
		blocks = append(blocks, w.BlockIDs...)
	}
	steady, err := attachReader(ts, blocks, 0)
	if err != nil {
		return err
	}
	defer func() { steady.close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// An events subscriber, reading everything.
	go func() {
		c := client.New(ts.dir + "/s.sock")
		for ctx.Err() == nil {
			_ = c.Events(ctx, created.SessionID, true, -1, func(client.Event) bool { return true })
			time.Sleep(100 * time.Millisecond)
		}
	}()
	// Agent reports, window churn, client churn.
	go func() {
		report := time.NewTicker(2 * time.Second)
		churn := time.NewTicker(10 * time.Second)
		reconnect := time.NewTicker(30 * time.Second)
		defer report.Stop()
		defer churn.Stop()
		defer reconnect.Stop()
		states := []string{"working", "done", "idle"}
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			case <-report.C:
				_ = ts.call("agent.report", map[string]any{"block_id": blocks[0], "harness": "claude", "state": states[i%3], "session_id": "soak-1"}, nil)
			case <-churn.C:
				var w struct {
					WindowID string `json:"window_id"`
				}
				if ts.call("session.new_window", map[string]any{"session_id": created.SessionID, "focus": false,
					"layout": map[string]any{"command": []string{"sh", "-c", "echo churn; sleep 1"}}}, &w) == nil {
					time.Sleep(2 * time.Second)
					_ = ts.call("session.close_window", map[string]any{"session_id": created.SessionID, "window_id": w.WindowID}, nil)
				}
			case <-reconnect.C:
				if r, err := attachReader(ts, blocks, 0); err == nil {
					time.Sleep(time.Second)
					r.close()
				}
			}
		}
	}()
	start := time.Now()
	tick := time.NewTicker(*every)
	defer tick.Stop()
	steady.start()
	fmt.Printf("soak: %v, sampling every %v (%d panes at %d KB/s, churn on)\n", time.Duration(*minutes*float64(time.Minute)), *every, *panes, *rate>>10)
	for time.Since(start) < time.Duration(*minutes*float64(time.Minute)) {
		<-tick.C
		lat, _, rs, _ := steady.stop()
		steady.start()
		m := memory()
		s := soakSample{Minute: time.Since(start).Minutes(), ResidentMB: m.ResidentMB, HeapMB: heapMB(), Goroutines: m.Goroutines,
			OpenFiles: openFiles(), Latency: dist(lat), Resyncs: rs}
		res.Samples = append(res.Samples, s)
		fmt.Printf("%5.1f min | rss %6.0f MB | heap %6.1f MB | goroutines %4d | files %4d | latency p99 %6.1f ms | resyncs %d\n",
			s.Minute, s.ResidentMB, s.HeapMB, s.Goroutines, s.OpenFiles, s.Latency.P99, s.Resyncs)
		if s.Latency.P99 > float64(*limit)/float64(time.Millisecond) && !res.Degraded {
			res.Degraded, res.Why = true, fmt.Sprintf("latency p99 %.1f ms at minute %.1f", s.Latency.P99, s.Minute)
		}
	}
	var x, rss, heap, gor, files []float64
	for _, s := range res.Samples {
		if s.Minute < *warmup {
			continue
		}
		x = append(x, s.Minute)
		rss, heap = append(rss, s.ResidentMB), append(heap, s.HeapMB)
		gor, files = append(gor, float64(s.Goroutines)), append(files, float64(s.OpenFiles))
	}
	res.PerHour["resident_mb"], res.PerHour["go_heap_mb"] = slope(x, rss), slope(x, heap)
	res.PerHour["goroutines"], res.PerHour["open_files"] = slope(x, gor), slope(x, files)
	fmt.Printf("growth per hour after %.0f min: rss %+.1f MB, heap %+.1f MB, goroutines %+.1f, files %+.1f\n", *warmup,
		res.PerHour["resident_mb"], res.PerHour["go_heap_mb"], res.PerHour["goroutines"], res.PerHour["open_files"])
	switch {
	case res.Degraded:
	case res.PerHour["go_heap_mb"] > 10:
		res.Degraded, res.Why = true, "heap growing"
	case res.PerHour["goroutines"] > 5:
		res.Degraded, res.Why = true, "goroutines growing"
	case res.PerHour["open_files"] > 5:
		res.Degraded, res.Why = true, "open files growing"
	}
	if res.Degraded {
		fmt.Println("DEGRADED:", res.Why)
	}
	res.Meta.LoadAfter = loadAvg()
	name, err := writeResult(*out, "soak", res)
	if err == nil {
		fmt.Println("result:", name)
	}
	return err
}
