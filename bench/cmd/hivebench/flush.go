package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/manueltarouca/hive/internal/server"
)

// The flush probe: saves × panes × scrollback. Every second the server
// snapshots each terminal whose output changed, and agent.report saves at
// once, so an agent's report waits for every pending snapshot. It ramps
// the number of busy panes, with empty and with full scrollback, and times
// agent.report and the saves. Degraded: agent.report p99 above the limit.

type flushStep struct {
	Panes      int    `json:"panes"`
	Scrollback string `json:"scrollback"`
	Report     Dist   `json:"agent_report"`
	Save       Dist   `json:"save"`
	SaveWait   Dist   `json:"save_wait"`
	Snapshots  int    `json:"snapshots_per_save_max"`
	Memory     Memory `json:"memory"`
	Degraded   bool   `json:"degraded"`
}

type flushResult struct {
	Scenario  string               `json:"scenario"`
	Meta      Meta                 `json:"meta"`
	Params    map[string]any       `json:"params"`
	Steps     []flushStep          `json:"steps"`
	Knee      map[string]int       `json:"knee_panes"`
	Note      string               `json:"note"`
}

func flushProbe(args []string) error {
	fs := flag.NewFlagSet("flush", flag.ContinueOnError)
	secs := fs.Int("secs", 8, "seconds measured per step (after 2 s of warm-up)")
	rate := fs.Int("rate", 100<<10, "bytes a second each pane writes")
	limit := fs.Duration("limit", 50*time.Millisecond, "agent.report p99 above this is degraded")
	out := fs.String("out", "bench/results", "where results go")
	only := fs.Int("panes", 0, "run only this many panes (with --profile)")
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
			if scroll.prefill > 0 {
				continue
			}
		}
		for _, panes := range steps {
			if panes > budget {
				res.Note += fmt.Sprintf("stopped at %d panes: pty budget %d. ", panes, budget)
				break
			}
			st, err := flushStepRun(exe, panes, scroll.prefill, *rate, *secs, *limit, *profile)
			if err != nil {
				return err
			}
			st.Scrollback = scroll.name
			res.Steps = append(res.Steps, st)
			fmt.Printf("%-28s panes %4d  report p50 %6.1f p99 %7.1f max %7.1f ms | save p50 %6.1f p99 %7.1f ms, ≤%d snapshots | rss %5.0f MB (go %4.0f, native %5.0f)%s\n",
				scroll.name, panes, st.Report.P50, st.Report.P99, st.Report.Max, st.Save.P50, st.Save.P99, st.Snapshots,
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
	var saves, waits []time.Duration
	maxSnaps := 0
	recording := false
	ts, err := startServer(server.Config{OnFlush: func(f server.FlushStats) {
		mu.Lock()
		defer mu.Unlock()
		if recording {
			saves, waits = append(saves, f.Took), append(waits, f.Wait)
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
	if profile != "" {
		f, err := os.Create(profile + ".cpu.pprof")
		if err != nil {
			return flushStep{}, err
		}
		_ = pprof.StartCPUProfile(f)
		defer func() {
			pprof.StopCPUProfile()
			f.Close()
			if h, err := os.Create(profile + ".heap.pprof"); err == nil {
				_ = pprof.WriteHeapProfile(h)
				h.Close()
			}
		}()
	}
	var reports []time.Duration
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
	mem := memory()
	mu.Lock()
	defer mu.Unlock()
	st := flushStep{Panes: panes, Report: dist(reports), Save: dist(saves), SaveWait: dist(waits), Snapshots: maxSnaps, Memory: mem}
	st.Degraded = st.Report.P99 > float64(limit)/float64(time.Millisecond)
	return st, nil
}
