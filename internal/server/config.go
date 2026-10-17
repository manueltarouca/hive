package server

import (
	"context"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/config"
)

// fileStamp tells one version of a file from another: inode, mtime and size,
// so a rename-saving editor or a same-second edit still counts.
type fileStamp struct {
	ino   uint64
	mtime time.Time
	size  int64
	ok    bool
}

func stampOf(path string) fileStamp {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	st := fileStamp{mtime: fi.ModTime(), size: fi.Size(), ok: true}
	if sys, ok := fi.Sys().(*syscall.Stat_t); ok {
		st.ino = uint64(sys.Ino)
	}
	return st
}

// readConfig loads the settings file, or the defaults without one.
func (s *Server) readConfig() (config.Config, []config.Diagnostic, fileStamp) {
	if s.cfg.ConfigPath == "" {
		return config.Defaults(), nil, fileStamp{}
	}
	stamp := stampOf(s.cfg.ConfigPath)
	c, diags := config.Load(s.cfg.ConfigPath)
	return c, diags, stamp
}

// reloadConfig reads the file again and applies what can change live:
// scrollback-limit to every terminal. resume-agents is read at start.
func (s *Server) reloadConfig() {
	// The poll and config.set can both reload: one at a time, so the newest
	// read is the one kept.
	s.confMu.Lock()
	defer s.confMu.Unlock()
	c, diags, stamp := s.readConfig()
	s.mu.Lock()
	old := s.conf.ScrollbackLimit
	s.conf, s.confDiags, s.confStamp = c, diags, stamp
	var blocks []*rec
	if c.ScrollbackLimit != old {
		for _, r := range s.blocks {
			blocks = append(blocks, r)
		}
	}
	s.mu.Unlock()
	for _, d := range diags {
		s.log.Warn("config", "path", s.cfg.ConfigPath, "problem", d.String())
	}
	for _, r := range blocks {
		if err := r.b.SetScrollbackLimit(c.ScrollbackLimit); err != nil && !strings.Contains(err.Error(), "closed") {
			s.log.Warn("scrollback limit", "err", err) // a pane closed meanwhile isn't news
		}
	}
}

// watchConfig picks up edits to the file, by any editor.
func (s *Server) watchConfig() {
	t := time.NewTicker(s.cfg.ConfigPoll)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
		}
		s.mu.Lock()
		last := s.confStamp
		s.mu.Unlock()
		if stampOf(s.cfg.ConfigPath) != last {
			s.reloadConfig()
		}
	}
}

type configView struct {
	Path        string              `json:"path"`
	Values      config.Config       `json:"values"`
	Diagnostics []config.Diagnostic `json:"diagnostics"`
}

func (s *Server) configView() configView {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.confDiags
	if d == nil {
		d = []config.Diagnostic{}
	}
	return configView{Path: s.cfg.ConfigPath, Values: s.conf, Diagnostics: d}
}

func (s *Server) registerConfigMethods() {
	type empty struct{}
	api.Register(s.reg, "config.get", "The server's settings: its file's path, every value, and problems in the file by line.",
		func(ctx context.Context, _ empty) (configView, error) { return s.configView(), nil })
	type setIn struct {
		Key   string `json:"key" required:"true" desc:"A setting, such as scrollback-limit"`
		Value string `json:"value" required:"true" desc:"Its new value; empty resets it to the default"`
	}
	api.Register(s.reg, "config.set", "Change one setting in the server's file, keeping the rest of the file, and apply it.",
		func(ctx context.Context, in setIn) (configView, error) {
			if s.cfg.ConfigPath == "" {
				return configView{}, api.BadRequest("this server has no settings file")
			}
			if err := config.Set(s.cfg.ConfigPath, in.Key, in.Value); err != nil {
				return configView{}, api.BadRequest("%v", err)
			}
			s.reloadConfig()
			return s.configView(), nil
		})
	api.Register(s.reg, "config.reload", "Read the settings file again and apply it.",
		func(ctx context.Context, _ empty) (configView, error) {
			s.reloadConfig()
			return s.configView(), nil
		})
}
