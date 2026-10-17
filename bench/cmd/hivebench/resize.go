package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/manueltarouca/hive/internal/server"
)

// The resize probe: a window dragged, or two clients fighting over a size.
// One pane streams stamped output to an attach client while resizes arrive
// at a ramped rate, alternating between sizes; every resize reflows the
// terminal, scrollback included. With empty and with full scrollback.
// Degraded: resize p99 or output latency p99 over the limit, a resync, or
// the terminal not ending at the size last asked for.

type resizeStep struct {
	Scrollback string  `json:"scrollback"`
	PerSec     int     `json:"resizes_per_s"`
	Achieved   float64 `json:"resizes_per_s_achieved"` // one caller, a call at a time
	History    int     `json:"scrollback_rows"`
	Resize     Dist    `json:"resize_call"`
	Output     Dist    `json:"output_latency"`
	Delivered  float64 `json:"delivered_ratio"`
	Resyncs    int     `json:"resyncs"`
	FinalOK    bool    `json:"final_size_ok"`
	CPUCores   float64 `json:"cpu_cores"`
	Memory     Memory  `json:"memory"`
	Degraded   bool    `json:"degraded"`
	Why        string  `json:"why,omitempty"`
}

type resizeResult struct {
	Scenario string         `json:"scenario"`
	Meta     Meta           `json:"meta"`
	Params   map[string]any `json:"params"`
	Steps    []resizeStep   `json:"steps"`
	Knee     map[string]int `json:"knee_resizes_per_s"`
}

func resizeProbe(args []string) error {
	fs := flag.NewFlagSet("resize", flag.ContinueOnError)
	secs := fs.Int("secs", 6, "seconds measured per step (after 2 s of warm-up)")
	rate := fs.Int("rate", 1<<20, "bytes a second the pane writes")
	limit := fs.Duration("limit", 50*time.Millisecond, "resize or output p99 above this is degraded")
	only := fs.Int("per-sec", 0, "run only this many resizes a second (with --profile)")
	profile := fs.String("profile", "", "write profiles of the measured step here (prefix)")
	out := fs.String("out", "bench/results", "where results go")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	steps := []int{10, 60, 250, 1000}
	if *only > 0 {
		steps = []int{*only}
	}
	res := resizeResult{Scenario: "resize", Meta: meta(), Knee: map[string]int{},
		Params: map[string]any{"rate_bytes_per_s": *rate, "secs": *secs, "steps": steps, "limit_ms": limit.Milliseconds()}}
	for _, sb := range []struct {
		name    string
		prefill int
	}{{"empty", 0}, {"full (12 MB written first)", 12 << 20}} {
		degradedRun := 0
		for _, per := range steps {
			prof := *profile
			if prof != "" {
				prof += map[bool]string{true: "-full", false: "-empty"}[sb.prefill > 0]
			}
			st, err := resizeStepRun(exe, sb.prefill, *rate, per, *secs, *limit, prof)
			if err != nil {
				return err
			}
			st.Scrollback = sb.name
			res.Steps = append(res.Steps, st)
			fmt.Printf("%-27s %5d resizes/s (%4.0f done, %6d rows) | resize p50 %6.1f p99 %7.1f ms | output p50 %6.1f p99 %7.1f ms | delivered %5.1f%% | resyncs %d | final %v | cpu %4.2f%s\n",
				sb.name, per, st.Achieved, st.History, st.Resize.P50, st.Resize.P99, st.Output.P50, st.Output.P99, st.Delivered*100, st.Resyncs, st.FinalOK, st.CPUCores,
				map[bool]string{true: "  DEGRADED: " + st.Why, false: ""}[st.Degraded])
			if st.Degraded {
				if degradedRun++; degradedRun == 2 {
					break
				}
			} else {
				degradedRun = 0
				res.Knee[sb.name] = per
			}
		}
	}
	res.Meta.LoadAfter = loadAvg()
	name, err := writeResult(*out, "resize", res)
	if err == nil {
		fmt.Println("result:", name)
	}
	return err
}

func resizeStepRun(exe string, prefill, rate, perSec, secs int, limit time.Duration, profile string) (resizeStep, error) {
	ts, err := startServer(server.Config{FlushDelay: time.Hour})
	if err != nil {
		return resizeStep{}, err
	}
	defer ts.stop()
	cmd := []string{exe, "emit", "--stamp", "--rate", fmt.Sprint(rate), "--prefill", fmt.Sprint(prefill)}
	var created struct {
		BlockIDs []string `json:"block_ids"`
	}
	if err := ts.call("session.create", map[string]any{"command": cmd, "cols": 120, "rows": 40}, &created); err != nil {
		return resizeStep{}, err
	}
	block := created.BlockIDs[0]
	r, err := attachReader(ts, []string{block}, 0)
	if err != nil {
		return resizeStep{}, err
	}
	defer r.close()
	time.Sleep(2 * time.Second)
	sizes := [][2]int{{80, 24}, {200, 60}, {120, 40}, {160, 50}, {100, 30}}
	resize := func(cols, rows int) (time.Duration, error) {
		t := time.Now()
		err := ts.call("hive.terminal.resize", map[string]any{"block_id": block,
			"args": map[string]any{"cols": cols, "rows": rows, "take": true}}, nil)
		return time.Since(t), err
	}
	r.start()
	stopProfile := startProfile(profile)
	c0, t0 := cpuTime(), time.Now()
	var calls []time.Duration
	tick := time.NewTicker(time.Second / time.Duration(perSec))
	for i := 0; time.Since(t0) < time.Duration(secs)*time.Second; i++ {
		<-tick.C
		s := sizes[i%len(sizes)]
		d, err := resize(s[0], s[1])
		if err != nil {
			return resizeStep{}, err
		}
		calls = append(calls, d)
	}
	tick.Stop()
	cores := (cpuTime() - c0).Seconds() / time.Since(t0).Seconds()
	stopProfile()
	lat, bytes, resyncs, took := r.stop()
	// The size last asked for is the one the terminal ends at.
	if _, err := resize(133, 41); err != nil {
		return resizeStep{}, err
	}
	time.Sleep(200 * time.Millisecond)
	var size struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	}
	_ = ts.call("hive.terminal.size", map[string]any{"block_id": block}, &size)
	var hist struct {
		Content string `json:"content"`
	}
	_ = ts.call("hive.terminal.format", map[string]any{"block_id": block, "args": map[string]any{"format": "text"}}, &hist)
	st := resizeStep{PerSec: perSec, Achieved: float64(len(calls)) / took.Seconds(), History: strings.Count(hist.Content, "\n"), Resize: dist(calls), Output: dist(lat), Resyncs: resyncs, CPUCores: cores, Memory: memory(),
		FinalOK: size.Cols == 133 && size.Rows == 41}
	st.Delivered = float64(bytes) / took.Seconds() / float64(rate)
	lim := float64(limit) / float64(time.Millisecond)
	switch {
	case !st.FinalOK:
		st.Degraded, st.Why = true, fmt.Sprintf("ended at %dx%d, not 133x41", size.Cols, size.Rows)
	case resyncs > 0:
		st.Degraded, st.Why = true, fmt.Sprintf("%d resyncs", resyncs)
	case st.Resize.P99 > lim:
		st.Degraded, st.Why = true, "resize latency"
	case st.Output.P99 > lim:
		st.Degraded, st.Why = true, "output latency"
	case st.Delivered < 0.9:
		st.Degraded, st.Why = true, "throughput"
	}
	return st, nil
}
