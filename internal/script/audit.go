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

// auditCap is when the audit log starts over (the previous one is kept as
// audit.jsonl.1).
const auditCap = 8 << 20

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
	// Seq numbers entries, newest highest.
	Seq uint64 `json:"seq,omitempty"`
	// Script is the folder script it belongs to: a one-shot a script
	// started counts as that script's.
	Script    string `json:"script,omitempty"`
	Summary   string `json:"summary,omitempty" desc:"What it did, in a few words."`
	BlockID   string `json:"block_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	// Preview is the start of typed text: kept in memory only, shown to
	// the owner's apps, never written to the file.
	Preview string `json:"preview,omitempty"`
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
	e.Preview = ""
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return
	}
	if st, err := os.Stat(a.path); err == nil && st.Size() > auditCap {
		_ = os.Rename(a.path, a.path+".1") // the one before is kept, the rest goes
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
	if w, ok := args["write"].(string); ok { // agent.act
		sum := sha256.Sum256([]byte(w))
		args["write"] = map[string]any{"length": len(w), "sha256": hex.EncodeToString(sum[:8])}
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
