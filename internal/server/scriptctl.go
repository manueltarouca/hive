package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/remote"
	"github.com/manueltarouca/hive/internal/script"
)

// Seeing what scripts do and turning it off: modes, options, the activity
// list. Only people change these: a script can't turn itself (or another)
// back on, widen its own switches, or grant itself anything.

// notByScript refuses a call from a script.
func notByScript(ctx context.Context, what string) error {
	if c, ok := script.CallerFrom(ctx); ok {
		return api.PermissionDenied("scripts can't %s (asked by %s)", what, c.Name)
	}
	return nil
}

// byWhom names a person's call for the activity list: who, and how they
// reached the server.
func byWhom(ctx context.Context) string {
	id := remote.IdentityOf(ctx)
	if id.Via == "" || id.Via == remote.Local.String() {
		return id.Principal
	}
	return id.Principal + " via " + id.Via
}

// ownerCaller: the caller is the server's owner, so may see what scripts
// typed. Local callers are; over the tailnet, only when nobody else can get
// in.
func (s *Server) ownerCaller(ctx context.Context) bool {
	if _, ok := script.CallerFrom(ctx); ok {
		return false
	}
	id := remote.IdentityOf(ctx)
	switch id.Via {
	case remote.Local.String():
		return true
	case remote.Tailnet.String(), remote.Tailscale.String(), remote.TailnetServe.String():
		if len(id.Tags) > 0 || id.Login == "" {
			return false
		}
		var pol remote.Policy
		if s.cfg.Remote != nil {
			pol = s.cfg.Remote.Policy
		}
		return !pol.AllowIdentified && !pol.AllowMembers
	}
	return false
}

// uiClientActions are the client actions a script with ui may ask for:
// showing things. The rest (new tabs, splits, closing, resizing) change
// your layout and need control.
var uiClientActions = map[string]bool{
	"client.palette": true, "client.shortcuts": true, "client.reveal": true, "client.sidebar": true,
	"client.session_picker": true, "client.settings": true, "client.mode": true, "client.focus": true,
}

func permOfClientAction(raw json.RawMessage) (string, error) {
	var in struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", api.BadRequest("%v", err)
	}
	if uiClientActions[in.Action] {
		return "ui", nil
	}
	return "control", nil
}

// Buttons the server runs itself, for the script that sent the
// notification: turning it, or one of its switches, off from where it
// says what it did.
const (
	noteSetOption = "hive.set_option"
	noteSetMode   = "hive.set_mode"
)

func builtinNoteAction(name string) bool { return name == noteSetOption || name == noteSetMode }

// checkBuiltinButton validates a built-in button a script offers, and
// says what it does in the server's words: the script picks the title,
// not what the app says pressing it does.
func (s *Server) checkBuiltinButton(scriptName string, a *NoteAction) error {
	var res *script.Resident
	for _, r := range s.scripts.Residents() {
		if r.Name == scriptName {
			r := r
			res = &r
		}
	}
	if res == nil {
		return api.BadRequest("%s buttons are for scripts in the scripts folder", a.Action)
	}
	switch a.Action {
	case noteSetMode:
		m, _ := a.Args["mode"].(string)
		switch m {
		case script.ModeWatch:
			a.Step = scriptName + " will only watch: every change it tries is refused"
		case script.ModeAct:
			a.Step = scriptName + " may act again"
		default:
			return api.BadRequest("%s needs args.mode: act or watch", noteSetMode)
		}
	case noteSetOption:
		key, _ := a.Args["key"].(string)
		for _, o := range res.Options {
			if o.Key != key {
				continue
			}
			v, ok := a.Args["value"]
			if !ok {
				return api.BadRequest("%s needs args.value", noteSetOption)
			}
			if _, err := o.Check(v); err != nil {
				return api.BadRequest("%s: %v", noteSetOption, err)
			}
			title := o.Title
			if title == "" {
				title = o.Key
			}
			a.Step = fmt.Sprintf("Sets %s's %q to %s", scriptName, title, shown(v))
			return nil
		}
		return api.BadRequest("%s: %s has no option %q", noteSetOption, scriptName, key)
	}
	return nil
}

func shown(v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "on"
		}
		return "off"
	case string:
		return fmt.Sprintf("%q", x)
	}
	return fmt.Sprint(v)
}

// runBuiltinButton performs a built-in button for the person who pressed it.
func (s *Server) runBuiltinButton(ctx context.Context, noteID, name string, args json.RawMessage) (json.RawMessage, error) {
	u := &s.ui
	u.mu.Lock()
	var src string
	var offered *NoteAction
	for _, n := range u.notes {
		if n.ID != noteID {
			continue
		}
		src = n.Source
		for i := range n.Actions {
			if n.Actions[i].Action == name {
				a := n.Actions[i]
				offered = &a
			}
		}
	}
	u.mu.Unlock()
	if offered == nil {
		return nil, api.NotFound("notification %s offers no %s (it may be over a day old)", noteID, name)
	}
	scriptName := strings.TrimPrefix(src, "script:")
	// The button's own arguments decide; the press can't change them.
	by := byWhom(ctx) + " (notification button)"
	var res script.Resident
	var err error
	switch name {
	case noteSetMode:
		m, _ := offered.Args["mode"].(string)
		res, err = s.scripts.SetMode(scriptName, m, by)
	case noteSetOption:
		key, _ := offered.Args["key"].(string)
		res, err = s.scripts.SetOption(scriptName, key, offered.Args["value"], by)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

func (s *Server) registerScriptControlMethods() {
	type modeIn struct {
		Name string `json:"name" required:"true" desc:"A script in the scripts folder."`
		Mode string `json:"mode" required:"true" enum:"act,watch" desc:"watch: it may look, badge and notify; every change it tries is refused and listed."`
	}
	api.Register(s.reg, "script.set_mode", "Let a script act, or only watch: in watch mode every change it tries is refused, and the activity list shows it.",
		func(ctx context.Context, in modeIn) (script.Resident, error) {
			if err := notByScript(ctx, "change a script's mode"); err != nil {
				return script.Resident{}, err
			}
			return s.scripts.SetMode(in.Name, in.Mode, byWhom(ctx))
		}, api.Perm("server"))
	type optionIn struct {
		Name  string `json:"name" required:"true" desc:"A script in the scripts folder."`
		Key   string `json:"key" required:"true" desc:"One of the options the script declares."`
		Value any    `json:"value" desc:"true or false, a number, or text, as the option's type says."`
	}
	api.Register(s.reg, "script.set_option", "Change one of a script's switches. The script reads it the next time it looks.",
		func(ctx context.Context, in optionIn) (script.Resident, error) {
			if err := notByScript(ctx, "change a script's options"); err != nil {
				return script.Resident{}, err
			}
			return s.scripts.SetOption(in.Name, in.Key, in.Value, byWhom(ctx))
		}, api.Perm("server"))
}

// scriptManage wraps grant, revoke, enable and disable: people only, and
// listed in the activity.
func (s *Server) scriptManage(ctx context.Context, what, done, name string, f func() (script.Resident, error)) (script.Resident, error) {
	if err := notByScript(ctx, what+" scripts"); err != nil {
		return script.Resident{}, err
	}
	r, err := f()
	if err == nil {
		s.scripts.Note(name, "script."+what, done, byWhom(ctx))
	}
	return r, err
}
