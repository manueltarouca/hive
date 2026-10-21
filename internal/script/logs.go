package script

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// logCapFile is when a resident script's log file starts over (the old one
// is kept as NAME.log.1).
const logCapFile = 1 << 20

var logMu sync.Mutex

// LogLine is one line of a script's log file.
type LogLine struct {
	Time  time.Time `json:"t"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

func (h *Host) logPath(name string) string {
	return filepath.Join(h.cfg.StateDir, "scripts", name+".log")
}

// appendLog keeps a resident script's log on disk, for hive script logs.
func (h *Host) appendLog(name, level, text string) {
	if h.cfg.StateDir == "" {
		return
	}
	line, _ := json.Marshal(LogLine{Time: time.Now().UTC(), Level: level, Text: text})
	logMu.Lock()
	defer logMu.Unlock()
	p := h.logPath(name)
	if st, err := os.Stat(p); err == nil && st.Size() > logCapFile {
		_ = os.Rename(p, p+".1")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// Logs returns the last n lines of a resident script's log.
func (h *Host) Logs(name string, n int) []LogLine {
	out := []LogLine{}
	if !nameRE.MatchString(name) {
		return out
	}
	logMu.Lock()
	defer logMu.Unlock()
	for _, p := range []string{h.logPath(name) + ".1", h.logPath(name)} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var l LogLine
			if json.Unmarshal(sc.Bytes(), &l) == nil {
				out = append(out, l)
			}
		}
		f.Close()
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// Audit returns the audit log's last n entries, optionally for one script.
func (h *Host) Audit(name string, n int) []Entry {
	out := []Entry{}
	if h.audit == nil {
		return out
	}
	h.audit.mu.Lock()
	defer h.audit.mu.Unlock()
	for _, p := range []string{h.audit.path + ".1", h.audit.path} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 8<<20)
		for sc.Scan() {
			var e Entry
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			if name != "" && strings.TrimPrefix(e.Principal, "script:") != name {
				continue
			}
			out = append(out, e)
			if n > 0 && len(out) > 2*n {
				out = out[len(out)-n:]
			}
		}
		f.Close()
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}
