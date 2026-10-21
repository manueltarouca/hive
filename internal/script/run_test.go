package script

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/events"
)

type fakeBackend struct {
	reg *api.Registry
	bus *events.Bus
}

func (f fakeBackend) Registry() *api.Registry            { return f.reg }
func (f fakeBackend) Bus() *events.Bus                   { return f.bus }
func (f fakeBackend) BlockSession(string) (string, bool) { return "", false }

func testHost(t *testing.T) *Host {
	reg := api.New()
	api.Register(reg, "echo", "Echo.", func(ctx context.Context, in struct {
		N int `json:"n"`
	}) (map[string]int, error) {
		return map[string]int{"n": in.N}, nil
	}, api.Perm("read"))
	api.Register(reg, "boom", "Panics.", func(ctx context.Context, _ struct{}) (struct{}, error) {
		panic("kaboom")
	}, api.Perm("read"))
	h := New(Config{Backend: fakeBackend{reg, events.New(64)}})
	t.Cleanup(h.Close)
	return h
}

func run(t *testing.T, h *Host, code string, o RunOptions) RunInfo {
	t.Helper()
	js, err := Bundle("", code, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	o.JS = js
	if o.Perms == nil {
		o.Perms = Perms{"read": true}
	}
	r, err := h.Start(o)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	info, err := h.Wait(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// The budget interrupts only the piece of JS it was armed for: a timer that
// fires just as a task ends must not hit the next one (goja's interrupt flag
// stays set until cleared).
func TestBudgetNeverLeaksIntoTheNextTask(t *testing.T) {
	h := testHost(t)
	testHookAfterTask = func() { time.Sleep(3 * time.Millisecond) } // the timer fires now
	defer func() { testHookAfterTask = nil }()
	info := run(t, h, `
let done = 0;
for (let i = 0; i < 50; i++) {
  await h.call("echo", { n: i });
  done++;
}
export default () => done;`, RunOptions{Budget: time.Millisecond})
	if !info.OK || resultOf(info) != "50" {
		t.Fatalf("info = %+v", info)
	}
}

func TestBudgetStopsARunaway(t *testing.T) {
	h := testHost(t)
	t0 := time.Now()
	info := run(t, h, `await h.sleep(1); while (true) {}`, RunOptions{Budget: 50 * time.Millisecond})
	if info.OK || info.Code != "timeout" || time.Since(t0) > time.Second {
		t.Fatalf("info = %+v after %v", info, time.Since(t0))
	}
}

func TestPanickingMethodFailsTheCall(t *testing.T) {
	h := testHost(t)
	info := run(t, h, `export default async () => { try { await h.call("boom") } catch (e: any) { return e.code } }`, RunOptions{})
	if !info.OK || !strings.Contains(resultOf(info), "internal") {
		t.Fatalf("info = %+v", info)
	}
}

func resultOf(in RunInfo) string {
	b, _ := json.Marshal(in.Result)
	return string(b)
}
