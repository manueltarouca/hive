// Package script runs TypeScript and JavaScript scripts inside the hive
// server. A script calls the same methods as the CLI and the apps, with the
// permissions it was granted, and hears the server's events.
//
// Scripts are bundled with esbuild (types stripped, relative imports
// inlined) and run on goja, one runtime per script, each owned by one
// goroutine.
package script

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// Bundle turns a script into one JavaScript program for the engine. Give
// either a file, or code and the folder its relative imports resolve from.
//
// The result is an async function expression taking no arguments: it runs
// the script's top level (top-level await works) and resolves to the
// script's exports, so the runtime can find a default export.
func Bundle(file, code, dir string) (string, error) {
	opts := api.BuildOptions{
		Bundle:   true,
		Write:    false,
		Format:   api.FormatESModule,
		Platform: api.PlatformNeutral,
		Target:   api.ES2022,
		// goja runs async functions and generators, not async generators.
		Supported:     map[string]bool{"async-generator": false},
		Sourcemap:     api.SourceMapNone,
		LogLevel:      api.LogLevelSilent,
		Plugins:       []api.Plugin{relativeOnly},
		AbsWorkingDir: dir,
	}
	if file != "" {
		abs, err := filepath.Abs(file)
		if err != nil {
			return "", err
		}
		opts.EntryPoints = []string{abs}
		opts.AbsWorkingDir = filepath.Dir(abs)
	} else {
		if dir == "" {
			return "", fmt.Errorf("bundle: code needs a folder for its imports")
		}
		opts.Stdin = &api.StdinOptions{Contents: code, ResolveDir: dir, Sourcefile: "script.ts", Loader: api.LoaderTS}
	}
	r := api.Build(opts)
	if len(r.Errors) > 0 {
		return "", buildError(r.Errors)
	}
	if len(r.OutputFiles) != 1 {
		return "", fmt.Errorf("bundle: %d outputs, want 1", len(r.OutputFiles))
	}
	return wrap(string(r.OutputFiles[0].Contents))
}

// exportTail is the one export statement esbuild ends a bundled ES module
// with: export { a as default, b };
var exportTail = regexp.MustCompile(`(?s)\nexport\s*\{([^}]*)\};?\s*$`)

// wrap makes a bundled ES module into an async function returning its
// exports. Everything else was inlined, so only the export list is left.
func wrap(js string) (string, error) {
	var ret strings.Builder
	ret.WriteString("return {")
	if m := exportTail.FindStringSubmatchIndex(js); m != nil {
		for _, part := range strings.Split(js[m[2]:m[3]], ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			local, exported, ok := strings.Cut(part, " as ")
			if !ok {
				exported = local
			}
			fmt.Fprintf(&ret, "%q: %s,", strings.TrimSpace(exported), strings.TrimSpace(local))
		}
		js = js[:m[0]] + "\n"
	}
	ret.WriteString("};")
	if regexp.MustCompile(`(?m)^export\b`).MatchString(js) {
		return "", fmt.Errorf("bundle: an export esbuild left in place; please report it")
	}
	return "(async () => {\n" + js + ret.String() + "\n})", nil
}

func buildError(msgs []api.Message) error {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteString("\n")
		}
		if m.Location != nil {
			fmt.Fprintf(&b, "%s:%d:%d: ", m.Location.File, m.Location.Line, m.Location.Column)
		}
		b.WriteString(m.Text)
	}
	return fmt.Errorf("%s", b.String())
}

// relativeOnly refuses package imports: a script may import its own files,
// not code from node_modules (decision 11: relative imports only, for now).
var relativeOnly = api.Plugin{
	Name: "hive-relative-only",
	Setup: func(b api.PluginBuild) {
		b.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
			if a.Kind == api.ResolveEntryPoint || strings.HasPrefix(a.Path, "./") || strings.HasPrefix(a.Path, "../") {
				return api.OnResolveResult{}, nil // esbuild resolves it
			}
			return api.OnResolveResult{Errors: []api.Message{{
				Text: fmt.Sprintf("import %q: scripts can import their own files (./name), not packages", a.Path),
			}}}, nil
		})
	},
}
