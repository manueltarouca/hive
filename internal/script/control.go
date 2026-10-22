package script

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/manueltarouca/hive/internal/api"
)

// What the owner sees of scripts and how they turn them off: every call
// that changes something is an activity entry and a script_acted event; a
// script in watch mode may badge and notify but every such call is
// refused; options are a script's own switches, set only by people (never
// by a script).

// Modes.
const (
	ModeAct   = "act"   // it may do what it was granted
	ModeWatch = "watch" // it may read, badge and notify; changes are refused
)

// auditedMethods change something the owner sees without needing a
// changing permission: they are logged too.
var auditedMethods = map[string]bool{
	"client.perform": true, "notification.ack": true, "script.run": true, "action.run": true,
}

// perScript is how many activity entries are kept in memory per script.
const perScript = 100

// Option is a switch or a value a script declares in its manifest.
type Option struct {
	Key     string   `json:"key"`
	Title   string   `json:"title,omitempty"`
	Type    string   `json:"type" enum:"bool,number,text"`
	Default any      `json:"default"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Value   any      `json:"value"`
}

var optionKey = nameRE

// parseOptions reads a manifest's options.
func parseOptions(raw map[string]json.RawMessage) ([]Option, error) {
	var out []Option
	for key, b := range raw {
		if !optionKey.MatchString(key) || len(key) > 40 {
			return nil, fmt.Errorf("option %q: lowercase letters, digits, - and _", key)
		}
		var o struct {
			Title   string   `json:"title"`
			Type    string   `json:"type"`
			Default any      `json:"default"`
			Min     *float64 `json:"min"`
			Max     *float64 `json:"max"`
		}
		if err := json.Unmarshal(b, &o); err != nil {
			return nil, fmt.Errorf("option %s: %v", key, err)
		}
		if o.Type == "" {
			switch o.Default.(type) {
			case bool:
				o.Type = "bool"
			case float64:
				o.Type = "number"
			default:
				o.Type = "text"
			}
		}
		opt := Option{Key: key, Title: clipText(o.Title, 120), Type: o.Type, Min: o.Min, Max: o.Max}
		def, err := opt.Check(o.Default)
		if err != nil {
			return nil, fmt.Errorf("option %s default: %v", key, err)
		}
		opt.Default, opt.Value = def, def
		out = append(out, opt)
	}
	sortOptions(out)
	return out, nil
}

func sortOptions(o []Option) {
	for i := 1; i < len(o); i++ {
		for j := i; j > 0 && o[j].Key < o[j-1].Key; j-- {
			o[j], o[j-1] = o[j-1], o[j]
		}
	}
}

// Check validates a value for the option, returning it in its JSON shape.
func (o Option) Check(v any) (any, error) {
	switch o.Type {
	case "bool":
		if v == nil {
			return false, nil
		}
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("%s takes true or false", o.Key)
		}
		return b, nil
	case "number":
		if v == nil {
			v = 0.0
		}
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("%s takes a number", o.Key)
		}
		if o.Min != nil && n < *o.Min || o.Max != nil && n > *o.Max {
			return nil, fmt.Errorf("%s takes a number from %v to %v", o.Key, deref(o.Min, math.Inf(-1)), deref(o.Max, math.Inf(1)))
		}
		return n, nil
	case "text":
		if v == nil {
			return "", nil
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s takes text", o.Key)
		}
		if len(s) > 1000 {
			return nil, fmt.Errorf("%s: at most 1000 bytes", o.Key)
		}
		return s, nil
	}
	return nil, fmt.Errorf("type %q: bool, number or text", o.Type)
}

func deref(p *float64, d float64) float64 {
	if p == nil {
		return d
	}
	return *p
}

func readJSON(path string, v any) {
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, v)
	}
}

func clipText(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (rs *residents) modesPath() string {
	return filepath.Join(rs.h.cfg.StateDir, "scripts", "modes.json")
}

func (rs *residents) optionsPath() string {
	return filepath.Join(rs.h.cfg.StateDir, "scripts", "options.json")
}

// modes and optionValues are read under rs.mu.
func (rs *residents) modes() map[string]string {
	m := map[string]string{}
	readJSON(rs.modesPath(), &m)
	return m
}

func (rs *residents) optionValues() map[string]map[string]any {
	m := map[string]map[string]any{}
	readJSON(rs.optionsPath(), &m)
	return m
}

// applyValues fills declared options with the values people set; a value
// that no longer fits the declaration falls back to the default.
func (rs *residents) applyValues(name string, opts []Option) []Option {
	vals := rs.optionValues()[name]
	for i := range opts {
		opts[i].Value = opts[i].Default
		if v, ok := vals[opts[i].Key]; ok {
			if c, err := opts[i].Check(v); err == nil {
				opts[i].Value = c
			}
		}
	}
	return opts
}

// modeOf is a script's mode: act unless someone set watch.
func (rs *residents) modeOf(name string) string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if r := rs.all[name]; r != nil && r.Mode != "" {
		return r.Mode
	}
	return ModeAct
}

// SetMode puts a script in act or watch mode. by says who, for the
// activity list.
func (h *Host) SetMode(name, mode, by string) (Resident, error) {
	rs := h.res
	if rs == nil {
		return Resident{}, api.BadRequest("this server has no scripts folder")
	}
	if mode != ModeAct && mode != ModeWatch {
		return Resident{}, api.BadRequest("mode: act or watch")
	}
	r, err := rs.get(name)
	if err != nil {
		return Resident{}, err
	}
	rs.mu.Lock()
	m := rs.modes()
	if mode == ModeAct {
		delete(m, name)
	} else {
		m[name] = mode
	}
	err = writeJSON(rs.modesPath(), m)
	if err == nil {
		r.Mode = mode
	}
	rs.mu.Unlock()
	if err != nil {
		return Resident{}, api.Internal("modes: %v", err)
	}
	h.record(Entry{Time: time.Now().UTC(), Principal: by, Script: name, Method: "script.set_mode",
		Summary: "mode " + mode, Result: "ok"}, "")
	h.changed(name, map[string]any{"mode": mode, "by": by})
	return h.resident(name)
}

// SetOption changes one of a script's options. The script reads the new
// value the next time it looks, and hears options_changed.
func (h *Host) SetOption(name, key string, value any, by string) (Resident, error) {
	rs := h.res
	if rs == nil {
		return Resident{}, api.BadRequest("this server has no scripts folder")
	}
	r, err := rs.get(name)
	if err != nil {
		return Resident{}, err
	}
	rs.mu.Lock()
	var opt *Option
	for i := range r.Options {
		if r.Options[i].Key == key {
			opt = &r.Options[i]
		}
	}
	if opt == nil {
		rs.mu.Unlock()
		return Resident{}, api.NotFound("%s has no option %s", name, key)
	}
	v, cerr := opt.Check(value)
	if cerr != nil {
		rs.mu.Unlock()
		return Resident{}, api.BadRequest("%v", cerr)
	}
	all := rs.optionValues()
	if all[name] == nil {
		all[name] = map[string]any{}
	}
	all[name][key] = v
	if err := writeJSON(rs.optionsPath(), all); err != nil {
		rs.mu.Unlock()
		return Resident{}, api.Internal("options: %v", err)
	}
	opt.Value = v
	run := r.run
	rs.mu.Unlock()
	h.record(Entry{Time: time.Now().UTC(), Principal: by, Script: name, Method: "script.set_option",
		Summary: fmt.Sprintf("%s = %v", key, showValue(v)), Result: "ok"}, "")
	h.changed(name, map[string]any{"option": key, "value": v, "by": by})
	if run != nil {
		p := map[string]any{"key": key, "value": v, "by": by}
		b, _ := json.Marshal(p)
		run.enqueue(event{Name: "options_changed", Payload: p, JSON: string(b)})
	}
	return h.resident(name)
}

func showValue(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", clipText(s, 40))
	}
	return fmt.Sprint(v)
}

// changed tells the apps a script's page needs redrawing.
func (h *Host) changed(name string, extra map[string]any) {
	p := map[string]any{"name": name}
	for k, v := range extra {
		p[k] = v
	}
	h.cfg.Backend.Bus().Server("script_changed", p)
}

// option is a run's current value for a key: a resident's live value, or
// the default a one-shot declared.
func (r *Run) option(key string) (any, bool) {
	if res := r.opts.Resident; res != nil && r.host.res != nil {
		rs := r.host.res
		rs.mu.Lock()
		defer rs.mu.Unlock()
		for _, o := range res.Options {
			if o.Key == key {
				return o.Value, true
			}
		}
		return nil, false
	}
	r.pmu.Lock()
	defer r.pmu.Unlock()
	for _, o := range r.declared {
		if o.Key == key {
			return o.Default, true
		}
	}
	return nil, false
}

// root is the folder script a run belongs to: itself, or the one that
// started it (a one-shot started by a resident is that resident's doing).
func (r *Run) root() *Resident {
	for x, n := r, 0; x != nil && n < 16; n++ {
		if x.opts.Resident != nil {
			return x.opts.Resident
		}
		x = x.parent
	}
	return nil
}

// owner names the script an entry belongs to: the root's, or the run's.
func (r *Run) owner() string {
	if res := r.root(); res != nil {
		return res.Name
	}
	return r.Name
}

// watching reports whether the run's script is in watch mode.
func (r *Run) watching() bool {
	res := r.root()
	if res == nil || r.host.res == nil {
		return false
	}
	return r.host.res.modeOf(res.Name) == ModeWatch
}

// record keeps an entry: in the audit file (without the preview), in the
// script's memory ring (with it), and as a script_acted event.
func (h *Host) record(e Entry, preview string) {
	h.mu.Lock()
	h.actSeq++
	e.Seq = h.actSeq
	if h.activity == nil {
		h.activity = map[string][]Entry{}
	}
	mem := e
	mem.Preview = preview
	ring := append(h.activity[e.Script], mem)
	if len(ring) > perScript {
		ring = ring[len(ring)-perScript:]
	}
	h.activity[e.Script] = ring
	h.mu.Unlock()
	h.audit.write(e)
	ev := map[string]any{"script": e.Script, "method": e.Method, "summary": e.Summary,
		"result": e.Result, "seq": e.Seq, "time": e.Time, "by": e.Principal}
	if e.RunID != "" {
		ev["run_id"] = e.RunID
	}
	if e.BlockID != "" {
		ev["block_id"] = e.BlockID
	}
	if e.SessionID != "" {
		ev["session_id"] = e.SessionID
	}
	h.cfg.Backend.Bus().Server("script_acted", ev)
}

// previews are the typed text the memory ring keeps, by seq.
func (h *Host) previews() map[uint64]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[uint64]string{}
	for _, ring := range h.activity {
		for _, e := range ring {
			if e.Preview != "" {
				out[e.Seq] = e.Preview
			}
		}
	}
	return out
}

// preview is what the activity list shows of typed text: the first 60
// characters, newlines as ⏎.
func preview(s string) string {
	s = strings.NewReplacer("\r\n", "⏎", "\r", "⏎", "\n", "⏎", "\x1b", "⎋", "\t", "⇥").Replace(s)
	return clipText(s, 60)
}

// summarize says what a call did in a few words, for the apps.
func summarize(method string, args map[string]any, typed string) string {
	inner, _ := args["args"].(map[string]any)
	keys := func(v any) string {
		var ks []string
		switch k := v.(type) {
		case []any:
			for _, x := range k {
				ks = append(ks, fmt.Sprint(x))
			}
		case string:
			ks = []string{k}
		}
		return strings.Join(ks, ", ")
	}
	switch method {
	case "hive.terminal.write":
		return fmt.Sprintf("typed %d characters", len([]rune(typed)))
	case "hive.terminal.send_key":
		if inner != nil {
			return "pressed " + keys(inner["key"]) + keys(inner["keys"])
		}
	case "agent.act":
		var parts []string
		if k := keys(args["send_key"]); k != "" {
			parts = append(parts, "pressed "+k)
		}
		if typed != "" {
			parts = append(parts, fmt.Sprintf("typed %d characters", len([]rune(typed))))
		}
		return strings.Join(parts, ", ")
	case "client.perform":
		return "asked the app to " + strings.TrimPrefix(fmt.Sprint(args["action"]), "client.")
	case "action.run":
		return "ran the action " + fmt.Sprint(args["name"])
	case "script.run":
		return "started a script"
	case "notification.ack":
		return "marked a notification seen"
	}
	name := method
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return strings.ReplaceAll(name, "_", " ")
}

// typedText is the text a call types, if any.
func typedText(method string, args map[string]any) string {
	switch method {
	case "agent.act":
		s, _ := args["write"].(string)
		return s
	case "hive.terminal.write":
		if inner, ok := args["args"].(map[string]any); ok {
			if s, ok := inner["data"].(string); ok {
				return s
			}
		}
	}
	return ""
}

// target finds the block or session a call touched.
func target(args map[string]any) (block, session string) {
	pick := func(m map[string]any) {
		if m == nil {
			return
		}
		if s, ok := m["block_id"].(string); ok && block == "" {
			block = s
		}
		if s, ok := m["session_id"].(string); ok && session == "" {
			session = s
		}
	}
	pick(args)
	inner, _ := args["args"].(map[string]any)
	pick(inner)
	return
}

// Activity is what scripts did, newest last: one script's, or all.
// withPreview includes typed text (for the owner's apps).
func (h *Host) Activity(name string, n int, withPreview bool) []Entry {
	var entries []Entry
	if h.audit == nil {
		entries = h.recent(name, 0, n)
	} else {
		entries = h.Audit(name, n)
	}
	if !withPreview {
		return entries
	}
	pv := h.previews()
	for i := range entries {
		entries[i].Preview = pv[entries[i].Seq]
	}
	return entries
}

// Note records something a person did to a script (granted, turned off).
func (h *Host) Note(name, method, summary, by string) {
	h.record(Entry{Time: time.Now().UTC(), Principal: by, Script: name, Method: method, Summary: summary, Result: "ok"}, "")
	h.changed(name, map[string]any{"by": by, "what": method})
}

// recent is what the memory ring holds after seq, oldest first: cheap, for
// following along.
func (h *Host) recent(name string, since uint64, n int) []Entry {
	h.mu.Lock()
	var out []Entry
	for script, ring := range h.activity {
		if name != "" && script != name {
			continue
		}
		for _, e := range ring {
			if e.Seq > since {
				out = append(out, e)
			}
		}
	}
	h.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// ActivitySince is what scripts did after seq, from memory.
func (h *Host) ActivitySince(name string, since uint64, withPreview bool) []Entry {
	out := h.recent(name, since, 0)
	if !withPreview {
		for i := range out {
			out[i].Preview = ""
		}
	}
	return out
}
