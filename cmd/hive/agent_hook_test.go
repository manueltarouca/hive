package main

import (
	"errors"
	"testing"

	"github.com/manueltarouca/hive/internal/client"
)

// Servers before agent_pid reject the field: the hook sends the report
// again without it, so reports keep working until the server restarts.
func TestReportFallsBackForOlderServers(t *testing.T) {
	var sent []map[string]any
	old := func(p map[string]any) error {
		sent = append(sent, p)
		if _, ok := p["agent_pid"]; ok {
			return &client.Error{Code: "bad_request", Message: `input: json: unknown field "agent_pid"`}
		}
		return nil
	}
	if err := sendReport(old, map[string]any{"block_id": "b", "agent_pid": 42}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 || sent[1]["agent_pid"] != nil {
		t.Fatalf("sent = %v", sent)
	}
	// Other errors aren't retried.
	sent = nil
	failing := func(p map[string]any) error { sent = append(sent, p); return errors.New("down") }
	if err := sendReport(failing, map[string]any{"agent_pid": 42}); err == nil || len(sent) != 1 {
		t.Fatalf("err = %v, sent = %v", err, sent)
	}
}

// A server older than kind refuses it too: drop it and send again, then
// agent_pid if that server is older still.
func TestSendReportDropsNewerFields(t *testing.T) {
	var sent []map[string]any
	oldest := func(p map[string]any) error {
		c := map[string]any{}
		for k, v := range p {
			c[k] = v
		}
		sent = append(sent, c)
		for _, f := range []string{"kind", "agent_pid"} {
			if _, ok := p[f]; ok {
				return &client.Error{Status: 400, Code: "bad_request", Message: `input: json: unknown field "` + f + `"`}
			}
		}
		return nil
	}
	if err := sendReport(oldest, map[string]any{"block_id": "b", "agent_pid": 42, "kind": "permission_prompt"}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 3 || sent[2]["kind"] != nil || sent[2]["agent_pid"] != nil || sent[2]["block_id"] != "b" {
		t.Fatalf("sent = %v", sent)
	}
}
