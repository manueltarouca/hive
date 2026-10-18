// Package hosts keeps the remote servers you've named: hosts.json, next to
// the settings file. The CLI and the app read the same file.
package hosts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/manueltarouca/hive/internal/client"
	"github.com/manueltarouca/hive/internal/config"
)

// Host is a named remote server. Address is a URL (https://box.ts.net) or
// a bare host, tried at https:443, then https and http on hive's port.
type Host struct {
	Label   string `json:"label"`
	Address string `json:"address"`
}

type file struct {
	Version int    `json:"version"`
	Hosts   []Host `json:"hosts"`
}

// Path is hosts.json, beside the settings file.
func Path() string { return filepath.Join(filepath.Dir(config.Path()), "hosts.json") }

// List returns the saved hosts; none when the file doesn't exist.
func List() ([]Host, error) {
	b, err := os.ReadFile(Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", Path(), err)
	}
	return f.Hosts, nil
}

// Find returns the host with this label (case-insensitive).
func Find(label string) (Host, bool, error) {
	hs, err := List()
	if err != nil {
		return Host{}, false, err
	}
	for _, h := range hs {
		if strings.EqualFold(h.Label, label) {
			return h, true, nil
		}
	}
	return Host{}, false, nil
}

// CheckLabel refuses labels that -S would read as something else.
func CheckLabel(l string) error {
	switch {
	case l == "":
		return errors.New("a host needs a label")
	case strings.ContainsAny(l, "/:@ \t") || strings.HasPrefix(l, "~") || strings.HasPrefix(l, "."):
		return fmt.Errorf("label %q: letters, digits, '-', '_' and '.' only, not starting with '.'", l)
	case strings.Contains(l, "."):
		return fmt.Errorf("label %q: a label with a dot reads as a host name; use something like %q", l, strings.ReplaceAll(l, ".", "-"))
	}
	return nil
}

// CheckAddress refuses an address that can't name a server.
func CheckAddress(a string) error {
	if strings.Contains(a, "://") {
		_, err := client.NewURL(a)
		return err
	}
	if a == "" || strings.ContainsAny(a, "/@ \t") {
		return fmt.Errorf("%q: give a URL (https://box.example.ts.net) or a host name", a)
	}
	return nil
}

// lock serializes edits to hosts.json between processes (the CLI and the
// app), so one doesn't lose another's change.
func lock() (func(), error) {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

// Add saves a host, replacing one with the same label.
func Add(h Host) error {
	if err := CheckLabel(h.Label); err != nil {
		return err
	}
	if err := CheckAddress(h.Address); err != nil {
		return err
	}
	unlock, err := lock()
	if err != nil {
		return err
	}
	defer unlock()
	hs, err := List()
	if err != nil {
		return err
	}
	out := []Host{h}
	for _, x := range hs {
		if !strings.EqualFold(x.Label, h.Label) {
			out = append(out, x)
		}
	}
	return save(out)
}

// Remove forgets a host. It reports whether there was one.
func Remove(label string) (bool, error) {
	unlock, err := lock()
	if err != nil {
		return false, err
	}
	defer unlock()
	hs, err := List()
	if err != nil {
		return false, err
	}
	var out []Host
	found := false
	for _, x := range hs {
		if strings.EqualFold(x.Label, label) {
			found = true
			continue
		}
		out = append(out, x)
	}
	if !found {
		return false, nil
	}
	return true, save(out)
}

func save(hs []Host) error {
	if hs == nil {
		hs = []Host{}
	}
	b, err := json.MarshalIndent(file{Version: 1, Hosts: hs}, "", "  ")
	if err != nil {
		return err
	}
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".hosts-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}
