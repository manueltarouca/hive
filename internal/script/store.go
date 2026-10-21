package script

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/manueltarouca/hive/internal/api"
)

// storeCap is how much one script may keep.
const storeCap = 1 << 20

// store is a script's key/value storage, a JSON file in the state folder.
// It survives reloads and restarts.
type store struct {
	mu   sync.Mutex
	path string
	data map[string]json.RawMessage
	size int
}

func openStore(dir, name string) *store {
	s := &store{path: filepath.Join(dir, "scripts", name+".store.json"), data: map[string]json.RawMessage{}}
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, &s.data)
		s.size = len(b)
	}
	return s
}

func (s *store) get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.data[key])
}

func (s *store) set(key, valueJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, had := s.data[key]
	s.data[key] = json.RawMessage(valueJSON)
	if err := s.save(); err != nil {
		if had {
			s.data[key] = old
		} else {
			delete(s.data, key)
		}
		return err
	}
	return nil
}

func (s *store) delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return nil
	}
	old := s.data[key]
	delete(s.data, key)
	if err := s.save(); err != nil {
		s.data[key] = old
		return err
	}
	return nil
}

func (s *store) keys(prefix string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for k := range s.data {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// save writes the file whole, through a temporary file, so a crash never
// leaves half of it.
func (s *store) save() error {
	b, err := json.Marshal(s.data)
	if err != nil {
		return api.BadRequest("store: %v", err)
	}
	if len(b) > storeCap {
		return api.BadRequest("store: over %d bytes", storeCap)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return api.Internal("store: %v", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return api.Internal("store: %v", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return api.Internal("store: %v", err)
	}
	s.size = len(b)
	return nil
}
