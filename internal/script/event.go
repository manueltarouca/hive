package script

import (
	"encoding/json"
	"reflect"

	"github.com/manueltarouca/hive/internal/events"
)

// event is a bus event as scripts see it: envelopes unwrapped, so a block's
// bell is "terminal.bell" with block_id and session_id in its payload.
type event struct {
	Seq       uint64
	Name      string
	SessionID string
	Payload   map[string]any // JSON-shaped: what the script gets
	JSON      string
}

// unwrap turns a bus event into a script event.
func unwrap(e events.Event) (event, bool) {
	raw, err := json.Marshal(e.Payload)
	if err != nil {
		return event{}, false
	}
	var p map[string]any
	if json.Unmarshal(raw, &p) != nil {
		return event{}, false
	}
	ev := event{Seq: e.Seq, Name: e.Type, Payload: p}
	if e.Type == "session_event" {
		sid, _ := p["session_id"].(string)
		inner, _ := p["event"].(map[string]any)
		name, _ := inner["type"].(string)
		payload, _ := inner["payload"].(map[string]any)
		if payload == nil {
			payload = map[string]any{}
		}
		if name == "block_event" {
			bname, _ := payload["name"].(string)
			bp, _ := payload["payload"].(map[string]any)
			if bp == nil {
				bp = map[string]any{}
			}
			bp["block_id"] = payload["block_id"]
			name, payload = "terminal."+bname, bp
		}
		if _, ok := payload["session_id"]; !ok || name == "terminal.agent_changed" {
			// agent_changed carries the harness's own session_id; the hive
			// session is hive_session_id there.
			if name == "terminal.agent_changed" {
				payload["hive_session_id"] = sid
			} else {
				payload["session_id"] = sid
			}
		}
		ev.Name, ev.SessionID, ev.Payload = name, sid, payload
	}
	out, _ := json.Marshal(ev.Payload)
	ev.JSON = string(out)
	return ev, true
}

// matches applies a Go-side filter: each key must equal the payload's
// value, or be one of an array's values.
func matches(filter, payload map[string]any) bool {
	for k, want := range filter {
		got, ok := payload[k]
		if !ok {
			return false
		}
		if list, isList := want.([]any); isList {
			if _, gotList := got.([]any); !gotList {
				found := false
				for _, w := range list {
					if reflect.DeepEqual(w, got) {
						found = true
						break
					}
				}
				if !found {
					return false
				}
				continue
			}
		}
		if !reflect.DeepEqual(want, got) {
			return false
		}
	}
	return true
}
