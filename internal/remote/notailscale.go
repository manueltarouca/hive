//go:build notsnet

package remote

import (
	"context"
	"errors"
)

// Built reports whether this binary has Tailscale (this one doesn't).
const Built = false

var errNoTailscale = errors.New("this hive was built without Tailscale (-tags notsnet)")

// Open starts the listeners for an --extra-listen spec: only plain TCP here.
func Open(_ context.Context, s Spec, pol Policy) ([]*Listener, error) {
	if s.Kind == Plain {
		return openPlain(s, pol)
	}
	return nil, errNoTailscale
}

type NodeOptions struct {
	Hostname, Dir, AuthKey, ControlURL string
	Tags                               []string
	Port                               int
	Logf                               func(format string, args ...any)
}

type NodeStatus struct {
	State   string   `json:"state"`
	AuthURL string   `json:"auth_url,omitempty"`
	DNSName string   `json:"dns_name,omitempty"`
	URLs    []string `json:"urls,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type Node struct{}

func StartNode(NodeOptions, Policy, func(*Listener), func(NodeStatus)) (*Node, error) {
	return nil, errNoTailscale
}

func (*Node) Close() error { return nil }
