package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/manueltarouca/hive/internal/api"
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
			r, err := s.scripts.Start(script.RunOptions{
				Name: in.Name, JS: in.Code, Args: in.Args, Perms: perms, Limit: script.PermsFrom(ctx),
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
	api.Register(s.reg, "script.list", "List running scripts and the latest that ended.",
		func(ctx context.Context, _ empty) (map[string]any, error) {
			return map[string]any{"runs": nonNilRuns(s.scripts.List())}, nil
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
