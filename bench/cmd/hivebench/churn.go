package main

import (
	"flag"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/manueltarouca/hive/internal/server"
)

// The churn probe: windows opened and closed, in batches, measuring memory
// after each batch. A closed pane must give back everything, its terminal
// (libghostty's native memory) included: anything left grows with every
// window ever opened. Degraded: memory per closed window over 16 KB.

type churnStep struct {
	Closed     int     `json:"windows_closed"`
	ResidentMB float64 `json:"resident_mb"`
	HeapMB     float64 `json:"go_heap_mb"`
	NativeMB   float64 `json:"native_mb"`
	Goroutines int     `json:"goroutines"`
	OpenFiles  int     `json:"open_files"`
}

type churnResult struct {
	Scenario  string         `json:"scenario"`
	Meta      Meta           `json:"meta"`
	Params    map[string]any `json:"params"`
	Steps     []churnStep    `json:"steps"`
	KBPerOpen float64        `json:"resident_kb_per_window"`
	Degraded  bool           `json:"degraded"`
}

func churnProbe(args []string) error {
	fs := flag.NewFlagSet("churn", flag.ContinueOnError)
	batches := fs.Int("batches", 5, "batches")
	per := fs.Int("per", 200, "windows opened and closed a batch")
	out := fs.String("out", "bench/results", "where results go")
	heapProf := fs.String("heap", "", "write heap profiles after the first batch and at the end here (prefix), for pprof -diff_base")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ts, err := startServer(server.Config{FlushDelay: time.Hour})
	if err != nil {
		return err
	}
	defer ts.stop()
	var created struct {
		SessionID string `json:"session_id"`
	}
	if err := ts.call("session.create", map[string]any{"command": []string{"sh", "-c", "exec cat"}, "cols": 120, "rows": 40}, &created); err != nil {
		return err
	}
	res := churnResult{Scenario: "churn", Meta: meta(), Params: map[string]any{"batches": *batches, "per": *per}}
	measure := func(closed int) churnStep {
		time.Sleep(500 * time.Millisecond) // closes finish
		heap := heapMB()
		debug.FreeOSMemory()
		m := memory()
		st := churnStep{Closed: closed, ResidentMB: m.ResidentMB, HeapMB: heap, NativeMB: m.ResidentMB - m.GoHeapMB,
			Goroutines: m.Goroutines, OpenFiles: openFiles()}
		fmt.Printf("closed %5d | rss %6.0f MB | heap %5.1f MB | native %6.0f MB | goroutines %4d | files %4d\n",
			closed, st.ResidentMB, st.HeapMB, st.NativeMB, st.Goroutines, st.OpenFiles)
		return st
	}
	res.Steps = append(res.Steps, measure(0))
	closed := 0
	for b := 0; b < *batches; b++ {
		for i := 0; i < *per; i++ {
			var w struct {
				WindowID string `json:"window_id"`
			}
			// A little output, as a real pane has, then closed.
			if err := ts.call("session.new_window", map[string]any{"session_id": created.SessionID, "focus": false,
				"layout": map[string]any{"command": []string{"sh", "-c", "seq 1 2000; exec cat"}}}, &w); err != nil {
				return err
			}
			time.Sleep(5 * time.Millisecond)
			if err := ts.call("session.close_window", map[string]any{"session_id": created.SessionID, "window_id": w.WindowID}, nil); err != nil {
				return err
			}
			closed++
		}
		res.Steps = append(res.Steps, measure(closed))
		if *heapProf != "" && b == 0 {
			writeHeap(*heapProf + ".base.pprof")
		}
	}
	if *heapProf != "" {
		writeHeap(*heapProf + ".end.pprof")
	}
	first, last := res.Steps[1], res.Steps[len(res.Steps)-1] // from after the first batch: one-off warm-up aside
	res.KBPerOpen = (last.ResidentMB - first.ResidentMB) * 1024 / float64(last.Closed-first.Closed)
	res.Degraded = res.KBPerOpen > 16
	fmt.Printf("resident memory per closed window (after the first batch): %.1f KB%s\n", res.KBPerOpen,
		map[bool]string{true: "  DEGRADED", false: ""}[res.Degraded])
	res.Meta.LoadAfter = loadAvg()
	name, err := writeResult(*out, "churn", res)
	if err == nil {
		fmt.Println("result:", name)
	}
	return err
}
