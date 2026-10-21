package script

import (
	"fmt"
	"runtime"
	"runtime/metrics"
	"time"
)

// goja shares the server's Go heap, so a script that keeps growing a Map
// can't be capped on its own. The watchdog samples the heap; past the
// limit over its baseline, it stops the script that ran the most in the
// last minute (scripts only allocate while they run). Its runtime is
// dropped, so the collector takes its memory back.

const (
	defaultHeapLimit = 1 << 30
	watchEvery       = time.Second
	cpuWindow        = time.Minute
)

func heapBytes() uint64 {
	s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(s)
	if s[0].Value.Kind() == metrics.KindUint64 {
		return s[0].Value.Uint64()
	}
	return 0
}

func (h *Host) watchHeap(quit <-chan struct{}) {
	limit := h.cfg.HeapLimit
	if limit == 0 {
		limit = defaultHeapLimit
	}
	runtime.GC()
	base := heapBytes()
	t := time.NewTicker(h.watchEvery())
	defer t.Stop()
	for {
		select {
		case <-quit:
			return
		case <-t.C:
		}
		if heapBytes() < base+limit {
			continue
		}
		runtime.GC() // garbage isn't a culprit
		used := heapBytes()
		if used < base+limit {
			continue
		}
		victim := h.busiest()
		if victim == nil {
			continue
		}
		why := fmt.Sprintf("the server's memory grew by %d MB while it was the busiest script", (used-base)>>20)
		h.log.Warn("script stopped for memory", "script", victim.Name, "heap_mb", used>>20)
		if res := victim.opts.Resident; res != nil && h.res != nil {
			h.res.quarantine(res, why)
		} else {
			victim.stop(why)
		}
		<-victim.done
		runtime.GC()
	}
}

func (h *Host) watchEvery() time.Duration {
	if h.cfg.watchEvery > 0 {
		return h.cfg.watchEvery
	}
	return watchEvery
}

// busiest is the run that spent the most time running JS in the last
// minute.
func (h *Host) busiest() *Run {
	h.mu.Lock()
	defer h.mu.Unlock()
	var best *Run
	var most time.Duration
	for _, r := range h.runs {
		if d := r.recentCPU(); d > most {
			best, most = r, d
		}
	}
	return best
}
