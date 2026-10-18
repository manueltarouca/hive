package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/manueltarouca/hive/internal/config"
)

// cmdConfig: hive config path|check|show [--json]|set KEY VALUE|default|reload.
// The app runs this too, so the file has one reader and one writer.
func cmdConfig(g globals, args []string) error {
	if len(args) == 0 {
		return usagef("usage: hive config path|check|show [--json]|set KEY VALUE|default|reload")
	}
	if args[0] != "reload" && remoteTarget(g) {
		return usagef("config %s edits this machine's settings file; for %s's, use hive -S %s api call config.get or config.set", args[0], g.server, g.server)
	}
	path := config.Path()
	switch args[0] {
	case "path":
		if len(args) > 1 && args[1] == "--create" {
			if err := config.Create(path); err != nil {
				return err
			}
		}
		fmt.Println(path)
		return nil
	case "default":
		fmt.Print(config.Template())
		return nil
	case "set":
		if len(args) != 3 {
			return usagef("usage: hive config set KEY VALUE")
		}
		return config.Set(path, args[1], args[2])
	case "check":
		_, diags := config.Load(path)
		bad := false
		for _, d := range diags {
			fmt.Printf("%s: %s: %s\n", path, d.Level, d)
			bad = bad || d.Level == config.Error
		}
		if bad {
			return exitError{1}
		}
		return nil
	case "show":
		asJSON := g.json || (len(args) > 1 && args[1] == "--json")
		c, diags := config.Load(path)
		b, err := json.Marshal(c)
		if err != nil {
			return fmt.Errorf("settings: %v", err) // never print a half answer
		}
		var values map[string]any
		_ = json.Unmarshal(b, &values)
		if asJSON {
			if diags == nil {
				diags = []config.Diagnostic{}
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"path": path, "values": values, "diagnostics": diags,
				"keys": config.Keys})
		}
		set := setKeys(path)
		for _, k := range config.Keys {
			note := "default"
			if set[k.Name] {
				note = "set in the file"
			}
			if k.Server {
				note += "; read by the server"
			}
			if k.Name == "theme" && os.Getenv("HIVE_STYLE") != "" {
				note = "HIVE_STYLE overrides it"
			}
			fmt.Printf("%s = %v    # %s\n", k.Name, values[k.Name], note)
		}
		for _, d := range diags {
			fmt.Printf("%s: %s\n", d.Level, d)
		}
		return nil
	case "reload":
		c, err := clientFor(g)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var out map[string]any
		if err := c.Call(ctx, "config.reload", map[string]any{}, &out); err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	return usagef("unknown config command %q", args[0])
}

// setKeys lists the keys the file sets on an active line.
func setKeys(path string) map[string]bool {
	set := map[string]bool{}
	b, err := os.ReadFile(path)
	if err != nil {
		return set
	}
	for raw := range strings.SplitSeq(string(b), "\n") {
		s := strings.TrimSpace(raw)
		if s == "" || s[0] == '#' {
			continue
		}
		if i := strings.IndexByte(s, '='); i > 0 {
			set[strings.TrimSpace(s[:i])] = true
		}
	}
	return set
}
