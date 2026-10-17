// Package persist keeps hive's state on disk so that sessions survive the
// server stopping and the machine restarting: state.json for the structure,
// and one libghostty snapshot per block for its screen and scrollback.
// Every write goes to a temporary file that is renamed into place, so a crash
// leaves the previous version intact.
package persist

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manueltarouca/hive/internal/block"
	"github.com/manueltarouca/hive/internal/session"
	"github.com/manueltarouca/hive/internal/vt"
)

// Version of the state file format.
const Version = 1

// State is everything hive needs to rebuild its sessions.
type State struct {
	Version  int                    `json:"version"`
	SavedAt  time.Time              `json:"saved_at"`
	Sessions []*session.Session     `json:"sessions"`
	Blocks   map[string]*BlockState `json:"blocks"`
}

// BlockState is what hive remembers about a block.
type BlockState struct {
	SessionID  string                      `json:"session_id"`
	Label      string                      `json:"label,omitempty"`
	Command    []string                    `json:"command,omitempty"`
	Cwd        string                      `json:"cwd,omitempty"`
	KeepOpen   bool                        `json:"keep_open,omitempty"`
	Cols       uint16                      `json:"cols"`
	Rows       uint16                      `json:"rows"`
	Title      string                      `json:"title,omitempty"`
	Pwd        string                      `json:"pwd,omitempty"`
	Foreground *block.Process              `json:"foreground,omitempty"`
	Agent      *Agent                      `json:"agent,omitempty"`
	Status     map[string]vt.ProgramStatus `json:"program_status,omitempty"`
	Exited     bool                        `json:"exited,omitempty"`
	ExitCode   int                         `json:"exit_code,omitempty"`
	Created    time.Time                   `json:"created_at"`
}

// Agent is a coding agent known to run in a block.
type Agent struct {
	Harness        string    `json:"harness"`
	SessionID      string    `json:"session_id,omitempty"`
	Cwd            string    `json:"cwd,omitempty"`
	TranscriptPath string    `json:"transcript_path,omitempty"`
	State          string    `json:"state,omitempty"` // idle, working, blocked, done, ended
	Message        string    `json:"message,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
	// EndedBy "shell": ended because the pane's shell came back in front,
	// not by the agent's word. It comes back, in its Before state, if the
	// same agent is in front again (a tool's shell had the terminal).
	EndedBy string `json:"ended_by,omitempty"`
	Before  string `json:"before,omitempty"`
}

// Store reads and writes a state directory.
type Store struct{ dir string }

// New uses dir for state.
func New(dir string) *Store { return &Store{dir: dir} }

// Dir returns the state directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) statePath() string { return filepath.Join(s.dir, "state.json") }

func (s *Store) snapPath(blockID string) string {
	return filepath.Join(s.dir, "blocks", strings.TrimPrefix(blockID, "block:")+".snap")
}

// LoadState reads the state, or returns nil if there is none.
func (s *Store) LoadState() (*State, error) {
	data, err := os.ReadFile(s.statePath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.Blocks == nil {
		st.Blocks = map[string]*BlockState{}
	}
	return &st, nil
}

// SaveState writes the state.
func (s *Store) SaveState(st *State) error {
	st.Version, st.SavedAt = Version, time.Now().UTC()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return s.atomic(s.statePath(), func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// SaveSnapshot writes a block's snapshot with write, buffered and without
// forcing it to the disk: a snapshot only carries scrollback, and one that
// a power cut leaves unreadable costs that scrollback, never the pane (see
// restore). Forcing every snapshot out (F_FULLFSYNC, ~4 ms each on a Mac
// SSD) made each save cost ~5 ms per busy pane.
func (s *Store) SaveSnapshot(blockID string, write func(io.Writer) error) error {
	return s.atomicWith(s.snapPath(blockID), func(w io.Writer) error {
		bw := bufio.NewWriterSize(w, 256<<10)
		if err := write(bw); err != nil {
			return err
		}
		return bw.Flush()
	}, false)
}

// LoadSnapshot reads a block's snapshot.
func (s *Store) LoadSnapshot(blockID string) ([]byte, error) {
	return os.ReadFile(s.snapPath(blockID))
}

// RemoveSnapshot deletes a block's snapshot.
func (s *Store) RemoveSnapshot(blockID string) {
	_ = os.Remove(s.snapPath(blockID))
}

// GC removes snapshots of blocks not in live, and leftover temporary files.
func (s *Store) GC(live map[string]bool) {
	entries, _ := os.ReadDir(filepath.Join(s.dir, "blocks"))
	for _, e := range entries {
		name := e.Name()
		id := "block:" + strings.TrimSuffix(name, ".snap")
		if strings.HasSuffix(name, ".tmp") || (strings.HasSuffix(name, ".snap") && !live[id]) {
			_ = os.Remove(filepath.Join(s.dir, "blocks", name))
		}
	}
}

func (s *Store) atomic(path string, write func(io.Writer) error) error {
	return s.atomicWith(path, write, true)
}

// atomicWith writes path through a temp file and a rename; durable forces
// the data to the disk before the rename.
func (s *Store) atomicWith(path string, write func(io.Writer) error, durable bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err := write(f); err != nil {
		return err
	}
	if durable {
		if err := syncFile(f); err != nil {
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}
