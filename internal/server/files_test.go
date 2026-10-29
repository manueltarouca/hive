package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fileGet asks the server for a file over its socket, as an app does.
func (h *harness) fileGet(method, blockID, path string, hdr map[string]string) (*http.Response, []byte) {
	h.t.Helper()
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", h.sock)
		}}}
	q := url.Values{"path": {path}}
	if blockID != "" {
		q.Set("block_id", blockID)
	}
	req, _ := http.NewRequest(method, "http://hive/v1/file?"+q.Encode(), nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := c.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, b
}

func TestFileRead(t *testing.T) {
	h := start(t, t.TempDir(), nil)
	defer h.stop()
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	os.WriteFile(filepath.Join(dir, "shot.png"), png, 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "app.ts"), []byte("export const x: number = 1\n"), 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(dir, "notes.txt"), old, old)

	created := h.call("session.create", map[string]any{"cwd": dir, "command": []string{"sh", "-c", "exec cat"}})
	bid := strs(created["block_ids"])[0]

	t.Run("relative to the pane's folder", func(t *testing.T) {
		res, body := h.fileGet("GET", bid, "notes.txt", nil)
		if res.StatusCode != 200 || string(body) != "one\ntwo\nthree\n" {
			t.Fatalf("status %d body %q", res.StatusCode, body)
		}
		if got := res.Header.Get("X-Hive-Path"); got != filepath.Join(dir, "notes.txt") {
			t.Errorf("X-Hive-Path %q", got)
		}
		if res.Header.Get("X-Hive-Text") != "1" || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") {
			t.Errorf("text headers: %v", res.Header)
		}
	})
	t.Run("a line suffix", func(t *testing.T) {
		res, _ := h.fileGet("HEAD", bid, "notes.txt:2", nil)
		if res.StatusCode != 200 || res.Header.Get("X-Hive-Line") != "2" || res.Header.Get("Content-Length") != "14" {
			t.Fatalf("status %d headers %v", res.StatusCode, res.Header)
		}
	})
	t.Run("the end of a file", func(t *testing.T) {
		res, body := h.fileGet("GET", "", filepath.Join(dir, "notes.txt"), map[string]string{"Range": "bytes=-6"})
		if res.StatusCode != 206 || string(body) != "three\n" {
			t.Fatalf("status %d body %q", res.StatusCode, body)
		}
	})
	t.Run("an image", func(t *testing.T) {
		res, _ := h.fileGet("HEAD", bid, "./shot.png", nil)
		if res.Header.Get("Content-Type") != "image/png" || res.Header.Get("X-Hive-Text") != "" {
			t.Fatalf("headers %v", res.Header)
		}
	})
	t.Run("TypeScript is text, not video", func(t *testing.T) {
		res, _ := h.fileGet("HEAD", bid, "app.ts", nil)
		if res.Header.Get("X-Hive-Text") != "1" {
			t.Fatalf("headers %v", res.Header)
		}
	})
	t.Run("a folder, newest first", func(t *testing.T) {
		res, body := h.fileGet("GET", bid, ".", nil)
		var out struct {
			Path    string `json:"path"`
			Entries []struct {
				Name string `json:"name"`
				Dir  bool   `json:"dir"`
				Size int64  `json:"size"`
			} `json:"entries"`
			Truncated bool `json:"truncated"`
		}
		if err := json.Unmarshal(body, &out); err != nil || res.StatusCode != 200 {
			t.Fatalf("status %d %v %s", res.StatusCode, err, body)
		}
		if res.Header.Get("X-Hive-Kind") != "dir" {
			t.Errorf("X-Hive-Kind %q", res.Header.Get("X-Hive-Kind"))
		}
		if out.Path != dir || len(out.Entries) != 4 || out.Entries[len(out.Entries)-1].Name != "notes.txt" || out.Truncated {
			t.Fatalf("listing %+v", out)
		}
	})
	t.Run("a pipe doesn't hang", func(t *testing.T) {
		fifo := filepath.Join(dir, "pipe")
		if err := syscall.Mkfifo(fifo, 0o644); err != nil {
			t.Skip(err)
		}
		res, _ := h.fileGet("GET", bid, "pipe", nil)
		if res.StatusCode != 400 {
			t.Fatalf("status %d", res.StatusCode)
		}
		os.Remove(fifo)
	})
	t.Run("a symlink loop", func(t *testing.T) {
		os.Symlink("loop", filepath.Join(dir, "loop"))
		res, _ := h.fileGet("GET", bid, "loop", nil)
		if res.StatusCode != 400 && res.StatusCode != 404 {
			t.Fatalf("status %d", res.StatusCode)
		}
		os.Remove(filepath.Join(dir, "loop"))
	})
	for name, c := range map[string]struct {
		block, path string
		status      int
	}{
		"not found":         {bid, "missing.txt", 404},
		"control character": {bid, "notes.txt\x1b[2J", 400},
		"bidi override":     {bid, "notes‮txt", 400},
		"no path":           {bid, "", 400},
		"unknown block":     {"block:nope", "notes.txt", 404},
		"relative, no pane": {"", "notes.txt", 404},
	} {
		t.Run(name, func(t *testing.T) {
			if res, body := h.fileGet("GET", c.block, c.path, nil); res.StatusCode != c.status {
				t.Fatalf("status %d, want %d: %s", res.StatusCode, c.status, body)
			}
		})
	}
}
