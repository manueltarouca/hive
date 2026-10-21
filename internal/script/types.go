package script

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

//go:embed hive.d.ts
var typesHead string

// MethodSchema is one method as api.list --full (or api.describe) gives it.
type MethodSchema struct {
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Permission   string         `json:"permission"`
	PermNote     string         `json:"permission_note"`
	InputSchema  map[string]any `json:"input_schema"`
	OutputSchema map[string]any `json:"output_schema"`
}

// Types writes hive.d.ts for a server's methods: the hand-written API (h,
// handles, events, manifests) and, generated, every method's arguments and
// result, as h.call and as namespaces (h.session.new_split).
func Types(version string, methods []MethodSchema) string {
	sort.Slice(methods, func(i, j int) bool { return methods[i].Name < methods[j].Name })
	var b strings.Builder
	fmt.Fprintf(&b, "// hive.d.ts for hive %s. Written by `hive script types`; don't edit.\n\n", version)
	b.WriteString(typesHead)
	b.WriteString("\ndeclare global {\n  namespace hive {\n    interface Methods {\n")
	for _, m := range methods {
		perm := m.Permission
		if m.PermNote != "" {
			perm += "; " + m.PermNote
		}
		fmt.Fprintf(&b, "      /** %s Permission: %s. */\n", doc(m.Description), doc(perm))
		fmt.Fprintf(&b, "      %q: { args: %s; result: %s };\n", m.Name, tsType(m.InputSchema, 4), tsType(m.OutputSchema, 4))
	}
	b.WriteString("    }\n\n    interface Generated {\n")
	writeNamespace(&b, tree(methods), 3)
	b.WriteString("    }\n  }\n}\n")
	return b.String()
}

type node struct {
	method   *MethodSchema
	children map[string]*node
}

// tree groups methods as the prelude does: hive.terminal.x is terminal.x.
func tree(methods []MethodSchema) *node {
	root := &node{children: map[string]*node{}}
	for i := range methods {
		m := &methods[i]
		parts := strings.Split(m.Name, ".")
		if strings.HasPrefix(m.Name, "hive.terminal.") {
			parts = []string{"terminal", strings.TrimPrefix(m.Name, "hive.terminal.")}
		}
		n := root
		for _, p := range parts {
			if n.children[p] == nil {
				n.children[p] = &node{children: map[string]*node{}}
			}
			n = n.children[p]
		}
		n.method = m
	}
	return root
}

// handles are members of h that the prelude defines by hand; generated
// namespaces with these names merge into them as properties.
var handles = []string{"block", "session"}

func writeNamespace(b *strings.Builder, n *node, depth int) {
	pad := strings.Repeat("  ", depth)
	keys := make([]string, 0, len(n.children))
	for k := range n.children {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := n.children[k]
		if c.method != nil {
			m := c.method
			opt := "?"
			if req, _ := m.InputSchema["required"].([]any); len(req) > 0 {
				opt = ""
			}
			fmt.Fprintf(b, "%s/** %s */\n", pad, doc(m.Description))
			fmt.Fprintf(b, "%s%s(args%s: Methods[%q][\"args\"]): Promise<Methods[%q][\"result\"]>;\n", pad, ident(k), opt, m.Name, m.Name)
			continue
		}
		fmt.Fprintf(b, "%s%s: {\n", pad, ident(k))
		writeNamespace(b, c, depth+1)
		fmt.Fprintf(b, "%s};\n", pad)
	}
}

func ident(k string) string {
	for _, r := range k {
		if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return fmt.Sprintf("%q", k)
		}
	}
	return k
}

func doc(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "*/", "* /")
}

// tsType turns a JSON Schema from the registry into a TypeScript type.
func tsType(s map[string]any, depth int) string {
	if s == nil {
		return "unknown"
	}
	if enum, ok := s["enum"].([]any); ok && len(enum) > 0 {
		var parts []string
		for _, e := range enum {
			b, _ := json.Marshal(e)
			parts = append(parts, string(b))
		}
		return strings.Join(parts, " | ")
	}
	switch s["type"] {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		items, _ := s["items"].(map[string]any)
		t := tsType(items, depth)
		if strings.ContainsAny(t, "|&") {
			t = "(" + t + ")"
		}
		return t + "[]"
	case "object":
		props, _ := s["properties"].(map[string]any)
		if props == nil {
			if ap, ok := s["additionalProperties"].(map[string]any); ok {
				return "Record<string, " + tsType(ap, depth) + ">"
			}
			return "Record<string, unknown>"
		}
		var req []string
		for _, r := range asList(s["required"]) {
			req = append(req, r)
		}
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			return "Record<string, never>"
		}
		pad := strings.Repeat("  ", depth+1)
		var b strings.Builder
		b.WriteString("{\n")
		for _, k := range keys {
			p, _ := props[k].(map[string]any)
			opt := "?"
			if slices.Contains(req, k) {
				opt = ""
			}
			if d, _ := p["description"].(string); d != "" {
				fmt.Fprintf(&b, "%s/** %s */\n", pad, doc(d))
			}
			fmt.Fprintf(&b, "%s%s%s: %s;\n", pad, ident(k), opt, tsType(p, depth+1))
		}
		b.WriteString(strings.Repeat("  ", depth) + "}")
		return b.String()
	}
	return "unknown"
}

func asList(v any) []string {
	var out []string
	switch l := v.(type) {
	case []any:
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		out = l
	}
	return out
}
