package server

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/manueltarouca/hive/internal/vt"
)

// A resize must reach subscribers inside the output stream, at its exact
// position, or a mirror that reflows at a different byte offset diverges.
func TestAttachMirrorSurvivesResizeReflow(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}, "cols": 80, "rows": 24})
	sid, bid := created["session_id"].(string), strs(created["block_ids"])[0]
	long := strings.Repeat("0123456789", 15) // wraps at 80, reflows at 50
	h.call("hive.terminal.write", map[string]any{"block_id": bid, "args": map[string]any{"data": long + "\r"}})
	h.waitScreen(sid, bid, "0123456789\n")

	c := dial(t, h, "app")
	defer c.ws.CloseNow()
	c.send(map[string]any{"type": "subscribe", "block_id": bid})
	sub := c.nextJSON()
	seq := uint64(sub["seq"].(float64))
	_, b := c.next()
	_, _, _, snap, _ := ParseFrame(b)
	mirror, err := vt.Restore(snap, vt.Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	defer mirror.Close()

	c.send(map[string]any{"type": "resize", "block_id": bid, "cols": 50, "rows": 20, "cell_width_px": 9, "cell_height_px": 18})
	c.send(map[string]any{"type": "input", "block_id": bid, "data": "after-" + long})
	c.send(map[string]any{"type": "key", "block_id": bid, "key": "enter"})

	next, resized := seq, false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		typ, b := c.next()
		if typ != websocket.MessageBinary {
			continue
		}
		kind, _, fseq, data, err := ParseFrame(b)
		if err != nil {
			t.Fatal(err)
		}
		if fseq != next {
			t.Fatalf("gap at kind %d: %d != %d", kind, fseq, next)
		}
		switch kind {
		case frameResize:
			if len(data) != 8 {
				t.Fatalf("resize frame payload %d bytes", len(data))
			}
			cols, rows := binary.BigEndian.Uint16(data[0:2]), binary.BigEndian.Uint16(data[2:4])
			cw, ch := binary.BigEndian.Uint16(data[4:6]), binary.BigEndian.Uint16(data[6:8])
			if cols != 50 || rows != 20 || cw != 9 || ch != 18 {
				t.Fatalf("resize frame %dx%d %dx%d", cols, rows, cw, ch)
			}
			mirror.ResizePx(cols, rows, uint32(cw), uint32(ch))
			resized = true
		case frameOutput:
			next += uint64(len(data))
			mirror.Feed(data)
		}
		got, _ := mirror.Format(vt.FormatOpts{Trim: true})
		if resized && strings.Count(got, "after-") >= 2 {
			if want := h.screen(sid, bid); got != want {
				t.Fatalf("mirror differs after resize:\n%q\nserver:\n%q", got, want)
			}
			return
		}
	}
	t.Fatalf("resized=%v; never saw the echo", resized)
}

func TestAttachEventsAckAndErrorAttribution(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)
	c := dial(t, h, "app")
	defer c.ws.CloseNow()
	c.send(map[string]any{"type": "events", "session_id": sid})
	if m := c.nextJSON(); m["type"] != "events_subscribed" || m["seq"] == nil {
		t.Fatalf("events ack = %v", m)
	}
	c.send(map[string]any{"type": "input", "block_id": "block:nope", "data": "x"})
	m := c.nextJSON()
	if m["type"] != "error" || m["block_id"] != "block:nope" || m["for"] != "input" {
		t.Fatalf("error = %v", m)
	}
}

func TestNewSplitAcceptsSizeAndInputBase64(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)
	split := h.call("session.new_split", map[string]any{"session_id": sid, "block_id": strs(created["block_ids"])[0],
		"cols": 77, "rows": 33, "layout": map[string]any{"command": []string{"cat"}}})
	bid := strs(split["block_ids"])[0]
	if size := h.call("hive.terminal.size", map[string]any{"block_id": bid}); size["cols"].(float64) != 77 || size["rows"].(float64) != 33 {
		t.Fatalf("size = %v", size)
	}
	c := dial(t, h, "app")
	defer c.ws.CloseNow()
	c.send(map[string]any{"type": "input", "block_id": bid, "data_base64": "YjY0LWlucHV0DQ=="}) // "b64-input\r"
	h.waitScreen(sid, bid, "b64-input")
}

func TestSessionNamesMoveAndListDir(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	created := h.call("session.create", map[string]any{"command": []string{"cat"}})
	sid := created["session_id"].(string)
	view := h.call("session.view", map[string]any{"session_id": sid})
	if label, _ := view["label"].(string); len(strings.Fields(label)) != 2 {
		t.Fatalf("unlabeled session got %q, want a two-word name", label)
	}
	a := strs(created["block_ids"])[0]
	b := strs(h.call("session.new_split", map[string]any{"session_id": sid, "block_id": a,
		"layout": map[string]any{"command": []string{"cat"}}})["block_ids"])[0]
	c := strs(h.call("session.new_split", map[string]any{"session_id": sid, "block_id": a, "direction": "vertical",
		"layout": map[string]any{"command": []string{"cat"}}})["block_ids"])[0]
	h.call("session.move_block", map[string]any{"session_id": sid, "block_id": c, "target_block_id": b, "side": "below"})
	view = h.call("session.view", map[string]any{"session_id": sid})
	layout := view["windows"].([]any)[0].(map[string]any)["layout"].(map[string]any)
	if layout["a"].(map[string]any)["block_id"] != a {
		t.Fatalf("a should now fill the left: %v", layout)
	}

	dir := t.TempDir()
	os.Mkdir(dir+"/sub1", 0o755)
	os.Mkdir(dir+"/sub2", 0o755)
	os.WriteFile(dir+"/file", nil, 0o644)
	out := h.call("hive.terminal.list_dir", map[string]any{"block_id": a, "args": map[string]any{"path": dir}})
	entries := out["entries"].([]any)
	if len(entries) != 2 || entries[0].(map[string]any)["name"] != "sub1" {
		t.Fatalf("list_dir = %v", out)
	}
}
