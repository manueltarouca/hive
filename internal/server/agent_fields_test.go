package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/manueltarouca/hive/internal/client"
)

// agent_changed carries what a rule needs: the notification's kind, a
// revision that moves on every change, when, and the folder.
func TestAgentChangedFields(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	c := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid, bid := c["session_id"].(string), strs(c["block_ids"])[0]
	since := int64(h.srv.bus.Seq())
	report := func(state, kind, msg string) {
		h.call("agent.report", map[string]any{"block_id": bid, "harness": "claude", "state": state,
			"kind": kind, "message": msg, "cwd": "/tmp/work"})
	}
	report("working", "", "")
	report("blocked", "permission_prompt", "Claude needs your permission to use Bash")
	report("blocked", "permission_prompt", "Claude needs your permission to use Bash") // no change
	report("blocked", "elicitation_dialog", "Which one?")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var got []map[string]any
	_ = h.c.Events(ctx, sid, false, since, func(e client.Event) bool {
		var p struct {
			Event struct {
				Type    string `json:"type"`
				Payload struct {
					Name    string         `json:"name"`
					Payload map[string]any `json:"payload"`
				} `json:"payload"`
			} `json:"event"`
		}
		_ = json.Unmarshal(e.Payload, &p)
		if p.Event.Payload.Name == "agent_changed" {
			got = append(got, p.Event.Payload.Payload)
		}
		return len(got) < 4
	})
	if len(got) != 4 {
		t.Fatalf("events = %v", got)
	}
	revs := []float64{}
	for _, e := range got {
		revs = append(revs, e["revision"].(float64))
		if e["cwd"] != "/tmp/work" || e["updated_at"] == nil {
			t.Fatalf("event = %v", e)
		}
	}
	if got[1]["kind"] != "permission_prompt" || got[3]["kind"] != "elicitation_dialog" {
		t.Fatalf("kinds = %v / %v", got[1]["kind"], got[3]["kind"])
	}
	// A repeat changes nothing a rule cares about: same revision.
	if !(revs[0] < revs[1] && revs[1] == revs[2] && revs[2] < revs[3]) {
		t.Fatalf("revisions = %v", revs)
	}
	// agent.list shows them too.
	var list struct {
		Agents []struct {
			Agent map[string]any `json:"agent"`
		} `json:"agents"`
	}
	raw, _ := h.c.CallRaw(context.Background(), "agent.list", nil)
	_ = json.Unmarshal(raw, &list)
	if a := list.Agents[0].Agent; a["kind"] != "elicitation_dialog" || a["revision"] != revs[3] {
		t.Fatalf("agent.list = %v", a)
	}
}
