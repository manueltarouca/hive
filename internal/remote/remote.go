// Package remote serves hive beyond its unix socket: the listener kinds
// (Tailscale Serve, a tailnet address, built-in Tailscale, plain TCP), who
// each request speaks for, and whether it's let in.
//
// A connection can do anything the server's account can, so the rules are
// strict. The principal is decided per request, before any route: on the
// Serve socket from Serve's identity header, since tailscaled's proxy reuses
// one connection for many users; elsewhere from the connection's peer. A
// tagged device has no owner. Anonymous callers get in only on a plain TCP
// listener started with --allow-anonymous.
package remote

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
)

// DefaultPort is hive's port for tailnet and plain listeners (HIVE on a
// phone keypad).
const DefaultPort = 4483

// Kind is how a listener is reached, which decides how callers are known.
type Kind int

const (
	Local        Kind = iota // the server's own unix socket
	TailnetServe             // a unix socket behind `tailscale serve`
	Tailnet                  // the machine's tailnet addresses, plain HTTP
	Tailscale                // built-in Tailscale (tsnet), HTTPS
	Plain                    // plain TCP, anonymous
)

func (k Kind) String() string {
	return [...]string{"local", "tailnet-serve", "tailnet", "tailscale", "plain"}[k]
}

// Spec is one --extra-listen value.
type Spec struct {
	Kind Kind
	Path string // TailnetServe: the socket
	Addr string // Tailnet, Plain: host:port
}

// ParseSpec reads an --extra-listen value: tailnet-serve:///path.sock,
// tailnet://:PORT, or [HOST]:PORT.
func ParseSpec(s string) (Spec, error) {
	switch {
	case strings.HasPrefix(s, "tailnet-serve://"):
		p := strings.TrimPrefix(s, "tailnet-serve://")
		if !strings.HasPrefix(p, "/") {
			return Spec{}, fmt.Errorf("%s: tailnet-serve needs an absolute socket path, such as tailnet-serve:///run/hive/serve.sock", s)
		}
		return Spec{Kind: TailnetServe, Path: p}, nil
	case strings.HasPrefix(s, "tailnet://"):
		a := strings.TrimPrefix(s, "tailnet://")
		if err := checkPort(a); err != nil {
			return Spec{}, fmt.Errorf("%s: %w (tailnet://:%d)", s, err, DefaultPort)
		}
		return Spec{Kind: Tailnet, Addr: a}, nil
	case strings.Contains(s, "://"):
		return Spec{}, fmt.Errorf("%s: unknown listener; use tailnet-serve:///PATH, tailnet://:PORT or :PORT", s)
	default:
		if err := checkPort(s); err != nil {
			return Spec{}, fmt.Errorf("%s: %w", s, err)
		}
		return Spec{Kind: Plain, Addr: s}, nil
	}
}

func checkPort(hostport string) error {
	_, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return errors.New("needs HOST:PORT or :PORT")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return errors.New("needs a port")
	}
	return nil
}

// Peer is who Tailscale says is on the other end.
type Peer struct {
	Login string   // a person's login, such as alice@github; empty for a tagged device
	Tags  []string // a tagged device's tags, such as tag:ci
	Node  string   // the device's name
}

// Principal is the peer in hive's principal format.
func (p Peer) Principal() string {
	if len(p.Tags) > 0 {
		return "tailscale:" + p.Tags[0]
	}
	if p.Login != "" {
		return "tailscale:" + p.Login
	}
	return "anonymous"
}

// Who asks Tailscale about this node and its peers.
type Who interface {
	// WhoIs names the peer at a remote address.
	WhoIs(ctx context.Context, remoteAddr string) (Peer, error)
	// Owner is the login that owns this node, or "" when it's tagged (a
	// tagged device has no owner).
	Owner(ctx context.Context) (string, error)
	// Names are the names and addresses this node answers to.
	Names(ctx context.Context) ([]string, error)
}

// Policy widens who gets in, beyond the owner.
type Policy struct {
	AllowIdentified bool // --tailnet-allow-identified: anyone Tailscale identifies (Serve, tailnet)
	AllowMembers    bool // --tailscale-allow-tailnet-members: anyone the ACLs let reach the device (built-in)
	AllowAnonymous  bool // --allow-anonymous: plain TCP listeners only
}

// Identity is who a request speaks for and how it arrived.
type Identity struct {
	Principal string   `json:"principal"`
	Via       string   `json:"via"`
	Login     string   `json:"login,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Node      string   `json:"node,omitempty"`
}

// Allowed decides a request: its listener, its peer, and this node's owner.
func (p Policy) Allowed(k Kind, peer Peer, owner string) bool {
	switch k {
	case Local:
		return true // the socket is 0600: only this account (and root) reach it
	case Plain:
		return p.AllowAnonymous
	}
	widen := p.AllowIdentified
	if k == Tailscale {
		widen = p.AllowMembers
	}
	switch {
	case len(peer.Tags) > 0:
		return widen // never the owner, whatever its user ID says
	case peer.Login == "":
		return false // anonymous is never let in through Tailscale's listeners
	case owner != "" && strings.EqualFold(peer.Login, owner):
		return true
	default:
		return widen
	}
}

// Listener is a net.Listener whose connections know where they came from.
type Listener struct {
	net.Listener
	Kind   Kind
	Who    Who // nil for Plain
	Policy Policy
}

func (l *Listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &Conn{Conn: c, l: l}, nil
}

// Conn is an accepted connection. Its peer is looked up once (one TCP
// connection is one peer), except on the Serve socket, where identity is
// per request.
type Conn struct {
	net.Conn
	l    *Listener
	once sync.Once
	peer Peer
	err  error
}

func (c *Conn) peerOnce(ctx context.Context) (Peer, error) {
	c.once.Do(func() { c.peer, c.err = c.l.Who.WhoIs(ctx, c.RemoteAddr().String()) })
	return c.peer, c.err
}

type connKey struct{}
type identityKey struct{}

// ConnContext is http.Server's ConnContext: it keeps the connection, and
// names a local socket's peer by user ID.
func ConnContext(ctx context.Context, c net.Conn) context.Context {
	if rc, ok := c.(*Conn); ok {
		return context.WithValue(ctx, connKey{}, rc)
	}
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return ctx // not the local socket, and not vetted: Authorize refuses it
	}
	p := "anonymous"
	if uid, err := peerUID(uc); err == nil {
		p = fmt.Sprintf("peer:%d", uid)
	}
	return context.WithValue(ctx, identityKey{}, &Identity{Principal: p, Via: Local.String()})
}

// IdentityOf is who a request speaks for, once Authorize let it in.
func IdentityOf(ctx context.Context) Identity {
	if id, ok := ctx.Value(identityKey{}).(*Identity); ok {
		return *id
	}
	return Identity{Principal: "anonymous"}
}

// Authorize decides every request before it reaches a route: who it speaks
// for, whether that's let in, and (on plain HTTP) that a browser didn't
// send it by way of DNS rebinding.
func Authorize(next http.Handler, logDenied func(r *http.Request, id Identity, why string)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Browsers send Sec-Fetch-* to HTTPS and localhost (not to a plain
		// http://100.x address: the Host checks below cover that); native
		// clients never do.
		if r.Header.Get("Sec-Fetch-Site") != "" || r.Header.Get("Sec-Fetch-Mode") != "" {
			deny(w, r, Identity{Principal: "anonymous"}, "browser requests are not allowed", logDenied)
			return
		}
		c, _ := r.Context().Value(connKey{}).(*Conn)
		if c == nil {
			// Only the local socket's connections arrive unwrapped, named by
			// ConnContext. Anything else is a listener nobody vetted.
			if id, ok := r.Context().Value(identityKey{}).(*Identity); !ok || id.Via != Local.String() {
				deny(w, r, Identity{Principal: "anonymous"}, "unknown listener", logDenied)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		id, owner, err := identify(r, c)
		if err != nil {
			deny(w, r, id, err.Error(), logDenied)
			return
		}
		if !c.l.Policy.Allowed(c.l.Kind, Peer{Login: id.Login, Tags: id.Tags}, owner) {
			deny(w, r, id, id.Principal+" may not use this server", logDenied)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, &id)))
	})
}

func identify(r *http.Request, c *Conn) (Identity, string, error) {
	id := Identity{Principal: "anonymous", Via: c.l.Kind.String()}
	ctx := r.Context()
	switch c.l.Kind {
	case Plain:
		if !isIP(r.Host) {
			if err := checkHost(r.Host, plainNames()); err != nil {
				return id, "", err
			}
		}
		return id, "", nil
	case TailnetServe:
		// Funnel brings the internet to the same socket: never.
		if r.Header.Get("Tailscale-Funnel-Request") != "" {
			return id, "", errors.New("funnel requests are not allowed")
		}
		// Serve encodes non-ASCII values (RFC 2047).
		if login, err := new(mime.WordDecoder).DecodeHeader(r.Header.Get("Tailscale-User-Login")); err == nil && login != "" {
			id.Login, id.Principal = login, "tailscale:"+login
		}
	case Tailnet, Tailscale:
		// Both can be plain HTTP to a tailnet address, where a rebound name
		// reaches them from the owner's own browser.
		names, err := c.l.Who.Names(ctx)
		if err != nil {
			return id, "", fmt.Errorf("tailscale: %v", err)
		}
		if err := checkHost(r.Host, names); err != nil {
			return id, "", err
		}
		p, err := c.peerOnce(ctx)
		if err != nil {
			return id, "", fmt.Errorf("unknown caller: %v", err)
		}
		id.Login, id.Tags, id.Node, id.Principal = p.Login, p.Tags, p.Node, p.Principal()
	}
	owner, err := c.l.Who.Owner(ctx)
	if err != nil {
		return id, "", fmt.Errorf("tailscale: %v", err)
	}
	return id, owner, nil
}

// checkHost refuses a Host header this node doesn't answer to: after DNS
// rebinding, a page's requests name the attacker's host.
func checkHost(host string, names []string) error {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.TrimSuffix(strings.ToLower(strings.Trim(h, "[]")), ".")
	for _, n := range names {
		if strings.EqualFold(h, strings.TrimSuffix(n, ".")) {
			return nil
		}
	}
	return fmt.Errorf("host %q is not this server", host)
}

// plainNames are what a plain listener answers to: any IP address, and
// this machine's own names.
func plainNames() []string {
	n := []string{"localhost"}
	if h, err := os.Hostname(); err == nil {
		n = append(n, h, strings.TrimSuffix(h, ".local"), h+".local")
	}
	return n
}

func isIP(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	_, err := netip.ParseAddr(strings.Trim(h, "[]"))
	return err == nil
}

func deny(w http.ResponseWriter, r *http.Request, id Identity, why string, log func(*http.Request, Identity, string)) {
	if log != nil {
		log(r, id, why)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, `{"error":{"status":403,"code":"forbidden","message":%q}}`+"\n", why)
}
