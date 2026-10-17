package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manueltarouca/hive/internal/agents"
	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/block"
	"github.com/manueltarouca/hive/internal/id"
	"github.com/manueltarouca/hive/internal/names"
	"github.com/manueltarouca/hive/internal/persist"
	"github.com/manueltarouca/hive/internal/session"
)

// Method names are part of the API's contract: they don't change.

type empty struct{}

type revisionOut struct {
	Revision uint64 `json:"revision"`
}

type sessionRef struct {
	SessionID string `json:"session_id" required:"true"`
}

type sessionSummary struct {
	SessionID string    `json:"session_id"`
	Label     string    `json:"label,omitempty"`
	Windows   int       `json:"windows"`
	Blocks    int       `json:"blocks"`
	Agents    int       `json:"agents"`
	Created   time.Time `json:"created_at"`
}

type newOut struct {
	SessionID     string   `json:"session_id,omitempty"`
	WindowID      string   `json:"window_id,omitempty"`
	AnchorBlockID string   `json:"anchor_block_id,omitempty"`
	BlockIDs      []string `json:"block_ids"`
	SplitIDs      []string `json:"split_ids"`
	Revision      uint64   `json:"revision"`
}

type sizeIn struct {
	Cols uint16 `json:"cols,omitempty" desc:"Width in cells. Default 120 for a new session."`
	Rows uint16 `json:"rows,omitempty" desc:"Height in cells. Default 36 for a new session."`
}

const terminalPrefix = "hive.terminal."

func (s *Server) registerMethods() {
	r := s.reg

	// ---- server ----
	api.Register(r, "server.version", "Return the server's version and where it keeps its state.",
		func(ctx context.Context, _ empty) (map[string]any, error) {
			return map[string]any{
				"hive_version": Version, "platform": platform(), "pid": os.Getpid(),
				"started_at": s.started.UTC(), "socket": s.cfg.Socket, "state_dir": s.cfg.StateDir,
			}, nil
		})
	api.Register(r, "server.shutdown", "Save every session and stop the server. Sessions are restored at the next start.",
		func(ctx context.Context, _ empty) (empty, error) {
			go func() { time.Sleep(50 * time.Millisecond); s.Shutdown() }()
			return empty{}, nil
		})
	api.Register(r, "server.flush", "Save state and snapshots to disk now.",
		func(ctx context.Context, _ empty) (empty, error) { return empty{}, s.Flush() })

	type listIn struct {
		Full bool `json:"full,omitempty" desc:"Include each method's schemas."`
	}
	api.Register(r, "api.list", "List every method the server offers.",
		func(ctx context.Context, in listIn) ([]map[string]any, error) {
			var out []map[string]any
			for _, m := range s.reg.List() {
				e := map[string]any{"name": m.Name, "description": m.Description}
				if in.Full {
					e["input_schema"], e["output_schema"] = m.InputSchema, m.OutputSchema
				}
				out = append(out, e)
			}
			return out, nil
		})
	type describeIn struct {
		Method string `json:"method" required:"true"`
	}
	api.Register(r, "api.describe", "Describe one method: its arguments and result as JSON Schema.",
		func(ctx context.Context, in describeIn) (*api.Method, error) {
			m, ok := s.reg.Describe(in.Method)
			if !ok {
				return nil, &api.Error{Status: 404, Code: "method_not_found", Message: "no method named " + in.Method}
			}
			return m, nil
		})

	// ---- targets ----
	type resolveIn struct {
		Kind      string `json:"kind" required:"true" enum:"session,window,block,client"`
		Target    string `json:"target" required:"true"`
		SessionID string `json:"session_id,omitempty" desc:"For windows and blocks: look in this session."`
		WindowID  string `json:"window_id,omitempty" desc:"For blocks: look in this window."`
	}
	type resolveOut struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id,omitempty"`
	}
	api.Register(r, "target.resolve", "Resolve a target (ID, label, position, or ID suffix) to an ID.",
		func(ctx context.Context, in resolveIn) (resolveOut, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.resolve(in.Kind, in.Target, in.SessionID, in.WindowID)
		})

	// ---- sessions ----
	api.Register(r, "session.list", "List sessions.",
		func(ctx context.Context, _ empty) (map[string][]sessionSummary, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			out := []sessionSummary{}
			for _, ss := range s.sessions {
				sum := sessionSummary{SessionID: ss.ID, Label: ss.Label, Windows: len(ss.Windows), Created: ss.Created}
				for _, bid := range ss.Blocks() {
					sum.Blocks++
					if rr := s.blocks[bid]; rr != nil && rr.st.Agent != nil && rr.st.Agent.State != "ended" {
						sum.Agents++
					}
				}
				out = append(out, sum)
			}
			return map[string][]sessionSummary{"sessions": out}, nil
		})

	type createIn struct {
		Label   string      `json:"label,omitempty"`
		Layout  *LayoutSpec `json:"layout,omitempty" desc:"Blocks of the first window. Default: one shell."`
		Cwd     string      `json:"cwd,omitempty" desc:"Shorthand for a one-block layout's cwd."`
		Command []string    `json:"command,omitempty" desc:"Shorthand for a one-block layout's command."`
		sizeIn
	}
	api.Register(r, "session.create", "Create a session with one window.",
		func(ctx context.Context, in createIn) (newOut, error) {
			spec := in.Layout
			if spec == nil {
				spec = &LayoutSpec{Cwd: in.Cwd, Command: in.Command}
			}
			cols, rows := orDefault(in.Cols, 120), orDefault(in.Rows, 36)
			s.mu.Lock()
			defer s.mu.Unlock()
			if in.Label != "" {
				for _, ss := range s.sessions {
					if strings.EqualFold(ss.Label, in.Label) {
						return newOut{}, api.Conflict("a session labeled %q already exists", in.Label)
					}
				}
			}
			label := in.Label
			if label == "" {
				// Every session gets a friendly unique name.
				label = names.New(func(n string) bool {
					for _, o := range s.sessions {
						if strings.EqualFold(o.Label, n) {
							return true
						}
					}
					return false
				})
			}
			ss := &session.Session{ID: id.New(id.Session), Label: label, Created: time.Now().UTC()}
			root, blocks, splits, err := s.buildLayout(ss.ID, spec, cols, rows)
			if err != nil {
				s.abandon(blocks)
				return newOut{}, err
			}
			w := &session.Window{ID: id.New(id.Window), Root: root, Focus: firstOf(blocks)}
			ss.Windows, ss.Active = []*session.Window{w}, w.ID
			s.sessions = append(s.sessions, ss)
			s.bus.Server("session_created", map[string]any{"session": map[string]any{"session_id": ss.ID, "label": ss.Label}})
			s.bus.Session(ss.ID, "window_created", map[string]any{"window_id": w.ID, "label": w.Label})
			s.changed(ss)
			return newOut{SessionID: ss.ID, WindowID: w.ID, BlockIDs: blocks, SplitIDs: nonNil(splits), Revision: ss.Revision}, nil
		})

	api.Register(r, "session.view", "Return the session's windows, layouts, and blocks.",
		func(ctx context.Context, in sessionRef) (json.RawMessage, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			ss, err := s.needSession(in.SessionID)
			if err != nil {
				return nil, err
			}
			blocks := map[string]any{}
			for _, bid := range ss.Blocks() {
				if rr := s.blocks[bid]; rr != nil {
					blocks[bid] = s.blockInfo(bid, rr)
				}
			}
			return copyJSON(map[string]any{
				"session_id": ss.ID, "label": ss.Label, "revision": ss.Revision,
				"active_window_id": ss.ActiveWindow().ID, "windows": ss.Windows, "blocks": blocks,
			})
		})

	api.Register(r, "session.list_blocks", "List every block in the session.",
		func(ctx context.Context, in sessionRef) (json.RawMessage, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			ss, err := s.needSession(in.SessionID)
			if err != nil {
				return nil, err
			}
			out := []map[string]any{}
			for _, bid := range ss.Blocks() {
				if rr := s.blocks[bid]; rr != nil {
					info := s.blockInfo(bid, rr)
					info["placed"] = true
					out = append(out, info)
				}
			}
			return copyJSON(map[string]any{"blocks": out})
		})

	api.Register(r, "session.set_label", "Set the session's label.",
		func(ctx context.Context, in struct {
			SessionID string `json:"session_id" required:"true"`
			Label     string `json:"label" required:"true"`
		}) (revisionOut, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			ss, err := s.needSession(in.SessionID)
			if err != nil {
				return revisionOut{}, err
			}
			ss.Label = in.Label
			s.bus.Session(ss.ID, "session_label_changed", map[string]any{"label": in.Label})
			s.changed(ss)
			return revisionOut{ss.Revision}, nil
		})

	api.Register(r, "session.close", "Close a session and every block in it. It is not restored.",
		func(ctx context.Context, in sessionRef) (empty, error) {
			s.mu.Lock()
			ss, err := s.needSession(in.SessionID)
			if err != nil {
				s.mu.Unlock()
				return empty{}, err
			}
			bids := ss.Blocks()
			s.mu.Unlock()
			for _, bid := range bids {
				_ = s.closeBlock(bid)
			}
			return empty{}, nil
		})

	// ---- windows ----
	type newWindowIn struct {
		SessionID string      `json:"session_id" required:"true"`
		Layout    *LayoutSpec `json:"layout,omitempty"`
		Label     string      `json:"label,omitempty"`
		Focus     *bool       `json:"focus,omitempty" desc:"Make it the active window. Default true."`
		sizeIn
	}
	api.Register(r, "session.new_window", "Create a window from a layout.",
		func(ctx context.Context, in newWindowIn) (newOut, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			ss, err := s.needSession(in.SessionID)
			if err != nil {
				return newOut{}, err
			}
			cols, rows := in.Cols, in.Rows
			if cols == 0 || rows == 0 {
				cols, rows = s.windowSize(ss.ActiveWindow())
			}
			root, blocks, splits, err := s.buildLayout(ss.ID, in.Layout, cols, rows)
			if err != nil {
				s.abandon(blocks)
				return newOut{}, err
			}
			w := &session.Window{ID: id.New(id.Window), Label: in.Label, Root: root, Focus: firstOf(blocks)}
			ss.Windows = append(ss.Windows, w)
			s.bus.Session(ss.ID, "window_created", map[string]any{"window_id": w.ID, "label": w.Label})
			if in.Focus == nil || *in.Focus {
				ss.Active = w.ID
				s.bus.Session(ss.ID, "active_window_changed", map[string]any{"window_id": w.ID})
			}
			s.changed(ss)
			return newOut{WindowID: w.ID, BlockIDs: blocks, SplitIDs: nonNil(splits), Revision: ss.Revision}, nil
		})

	type windowRef struct {
		SessionID string `json:"session_id" required:"true"`
		WindowID  string `json:"window_id" required:"true"`
	}
	api.Register(r, "session.close_window", "Close a window and every block in it.",
		func(ctx context.Context, in windowRef) (empty, error) {
			s.mu.Lock()
			ss, err := s.needSession(in.SessionID)
			if err != nil {
				s.mu.Unlock()
				return empty{}, err
			}
			w := ss.Window(in.WindowID)
			if w == nil {
				s.mu.Unlock()
				return empty{}, api.NotFound("no window %s", in.WindowID)
			}
			bids := w.Blocks()
			s.mu.Unlock()
			for _, bid := range bids {
				_ = s.closeBlock(bid)
			}
			return empty{}, nil
		})
	api.Register(r, "session.focus_window", "Make a window the active one.",
		func(ctx context.Context, in windowRef) (revisionOut, error) {
			return s.withSession(in.SessionID, func(ss *session.Session) error {
				if ss.Window(in.WindowID) == nil {
					return api.NotFound("no window %s", in.WindowID)
				}
				ss.Active = in.WindowID
				s.bus.Session(ss.ID, "active_window_changed", map[string]any{"window_id": in.WindowID})
				return nil
			})
		})
	type stepIn struct {
		SessionID string `json:"session_id" required:"true"`
	}
	for _, step := range []struct {
		name, desc string
		d          int
	}{
		{"session.focus_next_window", "Focus the window after the active one, wrapping to the first.", 1},
		{"session.focus_previous_window", "Focus the window before the active one, wrapping to the last.", -1},
	} {
		api.Register(r, step.name, step.desc,
			func(ctx context.Context, in stepIn) (revisionOut, error) {
				return s.withSession(in.SessionID, func(ss *session.Session) error {
					if len(ss.Windows) == 0 {
						return nil
					}
					i := indexOf(ss, ss.ActiveWindow().ID)
					ss.Active = ss.Windows[(i+step.d+len(ss.Windows))%len(ss.Windows)].ID
					s.bus.Session(ss.ID, "active_window_changed", map[string]any{"window_id": ss.Active})
					return nil
				})
			})
	}
	api.Register(r, "session.set_window_label", "Set a window's label.",
		func(ctx context.Context, in struct {
			SessionID string `json:"session_id" required:"true"`
			WindowID  string `json:"window_id" required:"true"`
			Label     string `json:"label" required:"true"`
		}) (revisionOut, error) {
			return s.withSession(in.SessionID, func(ss *session.Session) error {
				w := ss.Window(in.WindowID)
				if w == nil {
					return api.NotFound("no window %s", in.WindowID)
				}
				w.Label = in.Label
				s.bus.Session(ss.ID, "window_label_changed", map[string]any{"window_id": w.ID, "label": in.Label})
				return nil
			})
		})

	// ---- blocks and layout ----
	type newSplitIn struct {
		SessionID string      `json:"session_id" required:"true"`
		BlockID   string      `json:"block_id" required:"true" desc:"The anchor block to split."`
		Direction string      `json:"direction,omitempty" enum:"horizontal,vertical" desc:"horizontal puts the new part beside the anchor, vertical above or below it. Default horizontal."`
		Side      string      `json:"side,omitempty" enum:"before,after" desc:"Where the new part goes relative to the anchor. Default after."`
		Ratio     float64     `json:"ratio,omitempty" desc:"The new part's share, 0 to 1. Default 0.5."`
		Focus     *bool       `json:"focus,omitempty" desc:"Focus the new block. Default true."`
		Layout    *LayoutSpec `json:"layout,omitempty" desc:"The blocks to create. Default: one shell."`
		sizeIn
	}
	api.Register(r, "session.new_split", "Create a split beside an existing block.",
		func(ctx context.Context, in newSplitIn) (newOut, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			ss, err := s.needSession(in.SessionID)
			if err != nil {
				return newOut{}, err
			}
			w := ss.WindowOf(in.BlockID)
			anchor := s.blocks[in.BlockID]
			if w == nil || anchor == nil {
				return newOut{}, api.NotFound("block %s is not placed in session %s", in.BlockID, in.SessionID)
			}
			side := sideFor(in.Direction, in.Side)
			ratio := in.Ratio
			if ratio <= 0 || ratio >= 1 {
				ratio = 0.5
			}
			cols, rows := anchor.b.Size()
			dir, after, _ := session.SideDirection(side)
			newShare := ratio
			// The anchor keeps the rest of its space.
			ac, ar, nc, nr := splitSize(dir, 1-newShare, cols, rows)
			if !after {
				nc, nr, ac, ar = splitSize(dir, newShare, cols, rows)
			}
			// A client that knows the pixel geometry passes the new part's
			// size, so it doesn't start at a guess and reflow at once.
			if in.Cols > 0 && in.Rows > 0 {
				nc, nr = in.Cols, in.Rows
			}
			sub, blocks, splits, err := s.buildLayout(ss.ID, in.Layout, nc, nr)
			if err != nil {
				s.abandon(blocks)
				return newOut{}, err
			}
			sid := id.New(id.Split)
			if _, err := w.Split(in.BlockID, side, ratio, sub, sid); err != nil {
				s.abandon(blocks)
				return newOut{}, api.BadRequest("%v", err)
			}
			_ = anchor.b.Resize(ac, ar)
			if in.Focus == nil || *in.Focus {
				w.Focus = firstOf(blocks)
			}
			s.changed(ss)
			return newOut{AnchorBlockID: in.BlockID, WindowID: w.ID, BlockIDs: blocks,
				SplitIDs: append([]string{sid}, splits...), Revision: ss.Revision}, nil
		})

	type blockRef struct {
		SessionID string `json:"session_id" required:"true"`
		BlockID   string `json:"block_id" required:"true"`
	}
	api.Register(r, "block.close", "Close a block within a session.",
		func(ctx context.Context, in blockRef) (empty, error) {
			s.mu.Lock()
			rr, err := s.needBlock(in.BlockID)
			if err == nil && rr.st.SessionID != in.SessionID {
				err = api.NotFound("block %s is not in session %s", in.BlockID, in.SessionID)
			}
			s.mu.Unlock()
			if err != nil {
				return empty{}, err
			}
			return empty{}, s.closeBlock(in.BlockID)
		})
	api.Register(r, "session.describe_block", "Return a block's placement, state, and the methods it offers.",
		func(ctx context.Context, in blockRef) (json.RawMessage, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			rr, err := s.needBlock(in.BlockID)
			if err != nil {
				return nil, err
			}
			info := s.blockInfo(in.BlockID, rr)
			var methods []string
			for _, m := range s.reg.List() {
				if strings.HasPrefix(m.Name, terminalPrefix) {
					methods = append(methods, m.Name)
				}
			}
			info["methods"] = methods
			return copyJSON(info)
		})
	api.Register(r, "session.focus_block", "Focus a block, and its window.",
		func(ctx context.Context, in blockRef) (revisionOut, error) {
			return s.withSession(in.SessionID, func(ss *session.Session) error {
				w := ss.WindowOf(in.BlockID)
				if w == nil {
					return api.NotFound("block %s is not in session %s", in.BlockID, in.SessionID)
				}
				w.Focus, ss.Active = in.BlockID, w.ID
				return nil
			})
		})
	api.Register(r, "session.focus_direction", "Focus the nearest block in a direction.",
		func(ctx context.Context, in struct {
			SessionID string `json:"session_id" required:"true"`
			BlockID   string `json:"block_id,omitempty" desc:"From this block. Default: the focused one."`
			Direction string `json:"direction" required:"true" enum:"left,right,up,down"`
		}) (map[string]any, error) {
			var to string
			rev, err := s.withSession(in.SessionID, func(ss *session.Session) error {
				w := ss.ActiveWindow()
				from := in.BlockID
				if from != "" {
					w = ss.WindowOf(from)
				}
				if w == nil {
					return api.NotFound("no such block or window")
				}
				if from == "" {
					from = w.Focus
				}
				if to = w.Neighbor(from, in.Direction); to != "" {
					w.Focus = to
				}
				return nil
			})
			return map[string]any{"block_id": to, "revision": rev.Revision}, err
		})
	for _, step := range []struct {
		name, desc string
		d          int
	}{
		{"session.focus_next_pane", "Focus the next pane in the active window, in layout order, wrapping to the first.", 1},
		{"session.focus_previous_pane", "Focus the previous pane in the active window, in layout order, wrapping to the last.", -1},
	} {
		api.Register(r, step.name, step.desc,
			func(ctx context.Context, in stepIn) (revisionOut, error) {
				return s.withSession(in.SessionID, func(ss *session.Session) error {
					w := ss.ActiveWindow()
					if w == nil {
						return nil
					}
					bs := w.Blocks()
					i := 0
					for j, b := range bs {
						if b == w.Focus {
							i = j
						}
					}
					w.Focus = bs[(i+step.d+len(bs))%len(bs)]
					return nil
				})
			})
	}
	api.Register(r, "session.zoom_block", "Toggle or set whether a block fills its window.",
		func(ctx context.Context, in struct {
			SessionID string `json:"session_id" required:"true"`
			BlockID   string `json:"block_id" required:"true"`
			Zoom      *bool  `json:"zoom,omitempty" desc:"Omit to toggle."`
		}) (map[string]any, error) {
			var zoomed bool
			rev, err := s.withSession(in.SessionID, func(ss *session.Session) error {
				w := ss.WindowOf(in.BlockID)
				if w == nil {
					return api.NotFound("block %s is not in session %s", in.BlockID, in.SessionID)
				}
				if in.Zoom != nil {
					w.Zoomed = *in.Zoom
				} else {
					w.Zoomed = !w.Zoomed || w.Focus != in.BlockID
				}
				w.Focus, zoomed = in.BlockID, w.Zoomed
				return nil
			})
			return map[string]any{"zoomed": zoomed, "revision": rev.Revision}, err
		})
	api.Register(r, "session.resize_pane", "Move the border between a block and its neighbor.",
		func(ctx context.Context, in struct {
			SessionID string  `json:"session_id" required:"true"`
			BlockID   string  `json:"block_id" required:"true"`
			Direction string  `json:"direction" required:"true" enum:"left,right,up,down"`
			Amount    float64 `json:"amount,omitempty" desc:"Fraction of the window, 0 to 1. Default 0.05."`
		}) (revisionOut, error) {
			return s.withSession(in.SessionID, func(ss *session.Session) error {
				w := ss.WindowOf(in.BlockID)
				if w == nil {
					return api.NotFound("block %s is not in session %s", in.BlockID, in.SessionID)
				}
				amt := in.Amount
				if amt <= 0 {
					amt = 0.05
				}
				if err := w.ResizePane(in.BlockID, in.Direction, amt); err != nil {
					return api.BadRequest("%v", err)
				}
				return nil
			})
		})
	api.Register(r, "session.move_block", "Move a block into a new split beside another block.",
		func(ctx context.Context, in struct {
			SessionID     string  `json:"session_id" required:"true"`
			BlockID       string  `json:"block_id" required:"true"`
			TargetBlockID string  `json:"target_block_id" required:"true" desc:"The block to put it beside."`
			Side          string  `json:"side,omitempty" enum:"right,left,below,above" desc:"Default right."`
			Ratio         float64 `json:"ratio,omitempty" desc:"The moved block's share, 0 to 1. Default 0.5."`
		}) (revisionOut, error) {
			return s.withSession(in.SessionID, func(ss *session.Session) error {
				w := ss.WindowOf(in.BlockID)
				if w == nil || !w.Has(in.TargetBlockID) {
					return api.NotFound("both blocks must be in one window of session %s", in.SessionID)
				}
				if err := w.Move(in.BlockID, in.TargetBlockID, in.Side, in.Ratio, id.New(id.Split)); err != nil {
					return api.BadRequest("%v", err)
				}
				return nil
			})
		})
	api.Register(r, "session.set_block_label", "Update a block's label.",
		func(ctx context.Context, in struct {
			SessionID string `json:"session_id" required:"true"`
			BlockID   string `json:"block_id" required:"true"`
			Label     string `json:"label" required:"true"`
		}) (revisionOut, error) {
			return s.withSession(in.SessionID, func(ss *session.Session) error {
				rr := s.blocks[in.BlockID]
				if rr == nil || rr.st.SessionID != ss.ID {
					return api.NotFound("block %s is not in session %s", in.BlockID, in.SessionID)
				}
				rr.st.Label = in.Label
				return nil
			})
		})

	// ---- clients ----
	api.Register(r, "client.list", "List connected clients.",
		func(ctx context.Context, _ empty) (map[string][]*Client, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			out := []*Client{}
			for _, c := range s.clients {
				out = append(out, c)
			}
			return map[string][]*Client{"clients": out}, nil
		})

	s.registerTerminalMethods()
	s.registerAgentMethods()
}

// withSession runs fn on a session under the lock and bumps its revision.
func (s *Server) withSession(sessionID string, fn func(*session.Session) error) (revisionOut, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss, err := s.needSession(sessionID)
	if err != nil {
		return revisionOut{}, err
	}
	if err := fn(ss); err != nil {
		return revisionOut{}, err
	}
	s.changed(ss)
	return revisionOut{ss.Revision}, nil
}

func (s *Server) windowSize(w *session.Window) (uint16, uint16) {
	if w == nil {
		return 120, 36
	}
	var cols, rows uint16
	rects := w.Rects()
	for bid, rc := range rects {
		if rr := s.blocks[bid]; rr != nil {
			c, r := rr.b.Size()
			cols = max(cols, uint16(float64(c)/max(rc.W, 0.01)+0.5))
			rows = max(rows, uint16(float64(r)/max(rc.H, 0.01)+0.5))
		}
	}
	return orDefault(cols, 120), orDefault(rows, 36)
}

func (s *Server) resolve(kind, target, sessionID, windowID string) (struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
}, error) {
	type out = struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id,omitempty"`
	}
	conv := func(err error) error {
		if err == nil {
			return nil
		}
		var nf *session.NotFoundError
		var amb *session.AmbiguousError
		switch {
		case errors.As(err, &nf):
			return api.NotFound("%v", err)
		case errors.As(err, &amb):
			var m []map[string]string
			for _, it := range amb.Matches {
				m = append(m, map[string]string{"id": it.ID, "label": it.Label})
			}
			return api.Ambiguous(m, "%v", err)
		}
		return api.BadRequest("%v", err)
	}
	switch kind {
	case "session":
		got, err := session.Resolve("session", s.sessionItems(), target)
		return out{ID: got, SessionID: got}, conv(err)
	case "window":
		ss, err := s.needSession(sessionID)
		if err != nil {
			return out{}, err
		}
		var items []session.Item
		for _, w := range ss.Windows {
			items = append(items, session.Item{ID: w.ID, Label: w.Label})
		}
		got, err := session.Resolve("window", items, target)
		return out{ID: got, SessionID: ss.ID}, conv(err)
	case "block":
		var items []session.Item
		var scope []string
		if sessionID != "" {
			ss, err := s.needSession(sessionID)
			if err != nil {
				return out{}, err
			}
			if windowID != "" {
				w := ss.Window(windowID)
				if w == nil {
					return out{}, api.NotFound("no window %s", windowID)
				}
				scope = w.Blocks()
			} else {
				scope = ss.Blocks()
			}
		} else {
			for _, ss := range s.sessions {
				scope = append(scope, ss.Blocks()...)
			}
		}
		for _, bid := range scope {
			if rr := s.blocks[bid]; rr != nil {
				items = append(items, session.Item{ID: bid, Label: rr.st.Label})
			}
		}
		got, err := session.Resolve("block", items, target)
		if err != nil {
			return out{}, conv(err)
		}
		return out{ID: got, SessionID: s.blocks[got].st.SessionID}, nil
	case "client":
		var items []session.Item
		for _, c := range s.clients {
			items = append(items, session.Item{ID: c.ID, Label: c.Name})
		}
		got, err := session.Resolve("client", items, target)
		return out{ID: got}, conv(err)
	}
	return out{}, api.BadRequest("unknown kind %q", kind)
}

// ---- terminal methods ----

// blockCall is the input of every hive.terminal.* method.
type blockCall struct {
	SessionID string          `json:"session_id,omitempty"`
	BlockID   string          `json:"block_id" required:"true"`
	Args      json.RawMessage `json:"args,omitempty"`
}

func (s *Server) terminal(name, desc string, run func(r *rec, args json.RawMessage) (any, error)) {
	api.Register(s.reg, terminalPrefix+name, desc, func(ctx context.Context, in blockCall) (any, error) {
		s.mu.Lock()
		rr, err := s.needBlock(in.BlockID)
		if err == nil && in.SessionID != "" && rr.st.SessionID != in.SessionID {
			err = api.NotFound("block %s is not in session %s", in.BlockID, in.SessionID)
		}
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return run(rr, in.Args)
	})
}

func args[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 || string(raw) == "null" {
		return v, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, api.BadRequest("args: %v", err)
	}
	return v, nil
}

func (s *Server) registerTerminalMethods() {
	s.terminal("format", "Return the screen and scrollback as text or VT sequences. args: {format: text|vt, trim, unwrap}",
		func(r *rec, raw json.RawMessage) (any, error) {
			a, err := args[struct {
				Format string `json:"format"`
				Trim   bool   `json:"trim"`
				Unwrap bool   `json:"unwrap"`
			}](raw)
			if err != nil {
				return nil, err
			}
			out, err := r.b.Format(a.Format, a.Trim, a.Unwrap)
			if err != nil {
				return nil, api.BadRequest("%v", err)
			}
			return map[string]any{"content": out}, nil
		})
	s.terminal("title", "Return the terminal's title.", func(r *rec, _ json.RawMessage) (any, error) {
		return map[string]any{"title": r.b.Title()}, nil
	})
	s.terminal("pwd", "Return the working directory the program last reported.", func(r *rec, _ json.RawMessage) (any, error) {
		return map[string]any{"pwd": r.b.Pwd()}, nil
	})
	s.terminal("size", "Return the terminal's size in cells.", func(r *rec, _ json.RawMessage) (any, error) {
		c, rr := r.b.Size()
		return map[string]any{"cols": c, "rows": rr, "size_owner": r.owner}, nil
	})
	s.terminal("process", "Describe the terminal's foreground process.", func(r *rec, _ json.RawMessage) (any, error) {
		fg, err := r.b.Foreground()
		out := map[string]any{"child_pid": r.b.PID()}
		if err == nil {
			out["foreground"] = fg
		}
		return out, nil
	})
	s.terminal("write", "Write text to the terminal as if it were typed. args: {data} or {data_base64}",
		func(r *rec, raw json.RawMessage) (any, error) {
			a, err := args[struct {
				Data   string `json:"data"`
				Base64 string `json:"data_base64"`
			}](raw)
			if err != nil {
				return nil, err
			}
			data := []byte(a.Data)
			if a.Base64 != "" {
				if data, err = base64.StdEncoding.DecodeString(a.Base64); err != nil {
					return nil, api.BadRequest("data_base64: %v", err)
				}
			}
			return map[string]any{"written": len(data)}, r.b.Write(data)
		})
	s.terminal("send_key", "Press keys, such as ctrl+c or enter, encoded for the terminal's modes. args: {keys: [..]}",
		func(r *rec, raw json.RawMessage) (any, error) {
			a, err := args[struct {
				Keys []string `json:"keys"`
			}](raw)
			if err != nil {
				return nil, err
			}
			for _, k := range a.Keys {
				if err := r.b.SendKey(k); err != nil {
					return nil, api.BadRequest("%v", err)
				}
			}
			return map[string]any{"sent": len(a.Keys)}, nil
		})
	s.terminal("resize", "Request a size for the terminal. args: {cols, rows, client_id, take}. Only the client with size authority may resize, unless take is set.",
		func(r *rec, raw json.RawMessage) (any, error) {
			a, err := args[struct {
				Cols     uint16 `json:"cols"`
				Rows     uint16 `json:"rows"`
				CellW    uint32 `json:"cell_width_px"`
				CellH    uint32 `json:"cell_height_px"`
				ClientID string `json:"client_id"`
				Take     bool   `json:"take"`
			}](raw)
			if err != nil {
				return nil, err
			}
			who := a.ClientID
			if who == "" {
				who = "cli"
			}
			s.mu.Lock()
			_, ownerAlive := s.clients[r.owner]
			if r.owner != "" && r.owner != who && r.owner != "cli" && ownerAlive && !a.Take {
				s.mu.Unlock()
				return nil, api.Conflict("client %s has size authority; pass take to override", r.owner)
			}
			r.owner = who
			s.mu.Unlock()
			if err := r.b.ResizePx(a.Cols, a.Rows, a.CellW, a.CellH); err != nil {
				return nil, api.BadRequest("%v", err)
			}
			cw, ch := r.b.CellSize()
			s.mu.Lock()
			s.bus.Block(r.st.SessionID, r.b.ID, "size_changed", map[string]any{"cols": a.Cols, "rows": a.Rows,
				"cell_width_px": cw, "cell_height_px": ch, "size_owner": who})
			s.mu.Unlock()
			s.markDirty()
			return map[string]any{"cols": a.Cols, "rows": a.Rows, "size_owner": who}, nil
		})
	s.terminal("list_dir", "List the directories under a path, for Go to Directory. args: {path}; default the working directory.",
		func(r *rec, raw json.RawMessage) (any, error) {
			a, err := args[struct {
				Path   string `json:"path"`
				Hidden bool   `json:"hidden"`
			}](raw)
			if err != nil {
				return nil, err
			}
			path := expandHome(a.Path)
			if path == "" {
				if fg, err := r.b.Foreground(); err == nil && fg.Cwd != "" {
					path = fg.Cwd
				} else {
					path, _ = os.UserHomeDir()
				}
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil, api.BadRequest("%v", err)
			}
			out := []map[string]any{}
			for _, e := range entries {
				if !e.IsDir() && e.Type()&os.ModeSymlink == 0 {
					continue
				}
				if !a.Hidden && strings.HasPrefix(e.Name(), ".") {
					continue
				}
				full := filepath.Join(path, e.Name())
				if fi, err := os.Stat(full); err != nil || !fi.IsDir() {
					continue
				}
				out = append(out, map[string]any{"name": e.Name(), "path": full})
				if len(out) >= 2000 {
					break
				}
			}
			return map[string]any{"path": path, "entries": out}, nil
		})
	s.terminal("program_status", "Return the status records programs have reported (OSC 7501).",
		func(r *rec, _ json.RawMessage) (any, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			return map[string]any{"records": sortedStatus(r.st.Status)}, nil
		})
	api.Register(s.reg, terminalPrefix+"wait", "Wait for the program to exit. args: {timeout} in seconds, at most 60; repeat to wait longer. Works for a short while after the block has closed.",
		func(ctx context.Context, in blockCall) (map[string]any, error) {
			a, err := args[struct {
				Timeout float64 `json:"timeout"`
			}](in.Args)
			if err != nil {
				return nil, err
			}
			s.mu.Lock()
			r := s.blocks[in.BlockID]
			ex, gone := s.exits[in.BlockID]
			s.mu.Unlock()
			if r == nil {
				if gone {
					return map[string]any{"exited": true, "exit_code": ex.code, "timed_out": false}, nil
				}
				return nil, api.NotFound("no block %s", in.BlockID)
			}
			d := time.Duration(a.Timeout * float64(time.Second))
			if d <= 0 || d > time.Minute {
				d = time.Minute
			}
			select {
			case <-r.b.Done():
				code, _ := r.b.ExitStatus()
				return map[string]any{"exited": true, "exit_code": code, "timed_out": false}, nil
			case <-time.After(d):
				return map[string]any{"exited": false, "timed_out": true}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})

	type methodIn struct {
		SessionID string          `json:"session_id,omitempty"`
		BlockID   string          `json:"block_id" required:"true"`
		Method    string          `json:"method" required:"true" desc:"A block method, such as hive.terminal.format or format."`
		Args      json.RawMessage `json:"args,omitempty"`
	}
	api.Register(s.reg, "block.method", "Invoke a typed method on a block.",
		func(ctx context.Context, in methodIn) (any, error) {
			name := in.Method
			if !strings.HasPrefix(name, terminalPrefix) {
				name = terminalPrefix + name
			}
			raw, _ := json.Marshal(blockCall{SessionID: in.SessionID, BlockID: in.BlockID, Args: in.Args})
			return s.reg.Call(ctx, name, raw)
		})
}

// ---- agents ----

func (s *Server) registerAgentMethods() {
	type reportIn struct {
		BlockID        string `json:"block_id" required:"true"`
		Harness        string `json:"harness" required:"true" desc:"claude, codex, opencode, gemini, grok..."`
		Event          string `json:"event,omitempty"`
		State          string `json:"state" required:"true" enum:"idle,working,blocked,done,ended"`
		SessionID      string `json:"session_id,omitempty" desc:"The agent's own session ID, used to resume it."`
		Cwd            string `json:"cwd,omitempty"`
		TranscriptPath string `json:"transcript_path,omitempty"`
		Message        string `json:"message,omitempty"`
	}
	api.Register(s.reg, "agent.report", "Report an agent's identity and state from its hooks. Saved to disk at once.",
		func(ctx context.Context, in reportIn) (*persist.Agent, error) {
			// What a hook reports is untrusted: it ends up in a command line
			// at the next restore and in notifications.
			if in.SessionID != "" && !agents.ValidSessionID(in.SessionID) {
				return nil, api.BadRequest("session_id: letters, digits, '.', '-' and '_' only, not starting with '-'")
			}
			if in.Cwd != "" && !filepath.IsAbs(in.Cwd) {
				return nil, api.BadRequest("cwd must be an absolute path")
			}
			if r := []rune(in.Message); len(r) > 400 {
				in.Message = string(r[:400]) + "…"
			}
			s.mu.Lock()
			rr, err := s.needBlock(in.BlockID)
			if err != nil {
				s.mu.Unlock()
				return nil, err
			}
			a := rr.st.Agent
			if a == nil || a.Harness != in.Harness {
				a = &persist.Agent{Harness: in.Harness}
			}
			if in.SessionID != "" {
				a.SessionID = in.SessionID // /clear mints a new one: always take the latest
			}
			if in.Cwd != "" {
				a.Cwd = in.Cwd
			}
			if in.TranscriptPath != "" {
				a.TranscriptPath = in.TranscriptPath
			}
			a.State, a.Message, a.UpdatedAt = in.State, in.Message, time.Now().UTC()
			rr.st.Agent = a
			copyA := *a
			s.bus.Block(rr.st.SessionID, in.BlockID, "agent_changed", map[string]any{
				"harness": a.Harness, "session_id": a.SessionID, "state": a.State, "message": a.Message, "event": in.Event,
			})
			s.mu.Unlock()
			if err := s.flush(false); err != nil {
				return nil, api.Internal("save: %v", err)
			}
			return &copyA, nil
		})

	type agentRow struct {
		BlockID      string         `json:"block_id"`
		SessionID    string         `json:"session_id"`
		SessionLabel string         `json:"session_label,omitempty"`
		WindowID     string         `json:"window_id,omitempty"`
		Label        string         `json:"label,omitempty"`
		Title        string         `json:"title,omitempty"`
		Agent        *persist.Agent `json:"agent"`
		Status       any            `json:"program_status,omitempty"`
		Foreground   *block.Process `json:"foreground,omitempty"`
	}
	api.Register(s.reg, "agent.list", "List every block running a known agent, with its state.",
		func(ctx context.Context, _ empty) (json.RawMessage, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			rows := []agentRow{}
			for _, ss := range s.sessions {
				for _, bid := range ss.Blocks() {
					rr := s.blocks[bid]
					if rr == nil || rr.st.Agent == nil || rr.st.Agent.State == "ended" {
						continue
					}
					a := *rr.st.Agent
					row := agentRow{BlockID: bid, SessionID: ss.ID, SessionLabel: ss.Label, Label: rr.st.Label, Title: rr.st.Title,
						Agent: &a, Foreground: rr.st.Foreground}
					if w := ss.WindowOf(bid); w != nil {
						row.WindowID = w.ID
					}
					if len(rr.st.Status) > 0 {
						row.Status = sortedStatus(rr.st.Status)
					}
					rows = append(rows, row)
				}
			}
			return copyJSON(map[string][]agentRow{"agents": rows})
		})
}

// ---- small helpers ----

func orDefault(v, d uint16) uint16 {
	if v == 0 {
		return d
	}
	return v
}

func firstOf(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func indexOf(ss *session.Session, windowID string) int {
	for i, w := range ss.Windows {
		if w.ID == windowID {
			return i
		}
	}
	return 0
}

// sideFor maps a direction/side pair to a side.
func sideFor(direction, side string) string {
	after := side != "before"
	if direction == "vertical" {
		if after {
			return "below"
		}
		return "above"
	}
	if after {
		return "right"
	}
	return "left"
}
