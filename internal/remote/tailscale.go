//go:build !notsnet

package remote

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/client/tailscale/apitype"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tsnet"
)

// Built reports whether this binary has Tailscale (the notsnet build
// doesn't).
const Built = true

// tsWho asks a Tailscale local API: the machine's tailscaled for the Serve
// socket and tailnet addresses, or the built-in node's own.
type tsWho struct {
	lc *local.Client

	mu      sync.Mutex
	st      *ipnstate.Status
	fetched time.Time
}

// status is cached briefly: Names and Owner run on every request.
func (w *tsWho) status(ctx context.Context) (*ipnstate.Status, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.st != nil && time.Since(w.fetched) < 10*time.Second {
		return w.st, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	st, err := w.lc.StatusWithoutPeers(ctx)
	if err != nil {
		return nil, err
	}
	if st.Self == nil {
		return nil, errors.New("not connected to a tailnet")
	}
	w.st, w.fetched = st, time.Now()
	return st, nil
}

func (w *tsWho) WhoIs(ctx context.Context, addr string) (Peer, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	st, err := w.status(ctx)
	if err != nil {
		return Peer{}, err
	}
	r, err := w.lc.WhoIs(ctx, addr)
	if err != nil {
		return Peer{}, err
	}
	return peerOf(r, st.Self)
}

// peerOf reads a WhoIs answer. This node itself is refused: a process on
// this machine, any account's, that connects to its own tailnet address is
// identified as the node, and so as its owner. Local callers use the
// socket, whose permissions say who they are.
func peerOf(r *apitype.WhoIsResponse, self *ipnstate.PeerStatus) (Peer, error) {
	if r.Node == nil {
		return Peer{}, errors.New("no such node")
	}
	if self != nil && r.Node.StableID == self.ID {
		return Peer{}, errors.New("a connection from this machine itself: use the local socket")
	}
	p := Peer{Node: strings.TrimSuffix(r.Node.Name, ".")}
	if len(r.Node.Tags) > 0 {
		// A tagged device shares the tagged-devices pseudo-user: it's
		// known by its tags, never by a login.
		p.Tags = append([]string(nil), r.Node.Tags...)
		return p, nil
	}
	if r.UserProfile != nil {
		p.Login = r.UserProfile.LoginName
	}
	return p, nil
}

func (w *tsWho) Owner(ctx context.Context) (string, error) {
	st, err := w.status(ctx)
	if err != nil {
		return "", err
	}
	if st.Self.Tags != nil && st.Self.Tags.Len() > 0 {
		return "", nil // a tagged device has no owner
	}
	u, ok := st.User[st.Self.UserID]
	if !ok {
		return "", nil
	}
	return u.LoginName, nil
}

func (w *tsWho) Names(ctx context.Context) ([]string, error) {
	st, err := w.status(ctx)
	if err != nil {
		return nil, err
	}
	return selfNames(st), nil
}

func selfNames(st *ipnstate.Status) []string {
	var n []string
	if d := strings.TrimSuffix(st.Self.DNSName, "."); d != "" {
		n = append(n, d)
		if short, _, ok := strings.Cut(d, "."); ok {
			n = append(n, short)
		}
	}
	for _, ip := range st.Self.TailscaleIPs {
		n = append(n, ip.String())
	}
	return n
}

// Open starts the listeners for an --extra-listen spec. Tailnet binds every
// tailnet address of the machine, so it can be more than one.
func Open(ctx context.Context, s Spec, pol Policy) ([]*Listener, error) {
	switch s.Kind {
	case Plain:
		return openPlain(s, pol)
	case TailnetServe:
		who := &tsWho{lc: &local.Client{}}
		if _, err := who.status(ctx); err != nil {
			return nil, fmt.Errorf("%s: tailscale isn't running here: %v", s.Path, err)
		}
		_ = os.Remove(s.Path)
		old := syscall.Umask(0o077) // tailscaled (root) reaches it; nobody else
		ln, err := net.Listen("unix", s.Path)
		syscall.Umask(old)
		if err != nil {
			return nil, err
		}
		// The umask is process-wide; make sure, as the local socket does.
		if err := os.Chmod(s.Path, 0o600); err != nil {
			ln.Close()
			return nil, err
		}
		return []*Listener{{Listener: ln, Kind: TailnetServe, Who: who, Policy: pol}}, nil
	case Tailnet:
		who := &tsWho{lc: &local.Client{}}
		st, err := who.status(ctx)
		if err != nil {
			return nil, fmt.Errorf("tailnet://%s: tailscale isn't running here: %v", s.Addr, err)
		}
		host, port, _ := net.SplitHostPort(s.Addr)
		var addrs []string
		if host != "" {
			// Only this node's tailnet addresses: anything else would serve
			// plain HTTP beyond the tailnet.
			ok := false
			for _, ip := range st.Self.TailscaleIPs {
				ok = ok || ip.String() == strings.Trim(host, "[]")
			}
			if !ok {
				return nil, fmt.Errorf("tailnet://%s: %s isn't one of this machine's tailnet addresses", s.Addr, host)
			}
			addrs = []string{s.Addr}
		} else {
			for _, ip := range st.Self.TailscaleIPs {
				addrs = append(addrs, net.JoinHostPort(ip.String(), port))
			}
		}
		if len(addrs) == 0 {
			return nil, fmt.Errorf("tailnet://%s: this machine has no tailnet address", s.Addr)
		}
		var out []*Listener
		for _, a := range addrs {
			ln, err := net.Listen("tcp", a)
			if err != nil {
				for _, l := range out {
					l.Close()
				}
				return nil, fmt.Errorf("tailnet://%s: %v", a, err)
			}
			out = append(out, &Listener{Listener: ln, Kind: Tailnet, Who: who, Policy: pol})
		}
		return out, nil
	}
	return nil, fmt.Errorf("listener kind %v isn't opened with Open", s.Kind)
}

// NodeOptions configure built-in Tailscale.
type NodeOptions struct {
	Hostname   string   // default "hive"
	Dir        string   // its state: keys and login
	AuthKey    string   // sign in without a browser, once
	Tags       []string // sign in as a tagged device (the key must allow them)
	ControlURL string   // a control server other than Tailscale's, such as Headscale
	Port       int      // plain HTTP port on the node; default DefaultPort
	Logf       func(format string, args ...any)
}

// NodeStatus is built-in Tailscale's state, for server.version and the
// tailscale_status_changed event.
type NodeStatus struct {
	State   string   `json:"state"`
	AuthURL string   `json:"auth_url,omitempty"`
	DNSName string   `json:"dns_name,omitempty"`
	URLs    []string `json:"urls,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// Node is built-in Tailscale: hive joins the tailnet as its own device.
type Node struct {
	srv    *tsnet.Server
	cancel context.CancelFunc
}

// StartNode joins the tailnet in the background. listen is called with each
// listener once the node is up (HTTPS on 443 when the tailnet has
// certificates, plain HTTP on Port always); status with every change.
func StartNode(o NodeOptions, pol Policy, listen func(*Listener), status func(NodeStatus)) (*Node, error) {
	if o.Hostname == "" {
		o.Hostname = "hive"
	}
	if o.Port == 0 {
		o.Port = DefaultPort
	}
	if err := os.MkdirAll(o.Dir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(o.Dir, 0o700)
	discard := func(string, ...any) {}
	srv := &tsnet.Server{Dir: o.Dir, Hostname: o.Hostname, AuthKey: o.AuthKey, AdvertiseTags: o.Tags,
		ControlURL: o.ControlURL, Logf: discard, UserLogf: discard}
	if o.Logf != nil {
		srv.UserLogf = o.Logf
	}
	if err := srv.Start(); err != nil {
		return nil, err
	}
	lc, err := srv.LocalClient()
	if err != nil {
		srv.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := &Node{srv: srv, cancel: cancel}
	go n.run(ctx, lc, o, pol, listen, status)
	return n, nil
}

func (n *Node) run(ctx context.Context, lc *local.Client, o NodeOptions, pol Policy, listen func(*Listener), status func(NodeStatus)) {
	who := &tsWho{lc: lc}
	var last NodeStatus
	report := func(s NodeStatus) {
		if s.State != last.State || s.AuthURL != last.AuthURL || s.Error != last.Error || len(s.URLs) != len(last.URLs) {
			last = s
			status(s)
		}
	}
	started := false
	for ctx.Err() == nil {
		w, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
		if err != nil {
			report(NodeStatus{State: "error", Error: err.Error()})
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			continue
		}
		for ctx.Err() == nil {
			nf, err := w.Next()
			if err != nil {
				break
			}
			s := last
			if nf.State != nil {
				s.State = nf.State.String()
			}
			if nf.BrowseToURL != nil {
				s.AuthURL = *nf.BrowseToURL
			}
			if s.State == ipn.Running.String() {
				s.AuthURL = ""
				if !started {
					started = true
					s = n.listen(ctx, who, o, pol, listen, s, report)
				}
			}
			report(s)
		}
		w.Close()
	}
}

func (n *Node) listen(ctx context.Context, who *tsWho, o NodeOptions, pol Policy, listen func(*Listener), s NodeStatus, report func(NodeStatus)) NodeStatus {
	st, err := who.status(ctx)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	s.DNSName = strings.TrimSuffix(st.Self.DNSName, ".")
	if ln, err := n.srv.Listen("tcp", fmt.Sprintf(":%d", o.Port)); err == nil {
		listen(&Listener{Listener: ln, Kind: Tailscale, Who: who, Policy: pol})
		s.URLs = append(s.URLs, fmt.Sprintf("http://%s:%d", s.DNSName, o.Port))
	} else {
		s.Error = err.Error()
	}
	full, _ := n.srv.LocalClient()
	if fst, err := full.Status(ctx); err == nil && len(fst.CertDomains) > 0 {
		// The certificate first: issuing one takes 10-30 s, longer than a
		// first handshake waits. Plain HTTP serves meanwhile.
		s.State = "fetching certificate"
		report(s)
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		_, _, cerr := full.CertPair(cctx, fst.CertDomains[0])
		cancel()
		s.State = ipn.Running.String()
		if cerr != nil {
			s.Error = "https: " + cerr.Error()
		} else if ln, err := n.srv.Listen("tcp", ":443"); err == nil {
			// HTTP/1.1 only: the attach stream is an HTTP/1.1 upgrade.
			tl := tls.NewListener(ln, &tls.Config{GetCertificate: full.GetCertificate, NextProtos: []string{"http/1.1"}})
			listen(&Listener{Listener: tl, Kind: Tailscale, Who: who, Policy: pol})
			s.URLs = append([]string{"https://" + s.DNSName}, s.URLs...)
		}
	}
	return s
}

// Close leaves the tailnet (the node's login stays in its state dir).
func (n *Node) Close() error {
	n.cancel()
	return n.srv.Close()
}
