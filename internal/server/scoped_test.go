package server

import (
	"context"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/client"
)

func TestEventsOfTypes(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	h.srv.bus.Scoped("script_log", map[string]any{"text": "hi"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var got []string
	_ = h.c.EventsOf(ctx, []string{"script_log"}, 0, func(e client.Event) bool {
		got = append(got, e.Type)
		return e.Type != "script_log"
	})
	if len(got) == 0 || got[len(got)-1] != "script_log" {
		t.Fatalf("got %v", got)
	}
}
