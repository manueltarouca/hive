package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// hive keymap: what keys do, and where each binding came from.
func cmdKeymap(g globals, args []string) error {
	fs := flags("keymap")
	kind := fs.String("client", "mac", "")
	if err := parse(fs, args); err != nil {
		return err
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	raw, err := x.c.CallRaw(x.ctx, "keymap.get", map[string]any{"client_kind": *kind})
	if err != nil {
		return err
	}
	if g.json {
		return printRaw(raw)
	}
	var km struct {
		Bindings []struct {
			Key, Mode, Action, Title, Source string
			Args                             json.RawMessage
		}
		Warnings []string
	}
	_ = json.Unmarshal(raw, &km)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tACTION\tFROM")
	for _, b := range km.Bindings {
		key := b.Key
		if b.Mode != "" {
			key = b.Mode + "/" + key
		}
		action := b.Action
		if len(b.Args) > 0 {
			action += " " + string(b.Args)
		}
		if b.Title != "" {
			action += "  (" + b.Title + ")"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", key, action, b.Source)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	for _, w := range km.Warnings {
		fmt.Fprintln(os.Stderr, "Note:", w)
	}
	return nil
}

// hive actions: what scripts offer.
func cmdActions(g globals) error {
	x, err := open(g)
	if err != nil {
		return err
	}
	raw, err := x.c.CallRaw(x.ctx, "action.list", nil)
	if err != nil {
		return err
	}
	if g.json {
		return printRaw(raw)
	}
	var out struct {
		Actions []struct{ Name, Title, Script string }
	}
	_ = json.Unmarshal(raw, &out)
	if len(out.Actions) == 0 {
		fmt.Fprintln(os.Stderr, "No actions. Scripts register them with h.actions.register.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ACTION\tTITLE\tSCRIPT")
	for _, a := range out.Actions {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", a.Name, a.Title, a.Script)
	}
	return tw.Flush()
}

// hive do ACTION [KEY=VALUE...]: run a script's action, from the pane you're in.
func cmdDo(g globals, args []string) error {
	if len(args) == 0 {
		return usagef("usage: hive do ACTION [KEY=VALUE...] [--args JSON]")
	}
	name, rest := args[0], args[1:]
	fs := flags("do")
	argsJSON := fs.String("args", "", "")
	var kv []string
	for {
		if err := parse(fs, rest); err != nil {
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		kv, rest = append(kv, fs.Arg(0)), fs.Args()[1:]
	}
	a := map[string]any{}
	if *argsJSON != "" {
		if err := json.Unmarshal([]byte(*argsJSON), &a); err != nil {
			return usagef("--args must be a JSON object: %v", err)
		}
	}
	for _, p := range kv {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			return usagef("%q: arguments are KEY=VALUE", p)
		}
		a[k] = v
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	params := map[string]any{"name": name, "args": a, "origin": "cli"}
	if g.block != "" || g.session != "" || (!x.c.Remote() && os.Getenv("HIVE_SESSION") != "") {
		if sid, bid, err := x.blockID(); err == nil {
			params["session_id"], params["block_id"] = sid, bid
		}
	}
	raw, err := x.c.CallRaw(x.ctx, "action.run", params)
	if err != nil {
		return err
	}
	if string(raw) != "null" && len(raw) > 0 {
		return printRaw(raw)
	}
	return nil
}
