package server

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/id"
	"github.com/manueltarouca/hive/internal/persist"
	"github.com/manueltarouca/hive/internal/script"
)

// Notifications and badges that scripts (and the CLI) send. The apps show
// them; the server keeps notifications for a day, so an app that was closed
// shows what it missed when it connects.

const (
	keepNotes     = 100
	noteLife      = 24 * time.Hour
	noteRate      = 10 // per source per minute
	maxNoteText   = 1000
	maxNoteAction = 4
)

// Notification is one notification.
type Notification struct {
	ID        string       `json:"notification_id"`
	Time      time.Time    `json:"time"`
	Title     string       `json:"title"`
	Body      string       `json:"body,omitempty"`
	Level     string       `json:"level"`
	BlockID   string       `json:"block_id,omitempty"`
	SessionID string       `json:"session_id,omitempty"`
	Source    string       `json:"source" desc:"script:NAME, or who sent it"`
	Actions   []NoteAction `json:"actions,omitempty"`
	Acked     bool         `json:"acked,omitempty"`
}

// NoteAction is a button on a notification: it runs an action of the
// script that sent it.
type NoteAction struct {
	Title  string         `json:"title" required:"true"`
	Action string         `json:"action" required:"true" desc:"An action of the script sending the notification."`
	Args   map[string]any `json:"args,omitempty"`
	// Step says what pressing it does, in plain words, for the app to show.
	Step string `json:"step,omitempty"`
	// run is the script run that sent it: the button works only while
	// that run still owns the action.
	run string
}

// Badge is a status a script shows on a pane: the fields of a program's
// own status (OSC 7501), kept apart from them.
type Badge struct {
	BlockID  string    `json:"block_id"`
	App      string    `json:"app" desc:"script:NAME, or cli"`
	ID       string    `json:"id"`
	State    string    `json:"state" enum:"idle,working,done,blocked,error"`
	Kind     string    `json:"kind,omitempty"`
	Progress *int      `json:"progress,omitempty"`
	Title    string    `json:"title,omitempty"`
	Msg      string    `json:"msg,omitempty"`
	Time     time.Time `json:"updated_at"`
}

type uiState struct {
	mu     sync.Mutex
	notes  []Notification
	sent   map[string][]time.Time
	badges map[string]map[string]Badge // block to app/id
}

// sourceOf names who sends a notification or a badge: a script by name,
// anyone else as cli.
func sourceOf(ctx context.Context) string {
	if c, ok := script.CallerFrom(ctx); ok {
		return "script:" + c.Name
	}
	return "cli"
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (s *Server) registerUIMethods() {
	type notifyIn struct {
		Title   string       `json:"title" required:"true"`
		Body    string       `json:"body,omitempty"`
		Target  string       `json:"target,omitempty" desc:"A block or session ID: the notification opens it."`
		Level   string       `json:"level,omitempty" enum:"info,warn,error" desc:"Default info."`
		Actions []NoteAction `json:"actions,omitempty" desc:"Buttons, at most 4. Each runs an action of the script sending it."`
	}
	api.Register(s.reg, "notify.send", "Send a notification to every app. Kept for a day, so an app that was closed shows it when it connects.",
		func(ctx context.Context, in notifyIn) (Notification, error) {
			src := sourceOf(ctx)
			n := Notification{ID: id.New("notification"), Time: time.Now().UTC(), Title: clip(in.Title, 200),
				Body: clip(in.Body, maxNoteText), Level: in.Level, Source: src}
			if n.Level == "" {
				n.Level = "info"
			}
			if len(in.Actions) > maxNoteAction {
				return Notification{}, api.BadRequest("at most %d actions", maxNoteAction)
			}
			c, isScript := script.CallerFrom(ctx)
			for _, a := range in.Actions {
				if builtinNoteAction(a.Action) {
					// Run by the server for whoever presses it, never by the
					// script: it can offer to turn itself off, not do it.
					if !isScript {
						return Notification{}, api.BadRequest("%s buttons are for scripts", a.Action)
					}
					a.Title = clip(a.Title, 40)
					if err := s.checkBuiltinButton(c.Name, &a); err != nil {
						return Notification{}, err
					}
					n.Actions = append(n.Actions, a)
					continue
				}
				// A button runs with the sending script's rights: it may only
				// name that script's own actions.
				owner, ok := s.scripts.ActionOwner(a.Action)
				if !ok {
					return Notification{}, api.NotFound("no action %s", a.Action)
				}
				if run, _ := s.scripts.ActionRun(a.Action); !isScript || owner != c.Name || run != c.RunID {
					return Notification{}, api.PermissionDenied("a notification can only offer actions of the script that sends it")
				}
				a.run = c.RunID
				a.Title = clip(a.Title, 40)
				a.Step = clip(a.Step, 200)
				n.Actions = append(n.Actions, a)
			}
			if in.Target != "" {
				s.mu.Lock()
				if r := s.blocks[in.Target]; r != nil {
					n.BlockID, n.SessionID = in.Target, r.st.SessionID
				} else if s.session(in.Target) != nil {
					n.SessionID = in.Target
				}
				s.mu.Unlock()
				if n.BlockID == "" && n.SessionID == "" {
					return Notification{}, api.NotFound("no block or session %s", in.Target)
				}
			}
			u := &s.ui
			u.mu.Lock()
			now := time.Now()
			var recent []time.Time
			for _, t := range u.sent[src] {
				if now.Sub(t) < time.Minute {
					recent = append(recent, t)
				}
			}
			if len(recent) >= noteRate {
				u.mu.Unlock()
				return Notification{}, &api.Error{Status: 429, Code: "rate_limited", Message: fmt.Sprintf("at most %d notifications a minute", noteRate)}
			}
			if u.sent == nil {
				u.sent = map[string][]time.Time{}
			}
			u.sent[src] = append(recent, now)
			u.notes = append(u.notes, n)
			u.trim(now)
			u.mu.Unlock()
			s.bus.Server("notification", map[string]any{"notification": n})
			return n, nil
		}, api.Perm("ui"))

	type listIn struct {
		Since time.Time `json:"since,omitzero" desc:"Only those sent after this time."`
		All   bool      `json:"all,omitempty" desc:"Acknowledged ones too."`
	}
	api.Register(s.reg, "notification.list", "Notifications from the last day, oldest first: what an app missed while it was closed.",
		func(ctx context.Context, in listIn) (map[string]any, error) {
			u := &s.ui
			u.mu.Lock()
			defer u.mu.Unlock()
			u.trim(time.Now())
			out := []Notification{}
			for _, n := range u.notes {
				if n.Time.After(in.Since) && (in.All || !n.Acked) {
					out = append(out, n)
				}
			}
			return map[string]any{"notifications": out}, nil
		}, api.Perm("read"))
	type ackIn struct {
		ID string `json:"notification_id" required:"true"`
	}
	api.Register(s.reg, "notification.ack", "Mark a notification seen, on every app.",
		func(ctx context.Context, in ackIn) (empty, error) {
			u := &s.ui
			u.mu.Lock()
			found := false
			src, isScript := script.CallerFrom(ctx)
			for i := range u.notes {
				if u.notes[i].ID == in.ID {
					// A script marks only its own seen: never what else wants you.
					if isScript && u.notes[i].Source != "script:"+src.Name {
						u.mu.Unlock()
						return empty{}, api.PermissionDenied("a script can only mark its own notifications seen")
					}
					u.notes[i].Acked, found = true, true
				}
			}
			u.mu.Unlock()
			if !found {
				return empty{}, api.NotFound("no notification %s", in.ID)
			}
			s.bus.Server("notification_acked", map[string]any{"notification_id": in.ID})
			return empty{}, nil
		}, api.Perm("ui"))

	type statusIn struct {
		Target   string `json:"target" required:"true" desc:"A block ID."`
		ID       string `json:"id" required:"true" desc:"The badge's name, unique for its sender."`
		State    string `json:"state" required:"true" enum:"idle,working,done,blocked,error"`
		Kind     string `json:"kind,omitempty"`
		Progress *int   `json:"progress,omitempty" desc:"0 to 100."`
		Title    string `json:"title,omitempty"`
		Msg      string `json:"msg,omitempty"`
	}
	api.Register(s.reg, "status.set", "Show a badge on a pane: a state and a short message. A script's badges go when it stops.",
		func(ctx context.Context, in statusIn) (Badge, error) {
			if in.Progress != nil && (*in.Progress < 0 || *in.Progress > 100) {
				return Badge{}, api.BadRequest("progress: 0 to 100")
			}
			b := Badge{BlockID: in.Target, App: sourceOf(ctx), ID: clip(in.ID, 64), State: in.State, Kind: clip(in.Kind, 64),
				Progress: in.Progress, Title: clip(in.Title, 80), Msg: clip(in.Msg, 200), Time: time.Now().UTC()}
			s.mu.Lock()
			r := s.blocks[in.Target]
			if r == nil {
				s.mu.Unlock()
				return Badge{}, api.NotFound("no block %s", in.Target)
			}
			u := &s.ui
			u.mu.Lock()
			if u.badges == nil {
				u.badges = map[string]map[string]Badge{}
			}
			if u.badges[in.Target] == nil {
				u.badges[in.Target] = map[string]Badge{}
			}
			if _, ok := u.badges[in.Target][b.App+"/"+b.ID]; !ok && len(u.badges[in.Target]) >= 32 {
				u.mu.Unlock()
				s.mu.Unlock()
				return Badge{}, api.BadRequest("at most 32 badges on a pane")
			}
			u.badges[in.Target][b.App+"/"+b.ID] = b
			u.mu.Unlock()
			s.bus.Block(r.st.SessionID, in.Target, "status_changed", map[string]any{"badge": b})
			s.mu.Unlock()
			return b, nil
		}, api.Perm("ui"))
	type clearIn struct {
		Target string `json:"target,omitempty" desc:"A block ID. Without it, every pane."`
		ID     string `json:"id,omitempty" desc:"One badge. Without it, all of the sender's."`
	}
	api.Register(s.reg, "status.clear", "Remove badges the sender set.",
		func(ctx context.Context, in clearIn) (map[string]int, error) {
			n := s.clearBadges(sourceOf(ctx), in.Target, in.ID)
			return map[string]int{"cleared": n}, nil
		}, api.Perm("ui"))
	type statusListIn struct {
		Target string `json:"target,omitempty" desc:"A block ID. Without it, every pane."`
	}
	api.Register(s.reg, "status.list", "Badges on panes, from scripts and the CLI.",
		func(ctx context.Context, in statusListIn) (map[string]any, error) {
			u := &s.ui
			u.mu.Lock()
			defer u.mu.Unlock()
			out := []Badge{}
			for bid, m := range u.badges {
				if in.Target != "" && bid != in.Target {
					continue
				}
				for _, b := range m {
					out = append(out, b)
				}
			}
			sort.Slice(out, func(i, j int) bool {
				if out[i].BlockID != out[j].BlockID {
					return out[i].BlockID < out[j].BlockID
				}
				return out[i].App+out[i].ID < out[j].App+out[j].ID
			})
			return map[string]any{"badges": out}, nil
		}, api.Perm("read"))
}

// clearBadges removes app's badges (one, on one pane, or all). It returns
// how many went.
func (s *Server) clearBadges(app, blockID, badgeID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := &s.ui
	u.mu.Lock()
	type gone struct {
		block, session string
		b              Badge
	}
	var cleared []gone
	for bid, m := range u.badges {
		if blockID != "" && bid != blockID {
			continue
		}
		for k, b := range m {
			if b.App == app && (badgeID == "" || b.ID == badgeID) {
				delete(m, k)
				sid := ""
				if r := s.blocks[bid]; r != nil {
					sid = r.st.SessionID
				}
				cleared = append(cleared, gone{bid, sid, b})
			}
		}
		if len(m) == 0 {
			delete(u.badges, bid)
		}
	}
	u.mu.Unlock()
	for _, g := range cleared {
		if g.session != "" {
			g.b.State = "clear"
			s.bus.Block(g.session, g.block, "status_changed", map[string]any{"badge": g.b})
		}
	}
	return len(cleared)
}

// trim drops notifications older than a day, and the oldest past 100.
// Caller holds u.mu.
func (u *uiState) trim(now time.Time) {
	i := 0
	for i < len(u.notes) && now.Sub(u.notes[i].Time) > noteLife {
		i++
	}
	u.notes = u.notes[i:]
	if len(u.notes) > keepNotes {
		u.notes = u.notes[len(u.notes)-keepNotes:]
	}
}

// scriptEnded clears what a script left on screen.
func (s *Server) scriptEnded(name string) {
	s.clearBadges("script:"+name, "", "")
}

// badgesGone forgets a closed block's badges, and tells the apps. Caller
// holds s.mu.
func (s *Server) badgesGone(blockID, sessionID string) {
	s.ui.mu.Lock()
	gone := s.ui.badges[blockID]
	delete(s.ui.badges, blockID)
	s.ui.mu.Unlock()
	for _, b := range gone {
		b.State = "clear"
		s.bus.Block(sessionID, blockID, "status_changed", map[string]any{"badge": b})
	}
}

func (s *Server) registerActionMethods() {
	api.Register(s.reg, "action.list", "List the actions scripts offer.",
		func(ctx context.Context, _ empty) (map[string]any, error) {
			return map[string]any{"actions": s.scripts.Actions()}, nil
		}, api.Perm("read"))
	type runIn struct {
		Name           string          `json:"name" required:"true"`
		Args           json.RawMessage `json:"args,omitempty"`
		Origin         string          `json:"origin,omitempty" enum:"cli,notification,palette,key,api" desc:"Where it was started. Default api; notification needs notification_id."`
		NotificationID string          `json:"notification_id,omitempty" desc:"The notification whose button this is: it runs only while the script that sent it is running."`
		BlockID        string          `json:"block_id,omitempty" desc:"The pane it acts on, when it was started from one."`
		SessionID      string          `json:"session_id,omitempty"`
		ClientID       string          `json:"client_id,omitempty"`
	}
	api.Register(s.reg, "action.run", "Run a script's action, and return its result.",
		func(ctx context.Context, in runIn) (json.RawMessage, error) {
			origin := in.Origin
			if origin == "" {
				origin = "api"
			}
			if _, isScript := script.CallerFrom(ctx); isScript {
				origin = "script"
			}
			if builtinNoteAction(in.Name) {
				if err := notByScript(ctx, "press "+in.Name); err != nil {
					return nil, err
				}
				s.bus.Server("action_run", map[string]any{"name": in.Name, "origin": "notification", "client_id": in.ClientID})
				return s.runBuiltinButton(ctx, in.NotificationID, in.Name, in.Args)
			}
			if in.NotificationID != "" || origin == "notification" {
				if err := s.checkNoteAction(in.NotificationID, in.Name); err != nil {
					return nil, err
				}
				origin = "notification"
			}
			meta := map[string]any{"origin": origin, "block_id": in.BlockID, "session_id": in.SessionID, "client_id": in.ClientID}
			s.bus.Server("action_run", map[string]any{"name": in.Name, "origin": origin, "client_id": in.ClientID})
			return s.scripts.RunAction(ctx, in.Name, in.Args, meta)
		}, api.Perm("ui"))

	type actIn struct {
		BlockID  string   `json:"block_id" required:"true"`
		Revision uint64   `json:"revision" required:"true" desc:"The agent's revision when the decision was made: a newer one means it moved on, and nothing is sent."`
		SendKey  []string `json:"send_key,omitempty"`
		Write    string   `json:"write,omitempty"`
		Confirm  string   `json:"confirm,omitempty" desc:"A pattern (RE2) the pane's last 10 lines must match."`
		Expect   []string `json:"expect_state,omitempty" desc:"The states the agent must still be in. Default blocked; idle or done to nudge an agent that stopped; working to interrupt one."`
	}
	api.Register(s.reg, "agent.act", "Answer or nudge an agent: press keys or type, only if it is still in the state and revision that was seen.",
		func(ctx context.Context, in actIn) (map[string]any, error) {
			if len(in.SendKey) == 0 && in.Write == "" {
				return nil, api.BadRequest("send_key or write")
			}
			var confirm *regexp.Regexp
			if in.Confirm != "" {
				re, err := regexp.Compile(in.Confirm)
				if err != nil {
					return nil, api.BadRequest("confirm: %v", err)
				}
				confirm = re
			}
			// Check under the lock, act outside it: a terminal that isn't
			// reading would hold every other call up. The window between is
			// the one a person's key press has too; the revision is checked
			// again after, and the answer says if it moved.
			s.mu.Lock()
			r := s.blocks[in.BlockID]
			var a *persist.Agent
			if r != nil {
				a = r.st.Agent
			}
			expect := in.Expect
			if len(expect) == 0 {
				expect = []string{"blocked"}
			}
			for _, e := range expect {
				if e != "blocked" && e != "idle" && e != "done" && e != "working" {
					return nil, api.BadRequest("expect_state: blocked, idle, done or working")
				}
			}
			stale := a == nil || !slices.Contains(expect, a.State) || a.Revision != in.Revision
			s.mu.Unlock()
			if r == nil {
				return nil, api.NotFound("no block %s", in.BlockID)
			}
			if stale {
				return nil, api.Conflict("the agent moved on since: nothing was sent")
			}
			if confirm != nil {
				text, err := r.b.FormatScreen(true, true)
				if err != nil {
					return nil, err
				}
				if !confirm.MatchString(tailLines(text, 10)) {
					return nil, api.Conflict("the screen doesn't show what was expected: nothing was sent")
				}
			}
			for _, k := range in.SendKey {
				if err := r.b.SendKey(k); err != nil {
					return nil, api.BadRequest("%v", err)
				}
			}
			if in.Write != "" {
				if err := r.b.Write([]byte(in.Write)); err != nil {
					return nil, err
				}
			}
			return map[string]any{"sent": true}, nil
		}, api.Perm("input"))
}

// checkNoteAction: a notification's button runs its action only if the
// notification offered it and the run that sent it still owns it.
func (s *Server) checkNoteAction(noteID, action string) error {
	if noteID == "" {
		return api.BadRequest("a notification's button needs its notification_id")
	}
	u := &s.ui
	u.mu.Lock()
	var offered *NoteAction
	for _, n := range u.notes {
		if n.ID != noteID {
			continue
		}
		for i := range n.Actions {
			if n.Actions[i].Action == action {
				a := n.Actions[i]
				offered = &a
			}
		}
	}
	u.mu.Unlock()
	if offered == nil {
		return api.NotFound("notification %s offers no %s (it may be over a day old)", noteID, action)
	}
	if run, ok := s.scripts.ActionRun(action); !ok || run != offered.run {
		return api.Conflict("the script that sent this has stopped: nothing was run")
	}
	return nil
}
