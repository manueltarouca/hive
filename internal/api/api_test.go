package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type labelIn struct {
	SessionID string   `json:"session_id" required:"true" desc:"The session."`
	BlockID   string   `json:"block_id,omitempty"`
	Label     string   `json:"label" required:"true"`
	Side      string   `json:"side,omitempty" enum:"right,left,below,above"`
	Ratio     float64  `json:"ratio,omitempty"`
	Command   []string `json:"command,omitempty"`
}
type labelOut struct {
	Revision uint64 `json:"revision"`
}

func registry() *Registry {
	r := New()
	Register(r, "session.set_label", "Set a label.", func(ctx context.Context, in labelIn) (labelOut, error) {
		if in.Label == "boom" {
			return labelOut{}, NotFound("block %q not found", in.Label)
		}
		return labelOut{Revision: 7}, nil
	})
	return r
}

func TestCallDecodesAndReturns(t *testing.T) {
	out, err := registry().Call(context.Background(), "session.set_label", json.RawMessage(`{"session_id":"s","label":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out.(labelOut).Revision != 7 {
		t.Fatalf("out = %+v", out)
	}
}

func TestCallRejectsUnknownMissingAndBadEnum(t *testing.T) {
	r := registry()
	for _, body := range []string{
		`{"session_id":"s","lable":"x"}`,
		`{"label":"x"}`,
		`{"session_id":"s","label":"x","side":"diagonal"}`,
		`not json`,
	} {
		_, err := r.Call(context.Background(), "session.set_label", json.RawMessage(body))
		var e *Error
		if !errors.As(err, &e) || e.Code != "bad_request" || e.Status != 400 {
			t.Errorf("%s: err = %v", body, err)
		}
	}
	_, err := r.Call(context.Background(), "nope", nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != "method_not_found" {
		t.Errorf("err = %v", err)
	}
	_, err = r.Call(context.Background(), "session.set_label", json.RawMessage(`{"session_id":"s","label":"boom"}`))
	if !errors.As(err, &e) || e.Code != "not_found" || e.Status != 404 {
		t.Errorf("err = %v", err)
	}
}

func TestDescribeSchema(t *testing.T) {
	m, ok := registry().Describe("session.set_label")
	if !ok {
		t.Fatal("missing")
	}
	in := m.InputSchema
	if in["type"] != "object" || in["additionalProperties"] != false {
		t.Fatalf("schema = %v", in)
	}
	req := in["required"].([]string)
	if len(req) != 2 || req[0] != "session_id" || req[1] != "label" {
		t.Fatalf("required = %v", req)
	}
	props := in["properties"].(map[string]any)
	side := props["side"].(map[string]any)
	if side["type"] != "string" || len(side["enum"].([]string)) != 4 {
		t.Fatalf("side = %v", side)
	}
	if props["ratio"].(map[string]any)["type"] != "number" ||
		props["command"].(map[string]any)["type"] != "array" ||
		props["session_id"].(map[string]any)["description"] != "The session." {
		t.Fatalf("props = %v", props)
	}
	if m.OutputSchema["properties"].(map[string]any)["revision"].(map[string]any)["type"] != "integer" {
		t.Fatalf("out = %v", m.OutputSchema)
	}
}

// A handler that panics is an internal error, not a dead server: calls also
// arrive over the attach stream and from scripts, where nothing recovers.
func TestCallRecoversPanic(t *testing.T) {
	r := New()
	Register(r, "boom", "Panics.", func(ctx context.Context, _ struct{}) (struct{}, error) {
		var m map[string]int
		m["x"] = 1
		return struct{}{}, nil
	})
	_, err := r.Call(context.Background(), "boom", nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != "internal" || e.Status != 500 {
		t.Fatalf("err = %v", err)
	}
}

func TestPermissions(t *testing.T) {
	r := New()
	Register(r, "a", "A.", func(ctx context.Context, _ struct{}) (struct{}, error) { return struct{}{}, nil }, Perm("read"))
	Register(r, "b", "B.", func(ctx context.Context, _ struct{}) (struct{}, error) { return struct{}{}, nil },
		PermBy("control", "spawn with a command", func(raw json.RawMessage) (string, error) {
			if string(raw) == `{"command":["x"]}` {
				return "spawn", nil
			}
			return "control", nil
		}))
	for _, c := range []struct{ name, raw, want string }{
		{"a", `{}`, "read"}, {"b", `{}`, "control"}, {"b", `{"command":["x"]}`, "spawn"},
	} {
		got, err := r.PermissionOf(c.name, json.RawMessage(c.raw))
		if err != nil || got != c.want {
			t.Errorf("%s %s = %q, %v; want %q", c.name, c.raw, got, err, c.want)
		}
	}
	if m, _ := r.Describe("b"); m.Permission != "control" || m.PermissionNote == "" {
		t.Errorf("describe b = %+v", m)
	}
	if _, err := r.PermissionOf("nope", nil); err == nil {
		t.Error("unknown method has a permission")
	}
}
