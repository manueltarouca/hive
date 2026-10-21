package script

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Permissions a script can hold. The method registry says which one each
// method needs (api.Perm); the rest guard script-only abilities.
var Permissions = []string{"read", "screen", "control", "spawn", "input", "ui", "config", "exec", "network", "fs", "hosts", "server"}

// Perms is a set of permissions.
type Perms map[string]bool

// ParsePerms reads a list such as ["read", "control"] or "read,control".
func ParsePerms(list ...string) (Perms, error) {
	p := Perms{}
	for _, item := range list {
		for _, name := range strings.Split(item, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if !slices.Contains(Permissions, name) {
				return nil, fmt.Errorf("unknown permission %q (one of %s)", name, strings.Join(Permissions, ", "))
			}
			p[name] = true
		}
	}
	return p, nil
}

// Has reports whether the set allows a method needing need. Every script
// may read, and running a program (spawn) includes arranging the default
// shell (control).
func (p Perms) Has(need string) bool {
	return need == "read" || p[need] || need == "control" && p["spawn"]
}

// Intersect is what both sets allow. A nil set allows everything.
func (p Perms) Intersect(q Perms) Perms {
	if q == nil {
		return p
	}
	if p == nil {
		return q
	}
	out := Perms{}
	for name := range p {
		if q.Has(name) {
			out[name] = true
		}
	}
	return out
}

// List is the set, sorted.
func (p Perms) List() []string {
	out := make([]string, 0, len(p))
	for name := range p {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

type permsKey struct{}

// WithPerms marks ctx as a script's call, holding perms: a script that
// starts another can't give it more than it has.
func WithPerms(ctx context.Context, p Perms) context.Context {
	return context.WithValue(ctx, permsKey{}, p)
}

// PermsFrom is the permissions of the script making a call, or nil for a
// call that isn't a script's (the CLI or an app: everything).
func PermsFrom(ctx context.Context) Perms {
	p, _ := ctx.Value(permsKey{}).(Perms)
	return p
}

type callerKey struct{}

// Caller is the script making a call.
type Caller struct {
	Name  string
	RunID string
}

// WithCaller marks ctx as a call by a script.
func WithCaller(ctx context.Context, c Caller) context.Context {
	return context.WithValue(ctx, callerKey{}, c)
}

// CallerFrom is the script making a call, if one is.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}
