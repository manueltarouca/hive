package script

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"time"

	"github.com/dop251/goja"

	"github.com/manueltarouca/hive/internal/api"
)

// Actions are named things a script can do on request: from the CLI
// (hive do), a notification's button, and later a key or the palette. An
// action runs inside the script that registered it, with its permissions.

var actionName = regexp.MustCompile(`^(rule:)?[a-z0-9][a-z0-9_-]{0,63}$`)

// actionWait is how long an action may take.
const actionWait = 30 * time.Second

// ActionInfo describes an action, for action.list and the palette.
type ActionInfo struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description,omitempty"`
	Category    string         `json:"category,omitempty"`
	Keywords    []string       `json:"keywords,omitempty"`
	Args        map[string]any `json:"args,omitempty"`
	Public      bool           `json:"public,omitempty"`
	Script      string         `json:"script"`
}

type action struct {
	ActionInfo
	run *Run
	fn  goja.Callable
}

// ActionOwner is the script an action belongs to.
func (h *Host) ActionOwner(name string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	a := h.actions[name]
	if a == nil {
		return "", false
	}
	return a.Script, true
}

// Actions lists every action, by name.
func (h *Host) Actions() []ActionInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []ActionInfo{}
	for _, a := range h.actions {
		out = append(out, a.ActionInfo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RunAction runs an action and returns its result as JSON. A script may
// run another script's action only if that one is public and the caller
// already holds everything its owner does.
func (h *Host) RunAction(ctx context.Context, name string, args json.RawMessage, meta map[string]any) (json.RawMessage, error) {
	h.mu.Lock()
	a := h.actions[name]
	h.mu.Unlock()
	if a == nil {
		return nil, api.NotFound("no action %s", name)
	}
	if c, ok := CallerFrom(ctx); ok && c.Name != a.Script {
		if !a.Public {
			return nil, api.PermissionDenied("%s belongs to script %s and isn't public", name, a.Script)
		}
		mine, theirs := PermsFrom(ctx), a.run.permsNow()
		for p := range theirs {
			if !mine.Has(p) {
				return nil, api.PermissionDenied("%s can %s, which the calling script can't", name, p)
			}
		}
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	finish := func(out string, err error) {
		select {
		case done <- result{out, err}:
		default:
		}
	}
	r := a.run
	r.post(func() {
		if r.actionsOff() {
			finish("", api.ScriptUnloaded("%s stopped", a.Script))
			return
		}
		parse, _ := goja.AssertFunction(r.rt.Get("JSON").ToObject(r.rt).Get("parse"))
		argv, err := parse(goja.Undefined(), r.rt.ToValue(string(args)))
		if err != nil {
			finish("", api.BadRequest("args: %v", err))
			return
		}
		m := r.rt.NewObject()
		for k, v := range meta {
			_ = m.Set(k, v)
		}
		r.depth = 0
		_, err = a.fn(goja.Undefined(), argv, m,
			r.rt.ToValue(func(out string) { finish(out, nil) }),
			r.rt.ToValue(func(msg, code string) { finish("", &api.Error{Status: 500, Code: orDefault(code, "error"), Message: msg}) }))
		if err != nil {
			r.handlerErr(err)
			finish("", api.Internal("%s: %s", name, jsError(err)))
		}
	})
	select {
	case res := <-done:
		if res.err != nil {
			return nil, res.err
		}
		if res.out == "" {
			return json.RawMessage("null"), nil
		}
		return json.RawMessage(res.out), nil
	case <-r.done:
		return nil, api.ScriptUnloaded("%s stopped", a.Script)
	case <-time.After(actionWait):
		return nil, api.Timeout("%s took longer than %v", name, actionWait)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// jsRegisterAction adds an action for this run. fn(args, meta, ok, fail)
// is the prelude's wrapper around the script's run function.
func (r *Run) jsRegisterAction(specJSON string, fn goja.Callable) {
	var info ActionInfo
	if err := json.Unmarshal([]byte(specJSON), &info); err != nil {
		panic(r.rt.NewTypeError("action: %v", err))
	}
	if !actionName.MatchString(info.Name) {
		panic(r.rt.NewTypeError("action name %q: lowercase letters, digits, - and _", info.Name))
	}
	info.Script = r.Name
	h := r.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if old := h.actions[info.Name]; old != nil && old.run != r && !old.run.isEnded() {
		panic(r.rt.NewTypeError("action %s is already script %s's", info.Name, old.Script))
	}
	if h.actions == nil {
		h.actions = map[string]*action{}
	}
	h.actions[info.Name] = &action{ActionInfo: info, run: r, fn: fn}
	r.hasActions = true
}

// dropActions removes a run's actions as it ends.
func (h *Host) dropActions(r *Run) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for name, a := range h.actions {
		if a.run == r {
			delete(h.actions, name)
		}
	}
}

func (r *Run) actionsOff() bool { return r.isEnded() || r.rt == nil }
