package server

import (
	"errors"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/remote"
)

// GET|HEAD /v1/file?path=P&block_id=B: a file a pane printed, for the apps'
// viewer. A relative path is the pane's: its program's folder, else its
// shell's (OSC 7), else where it started. A file comes raw (Range works, for
// the end of a log); a folder as a JSON listing, newest first. Anyone this
// server lets in can already read files through a terminal; a read from
// another machine is logged.
const maxListing = 500

type fileEntry struct {
	Name     string    `json:"name"`
	Dir      bool      `json:"dir"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

var lineSuffix = regexp.MustCompile(`^(.+?):(\d+)(?::\d+)?$`)

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, bid := q.Get("path"), q.Get("block_id")
	if p == "" {
		writeJSON(w, 400, CallResponse{Error: api.BadRequest("path is required")})
		return
	}
	if unsafePath(p) {
		writeJSON(w, 400, CallResponse{Error: api.BadRequest("the path has control characters")})
		return
	}
	if strings.HasPrefix(p, "file://") {
		u, err := url.Parse(p)
		if err != nil || !localHost(u.Host) {
			writeJSON(w, 404, CallResponse{Error: api.NotFound("%s is on another machine", p)})
			return
		}
		p = u.Path
	}
	dirs, err := s.fileDirs(bid)
	if err != nil {
		writeJSON(w, 404, CallResponse{Error: api.NotFound("%v", err)})
		return
	}
	full, line := resolveFile(p, dirs)
	if id := remote.IdentityOf(r.Context()); id.Via != remote.Local.String() {
		s.log.Info("file read", "principal", id.Principal, "via", id.Via, "path", full)
	}
	// Non-blocking, then the type from the open file: a pipe never blocks,
	// and a path swapped for something else after a check can't slip by.
	fd, err := syscall.Open(full, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC|syscall.O_NOCTTY, 0)
	if err != nil {
		switch {
		case errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENOTDIR):
			writeJSON(w, 404, CallResponse{Error: api.NotFound("no file at %s", full)})
		case errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM):
			writeJSON(w, 403, CallResponse{Error: api.PermissionDenied("can't read %s", full)})
		default:
			writeJSON(w, 400, CallResponse{Error: api.BadRequest("%s: %v", full, err)})
		}
		return
	}
	f := os.NewFile(uintptr(fd), full)
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeJSON(w, 400, CallResponse{Error: api.BadRequest("%v", err)})
		return
	}
	w.Header().Set("X-Hive-Path", full)
	switch {
	case fi.IsDir():
		w.Header().Set("X-Hive-Kind", "dir")
		listFolder(w, f, full)
	case fi.Mode().IsRegular():
		w.Header().Set("X-Hive-Kind", "file")
		if line != "" {
			w.Header().Set("X-Hive-Line", line)
		}
		_ = syscall.SetNonblock(fd, false)
		ct, text := fileType(f, full)
		w.Header().Set("Content-Type", ct)
		if text {
			w.Header().Set("X-Hive-Text", "1")
		}
		http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
	default:
		writeJSON(w, 400, CallResponse{Error: api.BadRequest("%s is not a file or a folder", full)})
	}
}

// fileDirs are where a relative path may be, for the block (or home without one).
func (s *Server) fileDirs(bid string) ([]string, error) {
	home, _ := os.UserHomeDir()
	if bid == "" {
		return []string{home}, nil
	}
	s.mu.Lock()
	rr, err := s.needBlock(bid)
	var pwd, cwd string
	if err == nil {
		pwd, cwd = rr.st.Pwd, expandHome(rr.st.Cwd)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var dirs []string
	if fg, err := rr.b.Foreground(); err == nil && fg.Cwd != "" {
		dirs = append(dirs, fg.Cwd)
	}
	if u, err := url.Parse(pwd); pwd != "" && err == nil && u.Scheme == "file" && localHost(u.Host) {
		dirs = append(dirs, u.Path)
	}
	if cwd != "" {
		dirs = append(dirs, cwd)
	}
	return append(dirs, home), nil
}

// resolveFile makes p absolute: as is, under ~, or in the first of dirs that
// has it. A :line (or :line:col) suffix is split off when the path without
// it is what exists.
func resolveFile(p string, dirs []string) (full, line string) {
	try := func(p string) (string, bool) {
		p = expandHome(p)
		if filepath.IsAbs(p) {
			_, err := os.Lstat(p)
			return filepath.Clean(p), err == nil
		}
		for _, d := range dirs {
			c := filepath.Join(d, p)
			if _, err := os.Lstat(c); err == nil {
				return c, true
			}
		}
		if len(dirs) > 0 {
			return filepath.Join(dirs[0], p), false
		}
		return p, false
	}
	full, ok := try(p)
	if !ok {
		if m := lineSuffix.FindStringSubmatch(p); m != nil {
			if f, ok := try(m[1]); ok {
				return f, m[2]
			}
		}
	}
	return full, ""
}

// unsafePath: control characters (an escape sequence in a path) and bidi
// overrides (a name that reads differently than it is).
func unsafePath(p string) bool {
	for _, r := range p {
		if r < 0x20 || r == 0x7f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0x200e || r == 0x200f {
			return true
		}
	}
	return false
}

// fileType is the file's content type, and whether it reads as text.
func fileType(f *os.File, name string) (string, bool) {
	head := make([]byte, 8192)
	n, _ := f.ReadAt(head, 0)
	head = head[:n]
	ct := mime.TypeByExtension(filepath.Ext(name))
	if ct == "" {
		ct = http.DetectContentType(head)
	}
	readable := n == 0 || (utf8.Valid(trimPartialRune(head)) && !strings.ContainsRune(string(head), 0))
	text := strings.HasPrefix(ct, "text/") || strings.Contains(ct, "json") || strings.Contains(ct, "xml") || strings.Contains(ct, "javascript")
	switch {
	case text:
	case strings.HasPrefix(ct, "image/"):
		// Images are binary; one that decodes as text isn't an image (a .png that's a note).
		if readable && n > 0 && !strings.Contains(ct, "svg") {
			text, ct = true, "text/plain; charset=utf-8"
		}
	default:
		// Source files the extension calls something else (.ts is video/mp2t to
		// mime.types) or nothing: text when it decodes and has no NULs.
		if readable {
			text, ct = true, "text/plain; charset=utf-8"
		}
	}
	return ct, text
}

// trimPartialRune drops a UTF-8 sequence cut by the end of the sample.
func trimPartialRune(b []byte) []byte {
	for i := 0; i < 3 && len(b) > 0; i++ {
		if r, _ := utf8.DecodeLastRune(b); r != utf8.RuneError {
			return b
		}
		b = b[:len(b)-1]
	}
	return b
}

func listFolder(w http.ResponseWriter, f *os.File, path string) {
	entries, err := f.ReadDir(-1)
	if err != nil {
		writeJSON(w, 400, CallResponse{Error: api.BadRequest("%v", err)})
		return
	}
	out := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, fileEntry{Name: e.Name(), Dir: e.IsDir(), Size: fi.Size(), Modified: fi.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	truncated := len(out) > maxListing
	if truncated {
		out = out[:maxListing]
	}
	writeJSON(w, 200, map[string]any{"path": path, "entries": out, "truncated": truncated})
}
