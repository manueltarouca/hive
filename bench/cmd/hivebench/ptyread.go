package main

import (
	"flag"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/manueltarouca/hive/internal/block"
	"github.com/manueltarouca/hive/internal/server"
)

// The ptyread probe: the cost of reading programs' output, alone. Busy
// panes, no clients, no saves. It ramps the panes and reports pty reads,
// bytes per read and the process's own CPU time (getrusage: macOS CPU
// profiles over-count threads blocked in syscalls). Degraded: less than 90%
// of the offered bytes read.

type ptyStep struct {
	Panes        int     `json:"panes"`
	OfferedMBps  float64 `json:"offered_mb_per_s"`
	ReadMBps     float64 `json:"read_mb_per_s"`
	ReadsPerSec  float64 `json:"reads_per_s"`
	BytesPerRead float64 `json:"bytes_per_read"`
	CPUCores     float64 `json:"cpu_cores"`
	CPUPerReadUs float64 `json:"cpu_us_per_read"`
	CPUPerMBms   float64 `json:"cpu_ms_per_mb"`
	Memory       Memory  `json:"memory"`
	Degraded     bool    `json:"degraded"`
}

type ptyResult struct {
	Scenario string         `json:"scenario"`
	Meta     Meta           `json:"meta"`
	Params   map[string]any `json:"params"`
	Steps    []ptyStep      `json:"steps"`
	Knee     int            `json:"knee_panes"`
	Note     string         `json:"note,omitempty"`
}

func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func ptyProbe(args []string) error {
	fs := flag.NewFlagSet("ptyread", flag.ContinueOnError)
	secs := fs.Int("secs", 6, "seconds measured per step (after 2 s of warm-up)")
	rate := fs.Int("rate", 100<<10, "bytes a second each pane writes")
	maxPanes := fs.Int("max", 128, "stop after this many panes")
	only := fs.Int("panes", 0, "run only this many panes (with --profile)")
	profile := fs.String("profile", "", "write profiles of the measured step here (prefix)")
	out := fs.String("out", "bench/results", "where results go")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m := meta()
	steps := []int{1, 8, 32, 64, 128, 192, 256}
	if *only > 0 {
		steps = []int{*only}
	}
	res := ptyResult{Scenario: "ptyread", Meta: m, Params: map[string]any{"rate_bytes_per_s": *rate, "secs": *secs, "steps": steps}}
	degradedRun := 0
	for _, panes := range steps {
		if panes > *maxPanes || panes > ptyBudget(m) {
			res.Note += fmt.Sprintf("stopped before %d panes (max %d, pty budget %d). ", panes, *maxPanes, ptyBudget(m))
			break
		}
		st, err := ptyStepRun(exe, panes, *rate, *secs, *profile)
		if err != nil {
			return err
		}
		res.Steps = append(res.Steps, st)
		fmt.Printf("panes %4d  offered %6.2f MB/s read %6.2f | %7.0f reads/s, %6.0f B/read | cpu %4.2f cores, %6.1f µs/read, %6.1f ms/MB | rss %5.0f MB%s\n",
			panes, st.OfferedMBps, st.ReadMBps, st.ReadsPerSec, st.BytesPerRead, st.CPUCores, st.CPUPerReadUs, st.CPUPerMBms,
			st.Memory.ResidentMB, map[bool]string{true: "  DEGRADED", false: ""}[st.Degraded])
		if st.Degraded {
			if degradedRun++; degradedRun == 2 {
				break
			}
		} else {
			degradedRun = 0
			res.Knee = panes
		}
	}
	res.Meta.LoadAfter = loadAvg()
	name, err := writeResult(*out, "ptyread", res)
	if err == nil {
		fmt.Println("result:", name)
	}
	return err
}

func ptyStepRun(exe string, panes, rate, secs int, profile string) (ptyStep, error) {
	ts, err := startServer(server.Config{FlushDelay: time.Hour})
	if err != nil {
		return ptyStep{}, err
	}
	defer ts.stop()
	cmd := []string{exe, "emit", "--rate", fmt.Sprint(rate)}
	var created struct {
		SessionID string `json:"session_id"`
	}
	if err := ts.call("session.create", map[string]any{"command": cmd, "cols": 120, "rows": 40}, &created); err != nil {
		return ptyStep{}, err
	}
	for i := 1; i < panes; i++ {
		if err := ts.call("session.new_window", map[string]any{"session_id": created.SessionID, "focus": false,
			"layout": map[string]any{"command": cmd}}, nil); err != nil {
			return ptyStep{}, fmt.Errorf("pane %d: %w", i+1, err)
		}
	}
	time.Sleep(2 * time.Second)
	stopProfile := startProfile(profile)
	r0, b0 := block.ReadStats()
	c0, t0 := cpuTime(), time.Now()
	time.Sleep(time.Duration(secs) * time.Second)
	r1, b1 := block.ReadStats()
	c1, wall := cpuTime(), time.Since(t0)
	stopProfile()
	reads, bytes, cpu := float64(r1-r0), float64(b1-b0), (c1 - c0).Seconds()
	st := ptyStep{Panes: panes, Memory: memory()}
	st.OfferedMBps = float64(rate*panes) / (1 << 20)
	st.ReadMBps = bytes / wall.Seconds() / (1 << 20)
	st.ReadsPerSec = reads / wall.Seconds()
	st.CPUCores = cpu / wall.Seconds()
	if reads > 0 {
		st.BytesPerRead = bytes / reads
		st.CPUPerReadUs = cpu / reads * 1e6
	}
	if bytes > 0 {
		st.CPUPerMBms = cpu / (bytes / (1 << 20)) * 1e3
	}
	st.Degraded = st.ReadMBps < 0.9*st.OfferedMBps
	return st, nil
}
