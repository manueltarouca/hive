// Package examples holds the starter scripts hive ships: hive script add
// copies one into the scripts folder.
package examples

import (
	"embed"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

//go:embed scripts/*.ts
var files embed.FS

// Starter is one starter script.
type Starter struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Permissions string `json:"permissions"`
	Code        string `json:"-"`
}

var (
	descRE  = regexp.MustCompile(`(?m)^\s*description:\s*"([^"]*)"`)
	permsRE = regexp.MustCompile(`(?m)^\s*permissions:\s*\[([^\]]*)\]`)
)

// Starters lists them by name.
func Starters() []Starter {
	entries, _ := fs.Glob(files, "scripts/*.ts")
	var out []Starter
	for _, p := range entries {
		b, _ := files.ReadFile(p)
		code := string(b)
		s := Starter{Name: strings.TrimSuffix(strings.TrimPrefix(p, "scripts/"), ".ts"), Code: code}
		if m := descRE.FindStringSubmatch(code); m != nil {
			s.Description = m[1]
		}
		if m := permsRE.FindStringSubmatch(code); m != nil {
			s.Permissions = strings.NewReplacer(`"`, "", " ", "").Replace(m[1])
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get finds one by name.
func Get(name string) (Starter, bool) {
	for _, s := range Starters() {
		if s.Name == name {
			return s, true
		}
	}
	return Starter{}, false
}
