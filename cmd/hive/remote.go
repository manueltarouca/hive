package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/manueltarouca/hive/internal/remote"
	"github.com/manueltarouca/hive/internal/server"
)

// listFlag is a flag that can be given more than once.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

// remoteFlags are the server's listener and Tailscale options, shared by
// `server run` and `server start` (which passes them on).
type remoteFlags struct {
	fs              *flag.FlagSet
	listen          listFlag
	allowAnonymous  *bool
	allowIdentified *bool
	tailscale       *bool
	tsHostname      *string
	tsAuthKeyFile   *string
	tsTags          listFlag
	tsAllowMembers  *bool
	tsControlURL    *string
}

func addRemoteFlags(fs *flag.FlagSet) *remoteFlags {
	r := &remoteFlags{fs: fs}
	fs.Var(&r.listen, "extra-listen", "")
	r.allowAnonymous = fs.Bool("allow-anonymous", false, "")
	r.allowIdentified = fs.Bool("tailnet-allow-identified", false, "")
	r.tailscale = fs.Bool("tailscale", false, "")
	r.tsHostname = fs.String("tailscale-hostname", "", "")
	r.tsAuthKeyFile = fs.String("tailscale-auth-key-file", "", "")
	fs.Var(&r.tsTags, "tailscale-advertise-tag", "")
	r.tsAllowMembers = fs.Bool("tailscale-allow-tailnet-members", false, "")
	r.tsControlURL = fs.String("tailscale-control-url", "", "")
	return r
}

// given reports whether any remote option was set.
func (r *remoteFlags) given() bool {
	n := 0
	r.fs.Visit(func(f *flag.Flag) {
		if f.Name != "fresh" && f.Name != "socket" && f.Name != "state-dir" {
			n++
		}
	})
	return n > 0
}

// config builds the server's RemoteConfig. stdin is read for an auth key
// file of "-".
func (r *remoteFlags) config(stdin io.Reader) (*server.RemoteConfig, error) {
	if !r.given() {
		return nil, nil
	}
	rc := &server.RemoteConfig{Policy: remote.Policy{AllowAnonymous: *r.allowAnonymous,
		AllowIdentified: *r.allowIdentified, AllowMembers: *r.tsAllowMembers}}
	for _, l := range r.listen {
		s, err := remote.ParseSpec(l)
		if err != nil {
			return nil, usagef("--extra-listen %v", err)
		}
		rc.Listen = append(rc.Listen, s)
	}
	tsOnly := *r.tsHostname != "" || *r.tsAuthKeyFile != "" || len(r.tsTags) > 0 || *r.tsAllowMembers || *r.tsControlURL != ""
	if tsOnly && !*r.tailscale {
		return nil, usagef("the --tailscale-* options need --tailscale")
	}
	if len(r.tsTags) > 0 && !*r.tsAllowMembers {
		// A tagged device has no owner: nobody could connect.
		return nil, usagef("--tailscale-advertise-tag needs --tailscale-allow-tailnet-members: a tagged device has no owner to let in")
	}
	for _, t := range r.tsTags {
		if !strings.HasPrefix(t, "tag:") {
			return nil, usagef("--tailscale-advertise-tag %s: tags look like tag:NAME", t)
		}
	}
	if *r.tailscale {
		if !remote.Built {
			return nil, errors.New("this hive was built without Tailscale")
		}
		key, err := readAuthKey(*r.tsAuthKeyFile, stdin)
		if err != nil {
			return nil, err
		}
		rc.Node = &remote.NodeOptions{Hostname: *r.tsHostname, AuthKey: key, Tags: r.tsTags, ControlURL: *r.tsControlURL}
	}
	return rc, nil
}

func readAuthKey(path string, stdin io.Reader) (string, error) {
	if path == "" {
		return os.Getenv("TS_AUTHKEY"), nil // passed on by `server start`
	}
	var b []byte
	var err error
	if path == "-" {
		b, err = io.ReadAll(io.LimitReader(stdin, 4096))
	} else {
		b, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("--tailscale-auth-key-file: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// daemonArgs are the remote options for the server `start` spawns. The
// auth key goes in its environment, never on its command line, where any
// account could read it.
func (r *remoteFlags) daemonArgs(stdin io.Reader) (args, env []string, err error) {
	r.fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "fresh", "socket", "state-dir":
			return
		case "extra-listen", "tailscale-advertise-tag":
			for _, v := range *f.Value.(*listFlag) {
				args = append(args, "--"+f.Name, v)
			}
		case "tailscale-auth-key-file":
			// Read here: the server runs in /, where a relative path means
			// something else, and standard input isn't passed on.
			key, kerr := readAuthKey(f.Value.String(), stdin)
			if kerr != nil {
				err = kerr
				return
			}
			env = append(env, "TS_AUTHKEY="+key)
		default:
			args = append(args, "--"+f.Name+"="+f.Value.String())
		}
	})
	return args, env, err
}
