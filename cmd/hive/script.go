package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/manueltarouca/hive/examples"
	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/config"
	"github.com/manueltarouca/hive/internal/script"
)

const scriptUsage = `usage: hive script run FILE|-e CODE|- [KEY=VALUE...] [--args JSON] [--allow PERMS] [--for DUR] [--all]
       hive script stop RUN_ID
       hive script ls
       hive script show NAME
       hive script grant|revoke|enable|disable NAME
       hive script watch|act NAME
       hive script set NAME KEY=VALUE...
       hive script add [NAME...]
       hive script reload [NAME]
       hive script logs NAME [--tail N] [-f]
       hive script audit [NAME] [--tail N] [-f]
       hive script check FILE
       hive script types [--out FILE]`

func cmdScript(g globals, args []string) error {
	if len(args) == 0 {
		return usagef("%s", scriptUsage)
	}
	switch args[0] {
	case "run":
		return cmdScriptRun(g, args[1:])
	case "stop":
		if len(args) != 2 {
			return usagef("usage: hive script stop RUN_ID")
		}
		x, err := open(g)
		if err != nil {
			return err
		}
		return x.call("script.stop", map[string]any{"run_id": args[1]}, nil)
	case "ls":
		return cmdScriptLs(g)
	case "types":
		return cmdScriptTypes(g, args[1:])
	case "grant", "revoke", "enable", "disable":
		if len(args) != 2 {
			return usagef("usage: hive script %s NAME", args[0])
		}
		return cmdScriptAdmin(g, args[0], args[1])
	case "watch", "act":
		if len(args) != 2 {
			return usagef("usage: hive script %s NAME", args[0])
		}
		x, err := open(g)
		if err != nil {
			return err
		}
		var r script.Resident
		if err := x.call("script.set_mode", map[string]any{"name": args[1], "mode": args[0]}, &r); err != nil {
			return err
		}
		if r.Mode == script.ModeWatch {
			fmt.Printf("%s watches: it may look, mark panes and notify; every change it tries is refused and listed in hive script audit\n", r.Name)
		} else {
			fmt.Printf("%s acts: it may do what it was granted (%s)\n", r.Name, strings.Join(r.Permissions, ", "))
		}
		return nil
	case "set":
		return cmdScriptSet(g, args[1:])
	case "show":
		if len(args) != 2 {
			return usagef("usage: hive script show NAME")
		}
		return cmdScriptShow(g, args[1])
	case "add":
		return cmdScriptAdd(g, args[1:])
	case "reload":
		x, err := open(g)
		if err != nil {
			return err
		}
		params := map[string]any{}
		if len(args) > 1 {
			params["name"] = args[1]
		}
		if err := x.call("script.reload", params, nil); err != nil {
			return err
		}
		return cmdScriptLs(g)
	case "logs":
		return cmdScriptLogs(g, args[1:])
	case "audit":
		return cmdScriptAudit(g, args[1:])
	case "check":
		if len(args) != 2 {
			return usagef("usage: hive script check FILE")
		}
		if _, err := script.Bundle(args[1], "", ""); err != nil {
			return err
		}
		fmt.Println("ok")
		return nil
	}
	return usagef("%s", scriptUsage)
}

// scriptSource reads a script: a file, -e CODE, or - for stdin.
func scriptSource(fsArgs []string, code string) (js string, rest []string, err error) {
	wd, _ := os.Getwd()
	switch {
	case code != "":
		js, err = script.Bundle("", code, wd)
		return js, fsArgs, err
	case len(fsArgs) == 0:
		return "", nil, usagef("%s", scriptUsage)
	case fsArgs[0] == "-":
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", nil, err
		}
		js, err = script.Bundle("", string(b), wd)
		return js, fsArgs[1:], err
	}
	js, err = script.Bundle(fsArgs[0], "", "")
	return js, fsArgs[1:], err
}

func cmdScriptRun(g globals, args []string) error {
	fs := flags("script run")
	code := fs.String("e", "", "")
	argsJSON := fs.String("args", "", "")
	allow := fs.String("allow", "", "")
	forDur := fs.Duration("for", 0, "")
	all := fs.Bool("all", false, "")
	// Flags may come before or after the file and its KEY=VALUE pairs.
	var positional []string
	for {
		if err := parse(fs, args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	js, kv, err := scriptSource(positional, *code)
	if err != nil {
		return err
	}
	scriptArgs := map[string]any{}
	if *argsJSON != "" {
		if err := json.Unmarshal([]byte(*argsJSON), &scriptArgs); err != nil {
			return usagef("--args must be a JSON object: %v", err)
		}
	}
	for _, p := range kv {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return usagef("%q: arguments are KEY=VALUE", p)
		}
		scriptArgs[k] = v
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	params := map[string]any{"code": js, "args": scriptArgs, "all": *all}
	if n := runName(positional, *code); n != "" {
		params["name"] = n
	}
	if *allow != "" {
		params["allow"] = strings.Split(*allow, ",")
	}
	if *forDur > 0 {
		params["for"] = forDur.Seconds()
	}
	// The pane it runs from, when there is one: inside a hive terminal, or
	// named with -s or -b.
	if g.block != "" || g.session != "" || (!x.c.Remote() && os.Getenv("HIVE_SESSION") != "") {
		if sid, bid, err := x.blockID(); err == nil {
			params["session_id"], params["block_id"] = sid, bid
		} else if sid, err := x.sessionID(); err == nil {
			params["session_id"] = sid
		} else {
			return err
		}
	}
	var started struct {
		RunID string `json:"run_id"`
		Seq   int64  `json:"seq"`
	}
	if err := x.c.Call(context.Background(), "script.run", params, &started); err != nil {
		return err
	}
	// Ctrl-C stops the script, then the command waits for it to end.
	go func() {
		<-x.ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = x.c.Call(ctx, "script.stop", map[string]any{"run_id": started.RunID}, nil)
	}()
	var end struct {
		OK        bool            `json:"ok"`
		Result    json.RawMessage `json:"result"`
		Truncated bool            `json:"result_truncated"`
		Error     string          `json:"error"`
		Code      string          `json:"code"`
	}
	ended := false
	err = x.c.EventsOf(context.Background(), []string{"script_log", "script_ended"}, started.Seq, func(e client.Event) bool {
		var p struct {
			RunID string `json:"run_id"`
			Level string `json:"level"`
			Text  string `json:"text"`
		}
		if json.Unmarshal(e.Payload, &p) != nil || p.RunID != started.RunID {
			return true
		}
		switch e.Type {
		case "script_log":
			if g.json {
				os.Stderr.Write(append(append([]byte(nil), e.Raw...), '\n'))
			} else if p.Level == "info" {
				fmt.Fprintln(os.Stderr, p.Text)
			} else {
				fmt.Fprintf(os.Stderr, "%s: %s\n", p.Level, p.Text)
			}
		case "script_ended":
			_ = json.Unmarshal(e.Payload, &end)
			ended = true
			return false
		}
		return true
	})
	if !ended {
		if err == nil {
			err = errors.New("the server stopped before the script ended")
		}
		return err
	}
	if end.Truncated {
		var full struct {
			Result json.RawMessage `json:"result"`
		}
		if err := x.c.Call(context.Background(), "script.result", map[string]any{"run_id": started.RunID}, &full); err == nil {
			end.Result = full.Result
		}
	}
	if len(end.Result) > 0 {
		if err := printRaw(end.Result); err != nil {
			return err
		}
	}
	if !end.OK {
		if end.Code == "script_unloaded" && x.ctx.Err() != nil {
			return context.Canceled
		}
		fmt.Fprintln(os.Stderr, "Error:", end.Error)
		return exitError{exitFail}
	}
	return nil
}

func cmdScriptLs(g globals) error {
	x, err := open(g)
	if err != nil {
		return err
	}
	raw, err := x.c.CallRaw(x.ctx, "script.list", nil)
	if err != nil {
		return err
	}
	if g.json {
		return printRaw(raw)
	}
	var out struct {
		Scripts []script.Resident `json:"scripts"`
		Runs    []script.RunInfo  `json:"runs"`
	}
	_ = json.Unmarshal(raw, &out)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if len(out.Scripts) > 0 {
		fmt.Fprintln(tw, "SCRIPT\tSTATE\tMODE\tPERMISSIONS\tERRORS\tNOTE")
		for _, r := range out.Scripts {
			perms, note := strings.Join(r.Permissions, ","), firstLine(r.Error)
			if r.State == script.StatePending {
				perms = "asks " + strings.Join(r.Asks, ",")
				note = "hive script grant " + r.Name
			}
			if perms == "" {
				perms = "read"
			}
			mode := r.Mode
			if mode == "" {
				mode = script.ModeAct
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n", r.Name, r.State, mode, perms, r.Errors, note)
		}
		fmt.Fprintln(tw)
	}
	var runs []script.RunInfo
	for _, r := range out.Runs {
		if !isResident(r, out.Scripts) {
			runs = append(runs, r)
		}
	}
	if len(runs) > 0 {
		fmt.Fprintln(tw, "RUN\tNAME\tSTATE\tPERMISSIONS\tSTARTED\tERROR")
		for _, r := range runs {
			state := r.State
			if r.State == "ended" && !r.OK {
				state = "failed"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.RunID, r.Name, state, strings.Join(r.Permissions, ","),
				r.Started.Local().Format("15:04:05"), firstLine(r.Error))
		}
	}
	if len(out.Scripts) == 0 && len(runs) == 0 {
		fmt.Fprintln(os.Stderr, "No scripts. Put them in the scripts folder, or run one with hive script run FILE.")
	}
	return tw.Flush()
}

func isResident(r script.RunInfo, rs []script.Resident) bool {
	for _, s := range rs {
		if s.Name == r.Name {
			return true
		}
	}
	return false
}

// dangerous pairs, said in plain words before a grant.
var dangerous = []struct {
	a, b, why string
}{
	{"screen", "input", "it can read your terminals and type into them, agents included"},
	{"screen", "exec", "it can read your terminals and run programs with what it read"},
	{"screen", "network", "it can read your terminals and send what it read elsewhere"},
}

func cmdScriptAdmin(g globals, verb, name string) error {
	x, err := open(g)
	if err != nil {
		return err
	}
	var r script.Resident
	if err := x.call("script."+verb, map[string]any{"name": name}, &r); err != nil {
		return err
	}
	if g.json {
		return printJSON(os.Stdout, r)
	}
	switch verb {
	case "grant":
		perms := append([]string{"read"}, r.Permissions...)
		fmt.Printf("%s may now: %s\n", r.Name, strings.Join(perms, ", "))
		for _, d := range dangerous {
			if slices.Contains(r.Permissions, d.a) && slices.Contains(r.Permissions, d.b) {
				fmt.Printf("Note: %s.\n", d.why)
			}
		}
	default:
		fmt.Printf("%s is %s\n", r.Name, r.State)
	}
	return nil
}

func cmdScriptLogs(g globals, args []string) error {
	fs := flags("script logs")
	tail := fs.Int("tail", 50, "")
	follow := fs.Bool("f", false, "")
	var name string
	for {
		if err := parse(fs, args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		name, args = fs.Arg(0), fs.Args()[1:]
	}
	if name == "" {
		return usagef("usage: hive script logs NAME [--tail N] [-f]")
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	var out struct {
		Lines []script.LogLine `json:"lines"`
	}
	if err := x.call("script.logs", map[string]any{"name": name, "tail": *tail}, &out); err != nil {
		return err
	}
	for _, l := range out.Lines {
		printLog(l.Time, l.Level, l.Text)
	}
	if !*follow {
		return nil
	}
	err = x.c.EventsOf(x.ctx, []string{"script_log"}, -1, func(e client.Event) bool {
		var p struct {
			Name  string `json:"name"`
			Level string `json:"level"`
			Text  string `json:"text"`
		}
		if json.Unmarshal(e.Payload, &p) == nil && p.Name == name {
			printLog(e.Time, p.Level, p.Text)
		}
		return true
	})
	if errors.Is(x.ctx.Err(), context.Canceled) {
		return nil
	}
	return err
}

func printLog(t time.Time, level, text string) {
	fmt.Printf("%s %-5s %s\n", t.Local().Format("15:04:05"), level, text)
}

func cmdScriptAudit(g globals, args []string) error {
	fs := flags("script audit")
	tail := fs.Int("tail", 50, "")
	follow := fs.Bool("f", false, "")
	var name string
	for {
		if err := parse(fs, args); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		name, args = fs.Arg(0), fs.Args()[1:]
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	var last uint64
	fetch := func(n int) ([]script.Entry, json.RawMessage, error) {
		params := map[string]any{"name": name, "tail": n}
		if n == 0 {
			params = map[string]any{"name": name, "since": max(last, 1)}
		}
		raw, err := x.c.CallRaw(x.ctx, "audit.list", params)
		if err != nil {
			return nil, nil, err
		}
		var out struct {
			Entries []script.Entry `json:"entries"`
		}
		_ = json.Unmarshal(raw, &out)
		return out.Entries, raw, nil
	}
	entries, raw, err := fetch(*tail)
	if err != nil {
		return err
	}
	if g.json && !*follow {
		return printRaw(raw)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tSCRIPT\tWHAT\tRESULT")
	show := func(es []script.Entry) {
		for _, e := range es {
			if e.Seq != 0 && e.Seq <= last {
				continue
			}
			last = max(last, e.Seq)
			who := e.Script
			if who == "" {
				who = strings.TrimPrefix(e.Principal, "script:")
			}
			what := e.Summary
			if what == "" {
				what = e.Method
			}
			if e.Preview != "" {
				what += fmt.Sprintf(" %q", e.Preview)
			}
			if !strings.HasPrefix(e.Principal, "script:") {
				what += " (by " + e.Principal + ")"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.Time.Local().Format("01-02 15:04:05"), who, what, e.Result)
		}
		tw.Flush()
	}
	show(entries)
	for *follow {
		time.Sleep(time.Second)
		es, _, err := fetch(0) // what's new, from the server's memory
		if err != nil {
			return err
		}
		show(es)
	}
	return nil
}

func cmdScriptSet(g globals, args []string) error {
	if len(args) < 2 {
		return usagef("usage: hive script set NAME KEY=VALUE...")
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	var list struct {
		Scripts []script.Resident `json:"scripts"`
	}
	if err := x.call("script.list", nil, &list); err != nil {
		return err
	}
	types := map[string]string{}
	for _, r := range list.Scripts {
		if r.Name == args[0] {
			for _, o := range r.Options {
				types[o.Key] = o.Type
			}
		}
	}
	var r script.Resident
	for _, kv := range args[1:] {
		key, val, ok := strings.Cut(kv, "=")
		if !ok {
			return usagef("%s: KEY=VALUE", kv)
		}
		var v any = val
		switch types[key] {
		case "bool":
			b, err := strconv.ParseBool(val)
			if err != nil {
				return fmt.Errorf("%s takes true or false", key)
			}
			v = b
		case "number":
			f, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return fmt.Errorf("%s takes a number", key)
			}
			v = f
		}
		if err := x.call("script.set_option", map[string]any{"name": args[0], "key": key, "value": v}, &r); err != nil {
			return err
		}
	}
	return printOptions(r)
}

func printOptions(r script.Resident) error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "OPTION\tVALUE\tWHAT")
	for _, o := range r.Options {
		v := fmt.Sprint(o.Value)
		if s, ok := o.Value.(string); ok {
			v = fmt.Sprintf("%q", s)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", o.Key, v, o.Title)
	}
	return tw.Flush()
}

// cmdScriptShow is one script in full: what it is, may do, its switches,
// and what it did lately.
func cmdScriptShow(g globals, name string) error {
	x, err := open(g)
	if err != nil {
		return err
	}
	var list struct {
		Scripts []script.Resident `json:"scripts"`
	}
	if err := x.call("script.list", nil, &list); err != nil {
		return err
	}
	var r *script.Resident
	for i := range list.Scripts {
		if list.Scripts[i].Name == name {
			r = &list.Scripts[i]
		}
	}
	if r == nil {
		return fmt.Errorf("no script named %s in the scripts folder", name)
	}
	if g.json {
		return printJSON(os.Stdout, r)
	}
	fmt.Printf("%s: %s\n", r.Name, r.State)
	if r.Description != "" {
		fmt.Println(r.Description)
	}
	perms := append([]string{"read"}, r.Permissions...)
	if r.State == script.StatePending {
		fmt.Printf("Asks for: %s (hive script grant %s)\n", strings.Join(r.Asks, ", "), r.Name)
	} else {
		fmt.Printf("May: %s\n", strings.Join(perms, ", "))
	}
	if r.Mode == script.ModeWatch {
		fmt.Println("Mode: watch (changes are refused)")
	} else {
		fmt.Println("Mode: act")
	}
	if r.Error != "" {
		fmt.Println("Error: " + firstLine(r.Error))
	}
	fmt.Println(r.Path)
	if len(r.Options) > 0 {
		fmt.Println()
		if err := printOptions(*r); err != nil {
			return err
		}
	}
	fmt.Println()
	return cmdScriptAudit(g, []string{name, "--tail", "10"})
}

// cmdScriptAdd lists the starter scripts, or copies some into the scripts
// folder, where they wait for a grant.
func cmdScriptAdd(g globals, names []string) error {
	if len(names) == 0 {
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "STARTER\tASKS\tDOES")
		for _, s := range examples.Starters() {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", s.Name, s.Permissions, s.Description)
		}
		tw.Flush()
		fmt.Fprintln(os.Stderr, "\nhive script add NAME copies one into "+config.Scripts()+"; hive script grant NAME starts it.")
		return nil
	}
	if g.explicitServer {
		return fmt.Errorf("add copies into this machine's scripts folder; for another host, copy the file there")
	}
	dir := config.Scripts()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, n := range names {
		s, ok := examples.Get(n)
		if !ok {
			return fmt.Errorf("no starter named %s (hive script add lists them)", n)
		}
		p := filepath.Join(dir, n+".ts")
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("%s already exists: remove it first to get the starter again", p)
		}
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return fmt.Errorf("%s already exists as a folder", filepath.Join(dir, n))
		}
		if err := os.WriteFile(p, []byte(s.Code), 0o644); err != nil {
			return err
		}
		fmt.Printf("added %s: it asks for %s. Start it with: hive script grant %s\n", p, s.Permissions, n)
	}
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// cmdScriptTypes writes hive.d.ts for the server it talks to, so editors
// check scripts against the methods that server has.
func cmdScriptTypes(g globals, args []string) error {
	fs := flags("script types")
	out := fs.String("out", "hive.d.ts", "")
	if err := parse(fs, args); err != nil {
		return err
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	var methods []script.MethodSchema
	if err := x.call("api.list", map[string]any{"full": true}, &methods); err != nil {
		return err
	}
	var ver struct {
		Version string `json:"hive_version"`
	}
	if err := x.call("server.version", nil, &ver); err != nil {
		return err
	}
	dts := script.Types(ver.Version, methods)
	if *out == "-" {
		_, err := os.Stdout.WriteString(dts)
		return err
	}
	if err := os.WriteFile(*out, []byte(dts), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%d methods)\n", *out, len(methods))
	return nil
}

// runName names a run after its file (tests.ts: tests), when the name is
// one hive takes.
func runName(positional []string, code string) string {
	if code != "" || len(positional) == 0 || positional[0] == "-" {
		return ""
	}
	base := filepath.Base(positional[0])
	if base == "index.ts" || base == "index.js" {
		base = filepath.Base(filepath.Dir(positional[0]))
	}
	name, _, _ := strings.Cut(strings.ToLower(base), ".")
	for _, r := range name {
		if !(r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	if name == "" || len(name) > 64 || name[0] == '-' || name[0] == '_' {
		return ""
	}
	return name
}
