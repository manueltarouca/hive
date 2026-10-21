package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/block"
	"github.com/manueltarouca/hive/internal/remote"
	"github.com/manueltarouca/hive/internal/script"
)

// BlockSession is the session a block is in (for scripts' ID filling).
func (s *Server) BlockSession(blockID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.blocks[blockID]; r != nil {
		return r.st.SessionID, true
	}
	return "", false
}

type scriptRunIn struct {
	Code      string          `json:"code" required:"true" desc:"The script, bundled: hive script run makes it from TypeScript."`
	Name      string          `json:"name,omitempty"`
	Args      json.RawMessage `json:"args,omitempty" desc:"An object the script reads as h.args."`
	Allow     []string        `json:"allow,omitempty" desc:"Permissions for a script without a manifest. Default: read."`
	SessionID string          `json:"session_id,omitempty" desc:"The session it runs in: IDs it leaves out come from here."`
	BlockID   string          `json:"block_id,omitempty" desc:"The block it runs from."`
	For       float64         `json:"for,omitempty" desc:"Stop it after this many seconds."`
	All       bool            `json:"all,omitempty" desc:"Hear every session's events, not only its own."`
}

// startScripts loads the scripts folder once sessions are restored, then
// tells every script the server is up.
func (s *Server) startScripts() {
	s.scripts.StartResidents()
	s.mu.Lock()
	sessions, agents := len(s.sessions), s.agentsResumed
	s.mu.Unlock()
	s.bus.Server("server_started", map[string]any{"restored": !s.cfg.Fresh, "version": Version})
	s.bus.Server("restore_finished", map[string]any{"sessions": sessions, "agents_resumed": agents})
}

func (s *Server) registerScriptMethods() {
	api.Register(s.reg, "script.run", "Run a script once. Its log and end come as script_log and script_ended events (GET /v1/events?types=script_log,script_ended&since=SEQ).",
		func(ctx context.Context, in scriptRunIn) (map[string]any, error) {
			id := remote.IdentityOf(ctx)
			s.mu.Lock()
			remoteOK := s.conf.ScriptRemoteRun
			s.mu.Unlock()
			if id.Via != remote.Local.String() && !remoteOK {
				return nil, &api.Error{Status: 403, Code: "forbidden",
					Message: "this server doesn't run scripts for other machines (setting script-remote-run)"}
			}
			allow := in.Allow
			if len(allow) == 0 {
				allow = []string{"read"}
			}
			perms, err := script.ParsePerms(allow...)
			if err != nil {
				return nil, api.BadRequest("allow: %v", err)
			}
			if in.For < 0 {
				return nil, api.BadRequest("for must be positive")
			}
			seq := s.bus.Seq()
			parent, _ := script.CallerFrom(ctx)
			r, err := s.scripts.Start(script.RunOptions{
				Parent: parent.RunID,
				Name:   in.Name, JS: in.Code, Args: in.Args, Perms: perms, Limit: script.PermsFrom(ctx),
				Context:  script.Context{SessionID: in.SessionID, BlockID: in.BlockID},
				Identity: id, For: time.Duration(in.For * float64(time.Second)), All: in.All,
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{"run_id": r.ID, "seq": seq}, nil
		}, api.Perm("read"))
	type runRef struct {
		RunID string `json:"run_id" required:"true"`
	}
	api.Register(s.reg, "script.stop", "Stop a running script.",
		func(ctx context.Context, in runRef) (empty, error) { return empty{}, s.scripts.Stop(in.RunID) },
		api.Perm("control"))
	api.Register(s.reg, "script.list", "List the scripts folder's scripts, running one-shots, and the latest that ended.",
		func(ctx context.Context, _ empty) (map[string]any, error) {
			return map[string]any{"scripts": s.scripts.Residents(), "runs": nonNilRuns(s.scripts.List())}, nil
		}, api.Perm("read"))
	type nameIn struct {
		Name string `json:"name" required:"true" desc:"A script in the scripts folder."`
	}
	api.Register(s.reg, "script.grant", "Allow a script the permissions its manifest asks for, and start it.",
		func(ctx context.Context, in nameIn) (script.Resident, error) { return s.scripts.Grant(in.Name) },
		api.Perm("server"))
	api.Register(s.reg, "script.revoke", "Take a script's permissions away. It waits for a new grant.",
		func(ctx context.Context, in nameIn) (script.Resident, error) { return s.scripts.Revoke(in.Name) },
		api.Perm("server"))
	api.Register(s.reg, "script.enable", "Turn a script back on, quarantined ones included.",
		func(ctx context.Context, in nameIn) (script.Resident, error) { return s.scripts.Enable(in.Name, true) },
		api.Perm("server"))
	api.Register(s.reg, "script.disable", "Turn a script off without removing it.",
		func(ctx context.Context, in nameIn) (script.Resident, error) { return s.scripts.Enable(in.Name, false) },
		api.Perm("server"))
	type reloadIn struct {
		Name string `json:"name,omitempty" desc:"One script; all of them without it."`
	}
	api.Register(s.reg, "script.reload", "Load scripts from the folder again now, instead of at the next check.",
		func(ctx context.Context, in reloadIn) (map[string]any, error) {
			rs, err := s.scripts.Reload(in.Name)
			return map[string]any{"scripts": rs}, err
		}, api.Perm("server"))
	type logsIn struct {
		Name string `json:"name" required:"true"`
		Tail int    `json:"tail,omitempty" desc:"The last N lines. Default 100."`
	}
	api.Register(s.reg, "script.logs", "A script's log, from its file.",
		func(ctx context.Context, in logsIn) (map[string]any, error) {
			n := in.Tail
			if n <= 0 {
				n = 100
			}
			return map[string]any{"lines": s.scripts.Logs(in.Name, n)}, nil
		}, api.Perm("read"))
	type auditIn struct {
		Name string `json:"name,omitempty" desc:"One script's entries."`
		Tail int    `json:"tail,omitempty" desc:"The last N entries. Default 100."`
	}
	api.Register(s.reg, "audit.list", "What scripts changed: every call that arranged, ran, typed or changed settings.",
		func(ctx context.Context, in auditIn) (map[string]any, error) {
			n := in.Tail
			if n <= 0 {
				n = 100
			}
			return map[string]any{"entries": s.scripts.Audit(in.Name, n)}, nil
		}, api.Perm("read"))
	type resultIn struct {
		RunID   string  `json:"run_id" required:"true"`
		Timeout float64 `json:"timeout,omitempty" desc:"Wait up to this many seconds for it to end, at most 60. Default: don't wait."`
	}
	api.Register(s.reg, "script.result", "A script run's state and its whole result.",
		func(ctx context.Context, in resultIn) (script.RunInfo, error) {
			d := time.Duration(min(in.Timeout, 60) * float64(time.Second))
			wctx, cancel := context.WithTimeout(ctx, max(d, time.Millisecond))
			defer cancel()
			info, err := s.scripts.Wait(wctx, in.RunID)
			if err == context.DeadlineExceeded {
				for _, r := range s.scripts.List() {
					if r.RunID == in.RunID {
						return r, nil
					}
				}
			}
			return info, err
		}, api.Perm("read"))
}

func nonNilRuns(rs []script.RunInfo) []script.RunInfo {
	if rs == nil {
		return []script.RunInfo{}
	}
	return rs
}

// Blocks lists every block, for output triggers.
func (s *Server) Blocks() []script.BlockRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]script.BlockRef, 0, len(s.blocks))
	for id, r := range s.blocks {
		out = append(out, script.BlockRef{ID: id, SessionID: r.st.SessionID})
	}
	return out
}

// Tap reads a block's output from now on, for output triggers.
func (s *Server) Tap(blockID string) (*block.Sub, func(), error) {
	s.mu.Lock()
	r := s.blocks[blockID]
	s.mu.Unlock()
	if r == nil {
		return nil, nil, api.NotFound("no block %s", blockID)
	}
	sub := r.b.Tap(256)
	return sub, func() { r.b.Detach(sub) }, nil
}

// FormatScreen is a block's visible screen as text, for snapshot triggers.
func (s *Server) FormatScreen(blockID string) (string, error) {
	s.mu.Lock()
	r := s.blocks[blockID]
	s.mu.Unlock()
	if r == nil {
		return "", api.NotFound("no block %s", blockID)
	}
	return r.b.FormatScreen(true, false)
}
