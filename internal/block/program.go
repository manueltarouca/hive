package block

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Interpreters run another program: the script they run names it.
var interpreters = map[string]string{
	"node": "node", "bun": "bun", "deno": "deno", "python": "python", "ruby": "ruby", "perl": "perl",
}

// Package paths of agent CLIs installed with npm and run by an interpreter.
var packageMarkers = []struct{ marker, program string }{
	{"/@anthropic-ai/claude-code/", "claude"},
	{"/@google/gemini-cli/", "gemini"},
	{"/@openai/codex/", "codex"},
	{"/opencode-ai/", "opencode"},
}

var versionName = regexp.MustCompile(`^\d+\.\d+\.\d+`)

// Agents whose release binaries carry their version and platform:
// grok-1.0.62-macos-aarch64.
var versionedAgent = regexp.MustCompile(`^(claude|codex|gemini|grok|opencode)-\d`)

// programOf names the program a process runs, the way a person would. name
// is the kernel's short name, exe the executable's path and argv the
// arguments (either may be missing). An installed Claude Code's executable
// is its version (~/.local/share/claude/versions/2.1.302), and npm tools
// run as node, so neither name alone is enough.
func programOf(name, exe string, argv []string) string {
	// A versioned executable: name it by the folder that holds the versions.
	if versionName.MatchString(filepath.Base(exe)) {
		if dir := filepath.Dir(exe); filepath.Base(dir) == "versions" {
			return strings.ToLower(filepath.Base(filepath.Dir(dir)))
		}
	}
	if versionName.MatchString(name) && len(argv) > 0 {
		name = filepath.Base(argv[0])
	}
	if m := versionedAgent.FindStringSubmatch(strings.ToLower(name)); m != nil {
		return m[1]
	}
	base := normalize(name)
	if exeBase := normalize(filepath.Base(exe)); base == "" || interpreters[exeBase] != "" {
		if exeBase != "" && exeBase != "." {
			base = exeBase
		}
	}
	if len(argv) > 0 && interpreters[normalize(filepath.Base(argv[0]))] != "" {
		base = normalize(filepath.Base(argv[0]))
	}
	if shells[base] {
		if run := shellRuns(argv); run != "" {
			return programOf(run, "", nil)
		}
		return base
	}
	if interp := interpreters[base]; interp != "" {
		// The first argument that isn't a flag is the script.
		for _, a := range argv[min(1, len(argv)):] {
			if strings.HasPrefix(a, "-") {
				if a == "-m" || a == "-c" || a == "-e" {
					break
				}
				continue
			}
			for _, m := range packageMarkers {
				if strings.Contains(a, m.marker) {
					return m.program
				}
			}
			switch s := strings.ToLower(filepath.Base(a)); s {
			case "claude", "gemini", "codex", "opencode", "grok":
				return s
			}
			break
		}
		return interp
	}
	return base
}

// shellRuns names what a non-interactive shell runs: the script it was
// given, or the first command of its -c string. "" for an interactive shell.
func shellRuns(argv []string) string {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		if a == "-c" {
			if i+1 >= len(argv) {
				return ""
			}
			for _, w := range strings.Fields(argv[i+1]) {
				if w == "exec" || strings.Contains(w, "=") {
					continue // the command after env assignments and exec
				}
				return filepath.Base(w)
			}
			return ""
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		return strings.TrimSuffix(filepath.Base(a), filepath.Ext(a))
	}
	return ""
}

// normalize lowercases a name and folds versioned interpreters and login
// shells: python3.12 is python, -zsh is zsh.
func normalize(name string) string {
	n := strings.ToLower(strings.TrimPrefix(name, "-"))
	for _, p := range []string{"python", "ruby", "perl", "node"} {
		if strings.HasPrefix(n, p) && strings.Trim(n[len(p):], "0123456789.") == "" {
			return p
		}
	}
	return n
}

var shells = map[string]bool{"zsh": true, "bash": true, "sh": true, "fish": true, "dash": true, "ksh": true, "tcsh": true, "nu": true}

// AtShell reports whether the process is a shell: what a terminal returns
// to when the program it ran has exited.
func (p Process) AtShell() bool {
	n := p.Program
	if n == "" {
		n = normalize(p.Name) // saved by a server that didn't name programs
	}
	return shells[n]
}
