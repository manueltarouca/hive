package server

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/keymap"
)

// The keymap: an app's default shortcuts, what scripts bind, and the
// settings file's keybind lines, in that order (later wins).

func (s *Server) keymapFor(kind string) (keymap.Keymap, []string) {
	s.mu.Lock()
	lines := append([]string(nil), s.conf.Keybind...)
	s.mu.Unlock()
	var user []keymap.Binding
	for _, l := range lines {
		if b, err := keymap.ParseBinding(l); err == nil {
			user = append(user, b)
		}
	}
	km, warns := keymap.Merge(keymap.Defaults(kind), s.scripts.Bindings(), user)
	exclusive := s.scripts.Modes()
	for i := range km.Modes {
		km.Modes[i].Exclusive = exclusive[km.Modes[i].Name]
	}
	// A mode a script declared with no bindings yet still exists.
	have := map[string]bool{}
	for _, m := range km.Modes {
		have[m.Name] = true
	}
	var extra []string
	for name := range exclusive {
		if !have[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		km.Modes = append(km.Modes, keymap.Mode{Name: name, Exclusive: exclusive[name], Source: "script"})
	}
	return km, warns
}

func (s *Server) keymapChanged() { s.bus.Server("keymap_changed", nil) }

func (s *Server) registerKeyMethods() {
	type getIn struct {
		ClientKind string `json:"client_kind,omitempty" desc:"mac: the Mac app's defaults come first. Others get scripts' and the settings file's bindings."`
	}
	api.Register(s.reg, "keymap.get", "The key bindings for an app: its defaults, scripts' bindings, then the settings file's keybind lines. Apps resolve keys themselves.",
		func(ctx context.Context, in getIn) (map[string]any, error) {
			km, warns := s.keymapFor(in.ClientKind)
			if warns == nil {
				warns = []string{}
			}
			return map[string]any{"bindings": km.Bindings, "modes": km.Modes, "warnings": warns}, nil
		}, api.Perm("read"))

	type performIn struct {
		ClientID string          `json:"client_id" required:"true"`
		Action   string          `json:"action" required:"true" desc:"A client action, such as client.zoom or client.palette."`
		Args     json.RawMessage `json:"args,omitempty"`
	}
	api.Register(s.reg, "client.perform", "Ask a connected app to do one of its own actions (client.zoom, client.mode, client.palette). Apps take these only from the server on their own machine.",
		func(ctx context.Context, in performIn) (empty, error) {
			msg := map[string]any{"type": "perform", "action": in.Action}
			if len(in.Args) > 0 {
				msg["args"] = in.Args
			}
			s.attachMu.Lock()
			defer s.attachMu.Unlock()
			for c := range s.attaches {
				if c.client != nil && c.client.ID == in.ClientID {
					c.sendJSON(msg)
					return empty{}, nil
				}
			}
			return empty{}, api.NotFound("no connected client %s", in.ClientID)
		}, api.Perm("ui"))
}
