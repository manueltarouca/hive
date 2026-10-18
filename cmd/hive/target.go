package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/hosts"
)

// clientFor is the server a command talks to. -S (or HIVE_SERVER) is:
//
//	empty                      this machine's server
//	/PATH, ./PATH, ~/PATH       a Unix socket (also any existing socket file)
//	https://HOST, http://HOST:PORT
//	                           a remote server
//	LABEL                      a host saved with `hive hosts add`
//	HOST                       a remote server: https:443, then https and
//	                           http on hive's port
//
// HIVE_SERVER is always a socket path: the server sets it in every pane,
// and agent hooks and the app read it as one.
func clientFor(g globals) (*client.Client, error) {
	if !g.explicitServer || g.server == "" {
		return client.New(socketFor(g)), nil
	}
	return resolve(g.server)
}

// pathLike is a -S value spelled as a socket path.
func pathLike(s string) bool {
	for _, p := range []string{"/", "./", "../", "~/"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	fi, err := os.Stat(s)
	return err == nil && fi.Mode()&os.ModeSocket != 0
}

// remoteTarget reports, from its spelling alone (no network), whether -S
// names another machine's server.
func remoteTarget(g globals) bool {
	if !g.explicitServer || g.server == "" || pathLike(g.server) {
		return false
	}
	return true
}

// The resolver and the prober, replaced in tests.
var (
	lookupHost = net.DefaultResolver.LookupHost
	probe      = client.Probe
)

func resolve(s string) (*client.Client, error) {
	switch {
	case strings.Contains(s, "://"):
		return client.NewURL(s)
	case strings.HasPrefix(s, "~/"):
		home, _ := os.UserHomeDir()
		return client.New(filepath.Join(home, s[2:])), nil
	case pathLike(s):
		return client.New(s), nil
	}
	if strings.Contains(s, "@") {
		return nil, usagef("-S %s: choosing an account (user@host) isn't supported", s)
	}
	h, ok, err := hosts.Find(s)
	if err != nil {
		return nil, err
	}
	if ok {
		if strings.Contains(h.Address, "://") {
			return client.NewURL(h.Address)
		}
		s = h.Address
	} else if !strings.ContainsAny(s, ".:") && !resolves(s) {
		return nil, usagef("-S %s: no host by that name (hive hosts ls); a socket path starts with / or ./", s)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return probe(ctx, s)
}

// ---- hosts ----

func cmdHosts(g globals, args []string) error {
	if len(args) == 0 {
		args = []string{"ls"}
	}
	switch args[0] {
	case "ls", "list":
		hs, err := hosts.List()
		if err != nil {
			return err
		}
		if g.json {
			if hs == nil {
				hs = []hosts.Host{}
			}
			return printJSON(os.Stdout, hs)
		}
		if len(hs) == 0 {
			fmt.Println("no hosts; add one with hive hosts add LABEL URL")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "LABEL\tADDRESS")
		for _, h := range hs {
			fmt.Fprintf(tw, "%s\t%s\n", h.Label, h.Address)
		}
		return tw.Flush()
	case "add":
		if len(args) != 3 {
			return usagef("usage: hive hosts add LABEL URL")
		}
		addr := args[2]
		if err := hosts.CheckLabel(args[1]); err != nil {
			return usagef("hosts add: %v", err)
		}
		if err := hosts.CheckAddress(addr); err != nil {
			return usagef("hosts add: %v", err)
		}
		// A bare host is saved as the URL it answers at, so later commands
		// don't probe again, and HTTP is chosen once, visibly.
		if !strings.Contains(addr, "://") {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			c, err := probe(ctx, addr)
			cancel()
			if err == nil {
				addr = c.URL
			} else {
				fmt.Fprintf(os.Stderr, "%s doesn't answer now; saved as given, tried each time\n", addr)
			}
		}
		if err := hosts.Add(hosts.Host{Label: args[1], Address: addr}); err != nil {
			return usagef("hosts add: %v", err)
		}
		fmt.Printf("added %s (%s): hive -S %s ls\n", args[1], addr, args[1])
		return nil
	case "rm", "remove":
		if len(args) != 2 {
			return usagef("usage: hive hosts rm LABEL")
		}
		ok, err := hosts.Remove(args[1])
		if err != nil {
			return err
		}
		if !ok {
			return usagef("no host %q", args[1])
		}
		return nil
	}
	return usagef("usage: hive hosts [ls] | add LABEL URL | rm LABEL")
}

// ---- whoami, clients ----

func cmdWhoami(g globals) error {
	x, err := open(g)
	if err != nil {
		return err
	}
	var id struct {
		Principal string `json:"principal"`
		Via       string `json:"via"`
		Node      string `json:"node"`
	}
	raw, err := x.c.CallRaw(x.ctx, "whoami", nil)
	if err != nil {
		return err
	}
	if g.json {
		return printRaw(raw)
	}
	_ = json.Unmarshal(raw, &id)
	from := ""
	if id.Node != "" {
		from = " from " + id.Node
	}
	fmt.Printf("%s via %s%s, on %s\n", id.Principal, id.Via, from, x.c)
	return nil
}

func cmdClient(g globals, args []string) error {
	if len(args) == 0 || (args[0] != "ls" && args[0] != "list") {
		return usagef("usage: hive client ls")
	}
	x, err := open(g)
	if err != nil {
		return err
	}
	raw, err := x.c.CallRaw(x.ctx, "client.list", nil)
	if err != nil {
		return err
	}
	if g.json {
		return printRaw(raw)
	}
	var out struct {
		Clients []struct {
			ID          string    `json:"client_id"`
			Name        string    `json:"name"`
			Kind        string    `json:"kind"`
			Principal   string    `json:"principal"`
			ConnectedAt time.Time `json:"connected_at"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tKIND\tNAME\tPRINCIPAL\tCONNECTED")
	for _, c := range out.Clients {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", short(c.ID), c.Kind, c.Name, c.Principal, age(c.ConnectedAt))
	}
	return tw.Flush()
}

// resolves reports whether a dotless name is a host, as a MagicDNS short
// name (box, for box.example.ts.net) is.
func resolves(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addrs, err := lookupHost(ctx, name)
	return err == nil && len(addrs) > 0
}
