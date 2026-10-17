package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Set changes one setting in the file at path, keeping every other byte:
// the key's last active line, else its commented default, else a new line.
// The value is checked first, so the file always parses. Writes go to a
// symlink's target (stow-managed dotfiles keep their link) and are atomic.
// A missing file is created from the template, never over an existing one.
func Set(path, name, value string) error {
	// Edits from one process don't race each other (the server's config.set).
	writeMu.Lock()
	defer writeMu.Unlock()
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s: a value can't hold a line break", name)
	}
	k := key(name)
	if k == nil {
		return fmt.Errorf("unknown setting %q", name)
	}
	var probe Config
	if value != "" {
		if err := k.set(&probe, value); err != nil {
			return fmt.Errorf("%s %v", name, err)
		}
	}
	target, err := resolve(path)
	if err != nil {
		return err
	}
	// Read right before editing: a person may have saved since we last
	// looked, and their edit must survive ours.
	b, err := os.ReadFile(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return create(target, setIn(Template(), name, value))
	case err != nil:
		return err
	}
	fi, err := os.Stat(target)
	if err != nil {
		return err
	}
	return replace(target, setIn(string(b), name, value), fi.Mode().Perm())
}

var writeMu sync.Mutex

// Create makes the file from the template if it doesn't exist (through a
// symlink, like Set), and leaves an existing one alone.
func Create(path string) error {
	target, err := resolve(path)
	if err != nil {
		return err
	}
	if err := create(target, Template()); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return nil
}

// resolve follows symlinks to the file to write, as the kernel would:
// a relative link from its link's real folder, and chains of links to their
// end, even when that end doesn't exist yet. A missing folder is created
// only one level deep (~/.config/hive when ~/.config exists), and only for
// the path given, never at a link's far end.
func resolve(path string) (string, error) {
	if t, err := filepath.EvalSymlinks(path); err == nil {
		return t, nil
	}
	p := path
	for hops := 0; ; hops++ {
		fi, err := os.Lstat(p)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			break
		}
		if hops == 40 {
			return "", fmt.Errorf("%s: too many links", path)
		}
		dest, err := os.Readlink(p)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(dest) {
			dir := filepath.Dir(p)
			if d, err := filepath.EvalSymlinks(dir); err == nil {
				dir = d
			}
			dest = filepath.Join(dir, dest)
		}
		p = dest
	}
	dir := filepath.Dir(p)
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(d, filepath.Base(p)), nil
	}
	if p != path {
		return "", fmt.Errorf("%s links to %s, whose folder doesn't exist", path, p)
	}
	if err := os.Mkdir(dir, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("can't create %s: %w", dir, err)
	}
	return path, nil
}

// create writes a new file, failing if one appeared meanwhile.
func create(path, text string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// replace writes text atomically: a temp file beside it, then a rename.
func replace(path, text string, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(text); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func validValue(name, v string) bool {
	k := key(name)
	var c Config
	return k != nil && (v == "" || k.set(&c, v) == nil)
}

// setIn returns text with name set to value, every other byte kept.
func setIn(text, name, value string) string {
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(text, "\n")
	want := name + " = " + value
	active, commented := -1, -1
	for i, raw := range lines {
		s := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if k, _, hasEq, skip := line(raw); !skip && hasEq && k == name {
			active = i
		} else if commented < 0 && strings.HasPrefix(s, "#") {
			// A commented setting, not prose that starts like one
			// ("# font-size = 13 was too small for me").
			if k, v, hasEq, skip := line(strings.TrimSpace(s[1:])); !skip && hasEq && k == name && validValue(name, v) {
				commented = i
			}
		}
	}
	at := active
	if at < 0 {
		at = commented
	}
	if at >= 0 {
		cr := ""
		if strings.HasSuffix(lines[at], "\r") {
			cr = "\r"
		}
		lines[at] = want + cr
		return strings.Join(lines, "\n")
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += nl
	}
	return text + want + nl
}
