package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/script"
)

const scriptUsage = `usage: hive script run FILE|-e CODE|- [KEY=VALUE...] [--args JSON] [--allow PERMS] [--for DUR] [--all]
       hive script stop RUN_ID
       hive script ls
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
		Runs []script.RunInfo `json:"runs"`
	}
	_ = json.Unmarshal(raw, &out)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUN\tNAME\tSTATE\tPERMISSIONS\tSTARTED\tERROR")
	for _, r := range out.Runs {
		state := r.State
		if r.State == "ended" && !r.OK {
			state = "failed"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.RunID, r.Name, state, strings.Join(r.Permissions, ","),
			r.Started.Local().Format("15:04:05"), firstLine(r.Error))
	}
	return tw.Flush()
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
