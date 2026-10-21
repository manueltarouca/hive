package script

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// audited are the permissions whose calls go to the audit log: everything
// that changes something.
var audited = map[string]bool{"control": true, "spawn": true, "input": true, "config": true, "server": true}

// Entry is one line of the audit log.
type Entry struct {
	Time       time.Time      `json:"t"`
	Principal  string         `json:"principal"`
	OnBehalfOf string         `json:"on_behalf_of,omitempty"`
	RunID      string         `json:"run_id,omitempty"`
	Method     string         `json:"method"`
	Permission string         `json:"permission"`
	Args       map[string]any `json:"args,omitempty"`
	Result     string         `json:"result"`
}

// auditLog appends entries to scripts/audit.jsonl in the state folder.
type auditLog struct {
	mu   sync.Mutex
	path string
}

func (a *auditLog) write(e Entry) {
	if a == nil || a.path == "" {
		return
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// auditArgs is what the log keeps of a call's arguments: text typed into a
// terminal only as its length and a hash.
func auditArgs(raw []byte) map[string]any {
	var args map[string]any
	if json.Unmarshal(raw, &args) != nil {
		return nil
	}
	if inner, ok := args["args"].(map[string]any); ok {
		if d, ok := inner["data"].(string); ok {
			sum := sha256.Sum256([]byte(d))
			inner["data"] = map[string]any{"length": len(d), "sha256": hex.EncodeToString(sum[:8])}
		}
		if d, ok := inner["data_base64"].(string); ok {
			inner["data_base64"] = map[string]any{"length": len(d)}
		}
	}
	return args
}
