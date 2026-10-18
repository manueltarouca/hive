package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/manueltarouca/hive/internal/server"
)

// The clients probe: fan-out. Panes streaming stamped output, and a ramped
// number of attach clients (app windows, integrations) each subscribed to
// every pane. Degraded: output latency p99 over the limit for any client,
// a resync, or a client getting under 90% of the bytes.

type clientsStep struct {
	Clients      int     `json:"clients"`
	Panes        int     `json:"panes"`
	Latency      Dist    `json:"latency_all_clients"`
	MinDelivered float64 `json:"min_delivered_ratio"`
	Resyncs      int     `json:"resyncs"`
	CPUCores     float64 `json:"cpu_cores"`
	Memory       Memory  `json:"memory"`
	Degraded     bool    `json:"degraded"`
	Why          string  `json:"why,omitempty"`
}

type clientsResult struct {
	Scenario string         `json:"scenario"`
	Meta     Meta           `json:"meta"`
	Params   map[string]any `json:"params"`
	Steps    []clientsStep  `json:"steps"`
	Knee     int            `json:"knee_clients"`
}

func clientsProbe(args []string) error {
	fs := flag.NewFlagSet("clients", flag.ContinueOnError)
	secs := fs.Int("secs", 6, "seconds measured per step (after 2 s of warm-up)")
	panes := fs.Int("panes", 4, "panes streaming")
	rate := fs.Int("rate", 256<<10, "bytes a second each pane writes")
	limit := fs.Duration("limit", 50*time.Millisecond, "latency p99 above this is degraded")
	only := fs.Int("clients", 0, "run only this many clients (with --profile)")
	profile := fs.String("profile", "", "write profiles of the measured step here (prefix)")
	out := fs.String("out", "bench/results", "where results go")
	if err := fs.Parse(args); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	steps := []int{1, 4, 16, 64, 256}
	if *only > 0 {
		steps = []int{*only}
	}
	res := clientsResult{Scenario: "clients", Meta: meta(),
		Params: map[string]any{"panes": *panes, "rate_bytes_per_s": *rate, "secs": *secs, "steps": steps, "limit_ms": limit.Milliseconds()}}
	degradedRun := 0
	for _, n := range steps {
		st, err := clientsStepRun(exe, n, *panes, *rate, *secs, *limit, *profile)
		if err != nil {
			return err
		}
		res.Steps = append(res.Steps, st)
		fmt.Printf("clients %4d × %d panes | latency p50 %6.1f p99 %7.1f max %7.1f ms | min delivered %5.1f%% | resyncs %d | cpu %4.2f | rss %5.0f MB (go %4.0f, %d goroutines)%s\n",
			n, *panes, st.Latency.P50, st.Latency.P99, st.Latency.Max, st.MinDelivered*100, st.Resyncs, st.CPUCores,
			st.Memory.ResidentMB, st.Memory.GoHeapMB, st.Memory.Goroutines, map[bool]string{true: "  DEGRADED: " + st.Why, false: ""}[st.Degraded])
		if st.Degraded {
			if degradedRun++; degradedRun == 2 {
				break
			}
		} else {
			degradedRun = 0
			res.Knee = n
		}
	}
	res.Meta.LoadAfter = loadAvg()
	name, err := writeResult(*out, "clients", res)
	if err == nil {
		fmt.Println("result:", name)
	}
	return err
}

func clientsStepRun(exe string, clients, panes, rate, secs int, limit time.Duration, profile string) (clientsStep, error) {
	ts, err := startServer(server.Config{FlushDelay: time.Hour})
	if err != nil {
		return clientsStep{}, err
	}
	defer ts.stop()
	cmd := []string{exe, "emit", "--stamp", "--rate", fmt.Sprint(rate)}
	var created struct {
		SessionID string   `json:"session_id"`
		BlockIDs  []string `json:"block_ids"`
	}
	if err := ts.call("session.create", map[string]any{"command": cmd, "cols": 120, "rows": 40}, &created); err != nil {
		return clientsStep{}, err
	}
	blocks := []string{created.BlockIDs[0]}
	for i := 1; i < panes; i++ {
		var w struct {
			BlockIDs []string `json:"block_ids"`
		}
		if err := ts.call("session.new_window", map[string]any{"session_id": created.SessionID, "focus": false,
			"layout": map[string]any{"command": cmd}}, &w); err != nil {
			return clientsStep{}, err
		}
		blocks = append(blocks, w.BlockIDs...)
	}
	var readers []*reader
	defer func() {
		for _, r := range readers {
			r.close()
		}
	}()
	for i := range clients {
		r, err := attachReader(ts, blocks, 0)
		if err != nil {
			return clientsStep{}, fmt.Errorf("client %d: %w", i+1, err)
		}
		readers = append(readers, r)
	}
	time.Sleep(2 * time.Second)
	for _, r := range readers {
		r.start()
	}
	stopProfile := startProfile(profile)
	c0, t0 := cpuTime(), time.Now()
	time.Sleep(time.Duration(secs) * time.Second)
	cores := (cpuTime() - c0).Seconds() / time.Since(t0).Seconds()
	stopProfile()
	var all []time.Duration
	minDelivered, resyncs := 10.0, 0
	for _, r := range readers {
		lat, bytes, rs, took := r.stop()
		all = append(all, lat...)
		resyncs += rs
		minDelivered = min(minDelivered, float64(bytes)/took.Seconds()/float64(rate*panes))
	}
	st := clientsStep{Clients: clients, Panes: panes, Latency: dist(all), MinDelivered: minDelivered, Resyncs: resyncs,
		CPUCores: cores, Memory: memory()}
	switch {
	case resyncs > 0:
		st.Degraded, st.Why = true, fmt.Sprintf("%d resyncs", resyncs)
	case st.Latency.P99 > float64(limit)/float64(time.Millisecond):
		st.Degraded, st.Why = true, "latency"
	case minDelivered < 0.9:
		st.Degraded, st.Why = true, "a client short of bytes"
	}
	return st, nil
}
