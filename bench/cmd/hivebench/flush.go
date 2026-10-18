package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/manueltarouca/hive/internal/server"
)

// The flush probe: saves × panes × scrollback. The server snapshots each
// terminal whose output changed every 30 s (and on server.flush), and
// agent.report saves state.json at once. It ramps the number of busy panes,
// with empty and with full scrollback, and times agent.report while
// snapshot saves run back to back, so a report that has to wait for one
// shows. Degraded: agent.report p99 above the limit.

type flushStep struct {
	Panes      int    `json:"panes"`
	Scrollback string `json:"scrollback"`
	Report     Dist   `json:"agent_report"`
	Save       Dist   `json:"save"`
	SaveWait   Dist   `json:"save_wait"`
	// The reports' own saves (state.json only): waiting for the save locks,
	// and the rest (s.mu, encoding, writing and syncing).
	StateWait Dist `json:"state_save_wait"`
	StateIO   Dist `json:"state_save_io"`
	Snapshots int  `json:"snapshots_per_save_max"`
	// The process\'s own CPU (getrusage), not the profile\'s guess.
	CPUCores float64 `json:"cpu_cores"`
	Memory   Memory  `json:"memory"`
	Degraded bool    `json:"degraded"`
}

type flushResult struct {
	Scenario string         `json:"scenario"`
	Meta     Meta           `json:"meta"`
	Params   map[string]any `json:"params"`
	Steps    []flushStep    `json:"steps"`
	Knee     map[string]int `json:"knee_panes"`
	Note     string         `json:"note"`
}

func flushProbe(args []string) error {
	fs := flag.NewFlagSet("flush", flag.ContinueOnError)
	secs := fs.Int("secs", 8, "seconds measured per step (after 2 s of warm-up)")
	rate := fs.Int("rate", 100<<10, "bytes a second each pane writes")
	limit := fs.Duration("limit", 50*time.Millisecond, "agent.report p99 above this is degraded")
	out := fs.String("out", "bench/results", "where results go")
	only := fs.Int("panes", 0, "run only this many panes, with empty and full scrollback (with --profile)")
	profile := fs.String("profile", "", "write cpu and heap profiles of the measured step here (prefix)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m := meta()
	res := flushResult{Scenario: "flush", Meta: m, Knee: map[string]int{},
		Params: map[string]any{"rate_bytes_per_s": *rate, "secs": *secs, "limit_ms": limit.Milliseconds(), "steps": "1 8 32 64 128 192 256"}}
	budget := ptyBudget(m)
	for _, scroll := range []struct {
		name    string
		prefill int
	}{{"empty", 0}, {"full (12 MB written first)", 12 << 20}} {
		degradedRun := 0
		steps := []int{1, 8, 32, 64, 128, 192, 256}
		if *only > 0 {
			steps = []int{*only}
		}
		for _, panes := range steps {
			if panes > budget {
				res.Note += fmt.Sprintf("stopped at %d panes: pty budget %d. ", panes, budget)
				break
			}
			prof := *profile
			if prof != "" {
				prof += map[bool]string{true: "-full", false: "-empty"}[scroll.prefill > 0]
			}
			st, err := flushStepRun(exe, panes, scroll.prefill, *rate, *secs, *limit, prof)
			if err != nil {
				return err
			}
			st.Scrollback = scroll.name
			res.Steps = append(res.Steps, st)
			fmt.Printf("%-28s panes %4d  report p50 %6.1f p99 %7.1f max %7.1f ms | save p50 %6.1f p99 %7.1f ms, ≤%d snapshots | report's save: wait p99 %6.1f, io p99 %6.1f ms | cpu %4.2f cores | rss %5.0f MB (go %4.0f, native %5.0f)%s\n",
				scroll.name, panes, st.Report.P50, st.Report.P99, st.Report.Max, st.Save.P50, st.Save.P99, st.Snapshots, st.StateWait.P99, st.StateIO.P99, st.CPUCores,
				st.Memory.ResidentMB, st.Memory.GoHeapMB, st.Memory.NativeMB, map[bool]string{true: "  DEGRADED", false: ""}[st.Degraded])
			if st.Degraded {
				degradedRun++
				if degradedRun == 2 {
					break
				}
			} else {
				degradedRun = 0
				res.Knee[scroll.name] = panes
			}
		}
	}
	res.Meta.LoadAfter = loadAvg()
	name, err := writeResult(*out, "flush", res)
	if err == nil {
		fmt.Println("result:", name)
	}
	return err
}

func flushStepRun(exe string, panes, prefill, rate, secs int, limit time.Duration, profile string) (flushStep, error) {
	var mu sync.Mutex
	var saves, waits, stateWaits, stateIO []time.Duration
	maxSnaps := 0
	recording := false
	ts, err := startServer(server.Config{OnFlush: func(f server.FlushStats) {
		mu.Lock()
		defer mu.Unlock()
		if recording {
			saves, waits = append(saves, f.Took), append(waits, f.Wait)
			if f.Snapshots == 0 {
				stateWaits, stateIO = append(stateWaits, f.Wait), append(stateIO, f.Took-f.Wait)
			}
			maxSnaps = max(maxSnaps, f.Snapshots)
		}
	}})
	if err != nil {
		return flushStep{}, err
	}
	defer ts.stop()
	cmd := []string{exe, "emit", "--rate", fmt.Sprint(rate), "--prefill", fmt.Sprint(prefill)}
	var first string
	var created struct {
		SessionID string   `json:"session_id"`
		BlockIDs  []string `json:"block_ids"`
	}
	if err := ts.call("session.create", map[string]any{"command": cmd, "cols": 120, "rows": 40}, &created); err != nil {
		return flushStep{}, err
	}
	first = created.BlockIDs[0]
	for i := 1; i < panes; i++ {
		if err := ts.call("session.new_window", map[string]any{"session_id": created.SessionID, "focus": false,
			"layout": map[string]any{"command": cmd}}, nil); err != nil {
			return flushStep{}, fmt.Errorf("pane %d: %w", i+1, err)
		}
	}
	time.Sleep(2 * time.Second) // warm-up: prefills land, saves settle into their rhythm
	mu.Lock()
	recording = true
	mu.Unlock()
	defer startProfile(profile)()
	// Snapshot saves, back to back, as the 30 s tick would run them.
	stopSaves := make(chan struct{})
	savesDone := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stopSaves:
				savesDone <- nil
				return
			default:
			}
			// A save is what's measured: on a slow disk, every pane's full
			// scrollback takes longer than a call's usual 30 s.
			if err := ts.callWithin(10*time.Minute, "server.flush", map[string]any{}, nil); err != nil {
				savesDone <- err
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	var reports []time.Duration
	c0, t0 := cpuTime(), time.Now()
	tick := time.NewTicker(20 * time.Millisecond)
	end := time.Now().Add(time.Duration(secs) * time.Second)
	for time.Now().Before(end) {
		<-tick.C
		t0 := time.Now()
		if err := ts.call("agent.report", map[string]any{"block_id": first, "harness": "claude", "state": "working", "session_id": "bench-1"}, nil); err != nil {
			return flushStep{}, err
		}
		reports = append(reports, time.Since(t0))
	}
	tick.Stop()
	cores := (cpuTime() - c0).Seconds() / time.Since(t0).Seconds()
	close(stopSaves)
	if err := <-savesDone; err != nil {
		return flushStep{}, err
	}
	mem := memory()
	mu.Lock()
	defer mu.Unlock()
	st := flushStep{Panes: panes, Report: dist(reports), Save: dist(saves), SaveWait: dist(waits), StateWait: dist(stateWaits), StateIO: dist(stateIO), Snapshots: maxSnaps, CPUCores: cores, Memory: mem}
	st.Degraded = st.Report.P99 > float64(limit)/float64(time.Millisecond)
	return st, nil
}
