package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/manueltarouca/hive/internal/remote"
)

// RemoteConfig is how the server is reached beyond its socket.
type RemoteConfig struct {
	Listen []remote.Spec       // --extra-listen
	Policy remote.Policy       // who gets in besides the owner
	Node   *remote.NodeOptions // built-in Tailscale (--tailscale)
}

type listenerInfo struct {
	Kind string `json:"kind"`
	Addr string `json:"addr"`
}

// serveRemote opens the extra listeners and starts built-in Tailscale. A
// listener that can't open stops the server: one asked for and missing is
// worse than none.
func (s *Server) serveRemote() error {
	rc := s.cfg.Remote
	if rc == nil {
		return nil
	}
	for _, spec := range rc.Listen {
		if spec.Kind == remote.Plain && !rc.Policy.AllowAnonymous {
			return fmt.Errorf("%s: a plain listener can't tell callers apart; pass --allow-anonymous to let everyone who reaches it in", spec.Addr)
		}
		ls, err := remote.Open(context.Background(), spec, rc.Policy)
		if err != nil {
			return err
		}
		for _, l := range ls {
			s.addListener(l)
			if spec.Kind == remote.TailnetServe {
				s.removeOnStop = append(s.removeOnStop, spec.Path)
			}
		}
	}
	if rc.Node != nil {
		o := *rc.Node
		if o.Dir == "" {
			o.Dir = s.cfg.StateDir + "/tailscale"
		}
		o.Logf = func(f string, a ...any) { s.log.Info("tailscale: " + strings.TrimSpace(fmt.Sprintf(f, a...))) }
		n, err := remote.StartNode(o, rc.Policy, s.addListener, s.nodeChanged)
		if err != nil {
			return fmt.Errorf("tailscale: %w", err)
		}
		s.node = n
	}
	return nil
}

func (s *Server) addListener(l *remote.Listener) {
	s.mu.Lock()
	s.listeners = append(s.listeners, listenerInfo{Kind: l.Kind.String(), Addr: l.Addr().String()})
	s.mu.Unlock()
	s.log.Info("listening", "kind", l.Kind.String(), "addr", l.Addr().String())
	go func() {
		if err := s.srv.Serve(l); err != nil && err != http.ErrServerClosed {
			s.log.Error("listener stopped", "kind", l.Kind.String(), "addr", l.Addr().String(), "err", err)
		}
	}()
}

func (s *Server) nodeChanged(st remote.NodeStatus) {
	s.mu.Lock()
	s.nodeStatus = &st
	s.mu.Unlock()
	if st.AuthURL != "" {
		s.log.Info("tailscale: sign in to add this server to your tailnet", "url", st.AuthURL)
	} else {
		s.log.Info("tailscale", "state", st.State, "urls", strings.Join(st.URLs, " "), "err", st.Error)
	}
	s.bus.Server("tailscale_status_changed", map[string]any{"state": st.State, "auth_url": st.AuthURL,
		"dns_name": st.DNSName, "urls": st.URLs, "error": st.Error})
}

func (s *Server) stopRemote() {
	if s.node != nil {
		_ = s.node.Close()
	}
	for _, p := range s.removeOnStop {
		_ = os.Remove(p)
	}
}

func (s *Server) logDenied(r *http.Request, id remote.Identity, why string) {
	s.log.Warn("refused", "principal", id.Principal, "via", id.Via, "remote", r.RemoteAddr, "path", r.URL.Path, "why", why)
}

// remoteInfo is server.version's view of the listeners and Tailscale.
func (s *Server) remoteInfo() (ls []listenerInfo, node *remote.NodeStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ls = append([]listenerInfo{{Kind: "local", Addr: s.cfg.Socket}}, s.listeners...)
	if s.nodeStatus != nil {
		n := *s.nodeStatus
		node = &n
	}
	return ls, node
}
