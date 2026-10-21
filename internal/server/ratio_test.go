package server

import (
	"context"
	"errors"
	"testing"

	"github.com/manueltarouca/hive/internal/client"
)

// A ratio outside (0, 1) is an error, not a silent 0.5: --ratio 30 used to
// mean half. Absent (0) still means the default.
func TestRatioOutOfRange(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)
	first := strs(created["block_ids"])[0]

	bad := func(method string, params map[string]any) {
		t.Helper()
		err := h.c.Call(context.Background(), method, params, nil)
		var e *client.Error
		if !errors.As(err, &e) || e.Code != "bad_request" {
			t.Fatalf("%s %v: want bad_request, got %v", method, params, err)
		}
	}
	for _, r := range []float64{-0.2, 1, 30} {
		bad("session.new_split", map[string]any{"session_id": sid, "block_id": first, "ratio": r,
			"layout": map[string]any{"command": []string{"cat"}}})
		bad("session.new_window", map[string]any{"session_id": sid, "layout": map[string]any{
			"type": "split", "ratio": r,
			"a": map[string]any{"command": []string{"cat"}}, "b": map[string]any{"command": []string{"cat"}}}})
	}

	split := h.call("session.new_split", map[string]any{"session_id": sid, "block_id": first,
		"layout": map[string]any{"command": []string{"cat"}}})
	second := strs(split["block_ids"])[0]
	h.call("session.new_split", map[string]any{"session_id": sid, "block_id": first, "ratio": 0.3,
		"layout": map[string]any{"command": []string{"cat"}}})
	for _, r := range []float64{-1, 1.5} {
		bad("session.move_block", map[string]any{"session_id": sid, "block_id": second, "target_block_id": first, "ratio": r})
	}
}
