package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/server"
)

// Meta is what a result was measured on: without it, numbers from
// different machines or a busy machine would look comparable.
type Meta struct {
	When        string  `json:"when"`
	Machine     string  `json:"machine"`
	CPU         string  `json:"cpu"`
	Cores       int     `json:"cores"`
	MemoryGB    float64 `json:"memory_gb"`
	OS          string  `json:"os"`
	Go          string  `json:"go"`
	Commit      string  `json:"commit"`
	Power       string  `json:"power"`
	LoadBefore  string  `json:"load_before"`
	LoadAfter   string  `json:"load_after,omitempty"`
	PtyMax      int     `json:"pty_max"`
	PtyInUse    int     `json:"pty_in_use"`
	OpenFiles   string  `json:"open_files_limit"`
	GoMaxProcs  int     `json:"gomaxprocs"`
	FitBaseline bool    `json:"fit_for_baseline"`
	Why         string  `json:"why_not_baseline,omitempty"`
}

func sh(name string, args ...string) string {
	out, _ := exec.Command(name, args...).Output()
	return strings.TrimSpace(string(out))
}

func loadAvg() string { return strings.Trim(sh("sysctl", "-n", "vm.loadavg"), "{} ") }

func meta() Meta {
	m := Meta{When: time.Now().UTC().Format(time.RFC3339), Go: runtime.Version(), GoMaxProcs: runtime.GOMAXPROCS(0),
		Commit: sh("git", "rev-parse", "--short", "HEAD"), LoadBefore: loadAvg(), OpenFiles: sh("sh", "-c", "ulimit -n")}
	if d := sh("git", "status", "--porcelain"); d != "" {
		m.Commit += "+dirty"
	}
	if runtime.GOOS == "darwin" {
		m.Machine = sh("sysctl", "-n", "hw.model")
		m.CPU = sh("sysctl", "-n", "machdep.cpu.brand_string")
		m.Cores, _ = strconv.Atoi(sh("sysctl", "-n", "hw.ncpu"))
		b, _ := strconv.ParseFloat(sh("sysctl", "-n", "hw.memsize"), 64)
		m.MemoryGB = b / (1 << 30)
		m.OS = "macOS " + sh("sw_vers", "-productVersion")
		m.Power = strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(sh("pmset", "-g", "batt"), "\n", 2)[0], "Now drawing from '"), "'")
		m.PtyMax, _ = strconv.Atoi(sh("sysctl", "-n", "kern.tty.ptmx_max"))
		m.PtyInUse = len(globN("/dev/ttys*"))
	}
	var why []string
	if l, _ := strconv.ParseFloat(strings.Fields(m.LoadBefore + " 0")[0], 64); l > 1.5 {
		why = append(why, "load average "+m.LoadBefore)
	}
	if strings.Contains(m.Power, "Battery") {
		why = append(why, "on battery")
	}
	m.FitBaseline, m.Why = len(why) == 0, strings.Join(why, "; ")
	return m
}

func globN(p string) []string { g, _ := filepath.Glob(p); return g }

// ptyBudget is how many more terminals this machine can open, leaving room
// for the user's own.
func ptyBudget(m Meta) int {
	if m.PtyMax == 0 {
		return 1 << 30
	}
	return m.PtyMax - m.PtyInUse - 32
}

// Dist summarizes samples in milliseconds.
type Dist struct {
	N   int     `json:"n"`
	P50 float64 `json:"p50_ms"`
	P99 float64 `json:"p99_ms"`
	Max float64 `json:"max_ms"`
}

func dist(d []time.Duration) Dist {
	if len(d) == 0 {
		return Dist{}
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	ms := func(x time.Duration) float64 { return math.Round(float64(x)/1e4) / 100 }
	q := func(p float64) time.Duration { return s[int(math.Ceil(p*float64(len(s))))-1] }
	return Dist{N: len(s), P50: ms(q(0.5)), P99: ms(q(0.99)), Max: ms(s[len(s)-1])}
}

// testServer runs a hive server in-process, in its own temp dir: never the
// user's server, state or settings.
type testServer struct {
	srv  *server.Server
	c    *client.Client
	dir  string
	done chan error
}

func startServer(cfg server.Config) (*testServer, error) {
	dir, err := os.MkdirTemp("", "hivebench.")
	if err != nil {
		return nil, err
	}
	cfg.Socket, cfg.StateDir, cfg.ConfigPath = filepath.Join(dir, "s.sock"), filepath.Join(dir, "state"), ""
	srv, err := server.New(cfg)
	if err != nil {
		return nil, err
	}
	t := &testServer{srv: srv, c: client.New(cfg.Socket), dir: dir, done: make(chan error, 1)}
	go func() { t.done <- srv.Serve() }()
	for i := 0; !t.c.Ping(); i++ {
		if i > 100 {
			return nil, fmt.Errorf("server didn't start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return t, nil
}

func (t *testServer) call(method string, params any, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return t.c.Call(ctx, method, params, out)
}

func (t *testServer) stop() {
	t.srv.Shutdown()
	<-t.srv.Done()
	os.RemoveAll(t.dir)
}

// memory is the whole process's resident memory and Go's share of it:
// terminal state lives in libghostty's native memory, which Go's heap
// profile doesn't see, so native = total - Go.
type Memory struct {
	ResidentMB float64 `json:"resident_mb"`
	GoHeapMB   float64 `json:"go_heap_mb"`
	NativeMB   float64 `json:"native_mb"`
	Goroutines int     `json:"goroutines"`
}

func memory() Memory {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	rss, _ := strconv.ParseFloat(sh("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())), 64)
	m := Memory{ResidentMB: math.Round(rss / 1024), GoHeapMB: math.Round(float64(ms.HeapSys) / (1 << 20)), Goroutines: runtime.NumGoroutine()}
	m.NativeMB = math.Max(0, m.ResidentMB-m.GoHeapMB)
	return m
}

func writeResult(dir, scenario string, v any) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	name := filepath.Join(dir, fmt.Sprintf("%s-%s.json", time.Now().Format("20060102-150405"), scenario))
	return name, os.WriteFile(name, b, 0o644)
}
