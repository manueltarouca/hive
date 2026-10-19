package server

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/block"
	"github.com/manueltarouca/hive/internal/events"
	"github.com/manueltarouca/hive/internal/id"
	"github.com/manueltarouca/hive/internal/remote"
)

// The attach protocol, one WebSocket per client:
//
// Client → server, JSON text messages:
//
//	{"type":"hello","name":"Hive.app","kind":"app","platform":"darwin","version":"0.1.0"}
//	{"type":"subscribe","block_id":"block:…"}
//	{"type":"unsubscribe","block_id":"block:…"}
//	{"type":"input","block_id":"block:…","data":"ls\r"}
//	{"type":"key","block_id":"block:…","key":"ctrl+c"}
//	{"type":"resize","block_id":"block:…","cols":120,"rows":40,"take":false}
//	{"type":"call","id":7,"method":"session.view","params":{…}}
//	{"type":"events","session_id":"session:…","all":false,"since":-1}
//
// Server → client, JSON text messages:
//
//	{"type":"welcome","client_id":"client:…","version":"0.1.0"}
//	{"type":"subscribed","block_id":…,"seq":N,"cols":…,"rows":…}   followed by a snapshot frame
//	{"type":"resync","block_id":…}       the client fell behind; subscribe again
//	{"type":"exited","block_id":…}       the program exited; no more output
//	{"type":"result","id":7,"result":…} or {"type":"result","id":7,"error":{…}}
//	{"type":"event","event":{…}}
//	{"type":"error","error":{…}}
//
// Server → client, binary frames:
//
//	byte 0      kind: 1 = output, 2 = snapshot, 3 = resize
//	bytes 1-2   length of the block ID, big endian
//	…           the block ID
//	8 bytes     seq, big endian: the output position of the data's first byte
//	…           data: output bytes, a libghostty snapshot, or for a resize
//	            cols, rows, cell width px, cell height px (u16 each, big
//	            endian). A resize applies before any output at a later seq.
//
// A subscriber that feeds every output frame after a snapshot, in seq order,
// into a terminal restored from that snapshot mirrors the server's terminal.
//
// Client order of operations:
//  1. hello, then {"type":"events",...} first, and note the seq it reports,
//     so no layout change between reading state and subscribing is missed.
//  2. session.view over "call", then subscribe to each visible block.
//  3. On "resync", subscribe again; on "events_resync", re-read the view.
//  4. The client's mirror terminal must discard its own write-back replies
//     (device attributes, cursor reports): the server's terminal already
//     answered the program. Send only user input.
//  5. A block's size changes arrive as size_changed events; a client without
//     size authority letterboxes.

const (
	frameOutput   = 1
	frameSnapshot = 2
	frameResize   = 3
)

type attachMsg struct {
	Type    string          `json:"type"`
	BlockID string          `json:"block_id,omitempty"`
	Data    string          `json:"data,omitempty"`
	Key     string          `json:"key,omitempty"`
	Cols    uint16          `json:"cols,omitempty"`
	Rows    uint16          `json:"rows,omitempty"`
	CellW   uint32          `json:"cell_width_px,omitempty"`
	CellH   uint32          `json:"cell_height_px,omitempty"`
	Base64  string          `json:"data_base64,omitempty"`
	Take    bool            `json:"take,omitempty"`
	Release bool            `json:"release,omitempty"`
	ID      int64           `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	// hello
	Name     string `json:"name,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Platform string `json:"platform,omitempty"`
	Version  string `json:"version,omitempty"`
	// events
	SessionID string `json:"session_id,omitempty"`
	All       bool   `json:"all,omitempty"`
	Since     *int64 `json:"since,omitempty"`
}

type outMsg struct {
	text   []byte
	binary []byte
}

type attachConn struct {
	s      *Server
	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	client *Client
	out    chan outMsg

	mu   sync.Mutex
	subs map[string]*attachSub
	evs  *events.Sub

	// A client busy on our side is alive: handling one of its messages (a
	// pty write can block while the program isn't reading) or writing a
	// big frame to it. Neither lets a pong through in time.
	busy, writing atomic.Bool
}

type attachSub struct {
	b    *block.Block
	sub  *block.Sub
	done chan struct{}
}

// A remote client can vanish without closing (sleep, a network change):
// the server pings each attached client and drops one that doesn't answer,
// so its subscriptions and size authority don't linger. Config shortens
// these in tests.
const (
	attachPingEvery = 20 * time.Second
	attachPongWait  = 15 * time.Second
)

func (s *Server) pingTimes() (every, wait time.Duration) {
	every, wait = s.cfg.attachPingEvery, s.cfg.attachPongWait
	if every == 0 {
		every = attachPingEvery
	}
	if wait == 0 {
		wait = attachPongWait
	}
	return every, wait
}

func (s *Server) handleAttach(w http.ResponseWriter, r *http.Request) {
	// Native clients send no Origin. A browser does, and is refused: any
	// page could otherwise drive a terminal once hive listens on a network.
	if r.Header.Get("Origin") != "" {
		http.Error(w, "browser origins are not allowed", http.StatusForbidden)
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(16 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	c := &attachConn{s: s, ws: ws, ctx: ctx, cancel: cancel, out: make(chan outMsg, 256), subs: map[string]*attachSub{}}
	defer c.close()
	// Shutdown ends attached clients: http.Server.Shutdown leaves
	// connections it handed over (WebSockets) alone.
	s.attachMu.Lock()
	s.attaches[c] = struct{}{}
	s.attachMu.Unlock()
	defer func() {
		s.attachMu.Lock()
		delete(s.attaches, c)
		s.attachMu.Unlock()
	}()
	go c.writeLoop()

	if err := c.handshake(); err != nil {
		c.sendErr(api.BadRequest("%v", err))
		return
	}
	go c.keepalive()
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var m attachMsg
		if err := json.Unmarshal(data, &m); err != nil {
			c.sendErr(api.BadRequest("message: %v", err))
			continue
		}
		c.busy.Store(true)
		if f := c.s.cfg.beforeAttachMessage; f != nil {
			f(m.Type)
		}
		c.handle(m)
		c.busy.Store(false)
	}
}

// keepalive pings until the connection ends; a ping not answered in time
// ends it. The pong arrives through Read, which handleAttach keeps calling.
func (c *attachConn) keepalive() {
	every, wait := c.s.pingTimes()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(c.ctx, wait)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil && c.ctx.Err() == nil && !c.busy.Load() && !c.writing.Load() {
				c.s.log.Info("attach client stopped answering", "client", c.clientName(), "err", err)
				c.cancel()
				c.ws.CloseNow()
				return
			}
		}
	}
}

func (c *attachConn) clientName() string {
	if c.client == nil {
		return ""
	}
	return c.client.ID
}

func (c *attachConn) handshake() error {
	rctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	_, data, err := c.ws.Read(rctx)
	if err != nil {
		return err
	}
	var m attachMsg
	if err := json.Unmarshal(data, &m); err != nil || m.Type != "hello" {
		return errors.New(`the first message must be {"type":"hello",…}`)
	}
	c.client = &Client{ID: id.New(id.Client), Name: m.Name, Kind: m.Kind, Platform: m.Platform,
		Version: m.Version, Principal: remote.IdentityOf(c.ctx).Principal, ConnectedAt: time.Now().UTC()}
	if c.client.Kind == "" {
		c.client.Kind = "app"
	}
	c.s.mu.Lock()
	c.s.clients[c.client.ID] = c.client
	c.s.bus.Server("client_joined", map[string]any{"client": c.client})
	c.s.mu.Unlock()
	c.sendJSON(map[string]any{"type": "welcome", "client_id": c.client.ID, "version": Version})
	return nil
}

func (c *attachConn) close() {
	c.cancel()
	c.mu.Lock()
	for bid, as := range c.subs {
		as.b.Detach(as.sub)
		close(as.done)
		delete(c.subs, bid)
	}
	if c.evs != nil {
		c.s.bus.Unsubscribe(c.evs)
	}
	c.mu.Unlock()
	if c.client != nil {
		c.s.mu.Lock()
		delete(c.s.clients, c.client.ID)
		for _, r := range c.s.blocks {
			if r.owner == c.client.ID {
				r.owner = ""
			}
		}
		c.s.bus.Server("client_left", map[string]any{"client_id": c.client.ID})
		c.s.mu.Unlock()
	}
	_ = c.ws.Close(websocket.StatusNormalClosure, "")
}

func (c *attachConn) writeLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case m := <-c.out:
			wctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
			c.writing.Store(true)
			var err error
			if m.binary != nil {
				err = c.ws.Write(wctx, websocket.MessageBinary, m.binary)
			} else {
				err = c.ws.Write(wctx, websocket.MessageText, m.text)
			}
			c.writing.Store(false)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}

// send queues a message, blocking while the connection's queue is full. A
// slow connection therefore backs up into its block subscriptions, which
// turn the backlog into a resync instead of growing without bound.
func (c *attachConn) send(m outMsg) bool {
	select {
	case c.out <- m:
		return true
	case <-c.ctx.Done():
		return false
	}
}

func (c *attachConn) sendJSON(v any) {
	b, _ := json.Marshal(v)
	c.send(outMsg{text: b})
}

func (c *attachConn) sendErr(e *api.Error) {
	c.sendJSON(map[string]any{"type": "error", "error": e})
}

// sendErrFor reports an error about one request, naming its type and block.
func (c *attachConn) sendErrFor(m attachMsg, e *api.Error) {
	msg := map[string]any{"type": "error", "error": e, "for": m.Type}
	if m.BlockID != "" {
		msg["block_id"] = m.BlockID
	}
	c.sendJSON(msg)
}

func frame(kind byte, blockID string, seq uint64, data []byte) []byte {
	b := make([]byte, 0, 1+2+len(blockID)+8+len(data))
	b = append(b, kind)
	b = binary.BigEndian.AppendUint16(b, uint16(len(blockID)))
	b = append(b, blockID...)
	b = binary.BigEndian.AppendUint64(b, seq)
	return append(b, data...)
}

func frameFor(f block.Frame) []byte {
	if f.Resize {
		var p [8]byte
		binary.BigEndian.PutUint16(p[0:], f.Cols)
		binary.BigEndian.PutUint16(p[2:], f.Rows)
		binary.BigEndian.PutUint16(p[4:], f.CellW)
		binary.BigEndian.PutUint16(p[6:], f.CellH)
		return frame(frameResize, f.BlockID, f.Seq, p[:])
	}
	return frame(frameOutput, f.BlockID, f.Seq, f.Data)
}

// ParseFrame splits a binary attach frame. Exported for clients and tests.
func ParseFrame(b []byte) (kind byte, blockID string, seq uint64, data []byte, err error) {
	if len(b) < 3 {
		return 0, "", 0, nil, errors.New("short frame")
	}
	n := int(binary.BigEndian.Uint16(b[1:3]))
	if len(b) < 3+n+8 {
		return 0, "", 0, nil, errors.New("short frame")
	}
	return b[0], string(b[3 : 3+n]), binary.BigEndian.Uint64(b[3+n : 11+n]), b[11+n:], nil
}

func (c *attachConn) handle(m attachMsg) {
	switch m.Type {
	case "subscribe":
		c.subscribe(m.BlockID)
	case "unsubscribe":
		c.mu.Lock()
		if as := c.subs[m.BlockID]; as != nil {
			as.b.Detach(as.sub)
			close(as.done)
			delete(c.subs, m.BlockID)
		}
		c.mu.Unlock()
	case "input", "key":
		r, err := c.block(m.BlockID)
		if err == nil {
			switch {
			case m.Type == "key":
				err = r.b.SendKey(m.Key)
			case m.Base64 != "":
				var data []byte
				if data, err = base64.StdEncoding.DecodeString(m.Base64); err == nil {
					err = r.b.Write(data)
				}
			default:
				err = r.b.Write([]byte(m.Data))
			}
		}
		if err != nil {
			c.sendErrFor(m, asAPI(err))
		}
	case "resize":
		args, _ := json.Marshal(map[string]any{"cols": m.Cols, "rows": m.Rows, "client_id": c.client.ID, "take": m.Take,
			"release": m.Release, "cell_width_px": m.CellW, "cell_height_px": m.CellH})
		raw, _ := json.Marshal(blockCall{BlockID: m.BlockID, Args: args})
		if _, err := c.s.reg.Call(c.ctx, terminalPrefix+"resize", raw); err != nil {
			c.sendErrFor(m, asAPI(err))
		}
	case "call":
		// Calls run on their own, so a slow one (a wait) doesn't hold up
		// input; results carry the call's id.
		go func() {
			out, err := c.s.reg.Call(c.ctx, m.Method, m.Params)
			resp := map[string]any{"type": "result", "id": m.ID}
			if err != nil {
				resp["error"] = asAPI(err)
			} else {
				resp["result"] = out
			}
			c.sendJSON(resp)
		}()
	case "events":
		since := int64(events.Live)
		if m.Since != nil {
			since = *m.Since
		}
		c.mu.Lock()
		if c.evs != nil {
			c.s.bus.Unsubscribe(c.evs)
		}
		sub := c.s.bus.Subscribe(events.Filter{SessionID: m.SessionID, All: m.All}, since, 1024)
		c.evs = sub
		c.mu.Unlock()
		c.sendJSON(map[string]any{"type": "events_subscribed", "seq": c.s.bus.Seq()})
		if sub.Gap {
			c.sendJSON(map[string]any{"type": "events_resync", "reason": "since is older than the event history"})
		}
		filter := events.Filter{SessionID: m.SessionID, All: m.All}
		go func(sub *events.Sub) {
			for {
				for e := range sub.C {
					c.sendJSON(map[string]any{"type": "event", "event": e})
				}
				if !sub.Dropped() {
					return // closed, or replaced by a new "events"
				}
				// It fell behind. The stream goes on, live, and events_resync
				// tells the client to re-read state; subscribing first means
				// nothing after that re-read is missed. Ending the stream left
				// the app hearing nothing until it reconnected.
				c.mu.Lock()
				if c.evs != sub || c.ctx.Err() != nil {
					c.mu.Unlock()
					return
				}
				sub = c.s.bus.Subscribe(filter, events.Live, 1024)
				c.evs = sub
				c.mu.Unlock()
				c.sendJSON(map[string]any{"type": "events_resync"})
			}
		}(sub)
	default:
		c.sendErrFor(m, api.BadRequest("unknown message type %q", m.Type))
	}
}

func asAPI(err error) *api.Error {
	if e, ok := errors.AsType[*api.Error](err); ok {
		return e
	}
	return api.BadRequest("%v", err)
}

func (c *attachConn) block(blockID string) (*rec, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	return c.s.needBlock(blockID)
}

func (c *attachConn) subscribe(blockID string) {
	r, err := c.block(blockID)
	if err != nil {
		c.sendErrFor(attachMsg{Type: "subscribe", BlockID: blockID}, asAPI(err))
		return
	}
	c.mu.Lock()
	if old := c.subs[blockID]; old != nil {
		old.b.Detach(old.sub)
		close(old.done)
		delete(c.subs, blockID)
	}
	c.mu.Unlock()

	sub, snap, seq, err := r.b.Attach(512)
	if err != nil {
		c.sendErr(api.Internal("snapshot: %v", err))
		return
	}
	as := &attachSub{b: r.b, sub: sub, done: make(chan struct{})}
	c.mu.Lock()
	c.subs[blockID] = as
	c.mu.Unlock()
	cols, rows := r.b.Size()
	c.sendJSON(map[string]any{"type": "subscribed", "block_id": blockID, "seq": seq, "cols": cols, "rows": rows})
	c.send(outMsg{binary: frame(frameSnapshot, blockID, seq, snap)})

	// forward sends one frame; false ends the subscription.
	var forward func(f block.Frame) bool
	forward = func(f block.Frame) bool {
		if f.Resync {
			c.sendJSON(map[string]any{"type": "resync", "block_id": blockID})
			c.mu.Lock()
			if c.subs[blockID] == as {
				as.b.Detach(as.sub)
				delete(c.subs, blockID)
			}
			c.mu.Unlock()
			return false
		}
		if f.Resize {
			return c.send(outMsg{binary: frameFor(f)})
		}
		// Output already queued goes out as one frame.
		f, next := coalesce(f, sub.C, maxCoalesce)
		if !c.send(outMsg{binary: frameFor(f)}) {
			return false
		}
		if next != nil {
			// A resize, a resync, or output past the cap: in order.
			return forward(*next)
		}
		return true
	}
	go func() {
		for {
			select {
			case <-as.done:
				return
			case <-c.ctx.Done():
				return
			case <-r.b.Done():
				// Drain what is left, then say the block is gone.
				for {
					select {
					case f := <-sub.C:
						if !f.Resync {
							c.send(outMsg{binary: frameFor(f)})
						}
						continue
					default:
					}
					break
				}
				c.sendJSON(map[string]any{"type": "exited", "block_id": blockID})
				return
			case f := <-sub.C:
				if !forward(f) {
					return
				}
			}
		}
	}()
}

// maxCoalesce caps a merged output frame.
const maxCoalesce = 256 << 10

// coalesce merges f with the output frames already queued on q, in order,
// up to max bytes: a macOS pty gives ~1 KB a read, and a frame each cost a
// syscall on both ends. It never waits. It stops at a resize or resync, or
// at output past the cap, and returns that frame as next, taken off q.
func coalesce(f block.Frame, q <-chan block.Frame, max int) (merged block.Frame, next *block.Frame) {
	merged = f
	for {
		select {
		case g := <-q:
			if g.Resync || g.Resize || g.Seq != merged.Seq+uint64(len(merged.Data)) || len(merged.Data)+len(g.Data) > max {
				return merged, &g
			}
			if len(merged.Data) == len(f.Data) {
				// The first merge: copy, so f's data is left alone.
				merged.Data = append(make([]byte, 0, min(max, 4*(len(f.Data)+len(g.Data)))), f.Data...)
			}
			merged.Data = append(merged.Data, g.Data...)
		default:
			return merged, nil
		}
	}
}
