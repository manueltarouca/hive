// Package client talks to a hive server: over its Unix socket, or over the
// network to a remote server (HTTPS or HTTP, as on a tailnet).
package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Error is an error the server returned.
type Error struct {
	Status  int             `json:"status"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (status=%d code=%s)", e.Message, e.Status, e.Code)
}

// UnreachableError means the server could not be reached.
type UnreachableError struct{ Err error }

func (e *UnreachableError) Error() string { return "server unreachable: " + e.Err.Error() }
func (e *UnreachableError) Unwrap() error { return e.Err }

// DefaultPort is hive's port for tailnet and plain listeners (the server's
// remote.DefaultPort).
const DefaultPort = 4483

// Client is a connection to one server.
type Client struct {
	Socket string // the Unix socket; "" for a remote server
	URL    string // the server's base URL
	http   *http.Client
}

// New makes a client for the server on socket.
func New(socket string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &Client{Socket: socket, URL: "http://hive", http: &http.Client{Transport: tr}}
}

// NewURL makes a client for a remote server, such as
// https://box.example.ts.net or http://100.64.0.1:4483.
func NewURL(base string) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", base, err)
	}
	switch {
	case u.Scheme != "https" && u.Scheme != "http":
		return nil, fmt.Errorf("%s: a server URL starts with https:// or http://", base)
	case u.User != nil:
		// Reserved: user@host will choose an account on a login server.
		return nil, fmt.Errorf("%s: choosing an account (user@host) isn't supported", base)
	case u.Host == "":
		return nil, fmt.Errorf("%s: no host", base)
	case strings.Trim(u.Path, "/") != "" || u.RawQuery != "":
		return nil, fmt.Errorf("%s: a server URL has no path", base)
	}
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 0, // calls such as wait take as long as they take; contexts bound them
		// HTTP/1.1 only: the attach stream is an HTTP/1.1 upgrade.
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
		TLSClientConfig:   &tls.Config{NextProtos: []string{"http/1.1"}},
	}
	return &Client{URL: u.Scheme + "://" + u.Host, http: &http.Client{Transport: tr}}, nil
}

// Remote reports whether the server is reached over the network.
func (c *Client) Remote() bool { return c.Socket == "" }

// String names the server, for messages.
func (c *Client) String() string {
	if c.Remote() {
		return c.URL
	}
	return c.Socket
}

// Ping reports whether the server answers. A remote server that refuses
// this caller still answered.
func (c *Client) Ping() bool {
	if !c.Remote() {
		conn, err := net.DialTimeout("unix", c.Socket, 300*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := c.CallRaw(ctx, "server.version", nil)
	var e *Error
	return err == nil || errors.As(err, &e)
}

// Candidates are the URLs a bare host is tried at, in order: HTTPS on 443,
// then HTTPS and HTTP on hive's port. With a port, HTTPS then HTTP on it.
func Candidates(host string) []string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return []string{"https://" + host, "http://" + host}
	}
	bare := strings.Trim(host, "[]")
	h := bare
	if ip := net.ParseIP(bare); ip != nil && strings.Contains(bare, ":") {
		h = "[" + bare + "]" // an IPv6 address, as tailscale ip -6 prints it
	}
	p := fmt.Sprint(DefaultPort)
	return []string{"https://" + h, "https://" + net.JoinHostPort(bare, p), "http://" + net.JoinHostPort(bare, p)}
}

// Probe finds where a bare host's server answers.
func Probe(ctx context.Context, host string) (*Client, error) {
	var tried []string
	for _, u := range Candidates(host) {
		c, err := NewURL(u)
		if err != nil {
			tried = append(tried, u)
			continue
		}
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err = c.CallRaw(pctx, "server.version", nil)
		cancel()
		var e *Error
		if err == nil || errors.As(err, &e) {
			return c, nil
		}
		tried = append(tried, u)
	}
	return nil, &UnreachableError{fmt.Errorf("no hive server at %s (tried %s)", host, strings.Join(tried, ", "))}
}

// Call calls a method. params may be nil, a json.RawMessage, or any value
// that marshals to a JSON object. The result is decoded into out if it is
// not nil.
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	raw, err := c.CallRaw(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// callTimeout bounds a remote call that has no deadline of its own: a server
// that accepts the connection and never answers mustn't hang a command.
// Longer than any one step of a wait (50 s).
const callTimeout = 2 * time.Minute

// CallRaw calls a method and returns its result as JSON.
func (c *Client) CallRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if _, ok := ctx.Deadline(); !ok && c.Remote() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, callTimeout)
		defer cancel()
	}
	var p json.RawMessage
	switch v := params.(type) {
	case nil:
	case json.RawMessage:
		p = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		p = b
	}
	body, _ := json.Marshal(map[string]any{"method": method, "params": p})
	req, _ := http.NewRequestWithContext(ctx, "POST", c.URL+"/v1/call", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &UnreachableError{err}
	}
	defer resp.Body.Close()
	var out struct {
		Result json.RawMessage `json:"result"`
		Error  *Error          `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		if c.Remote() {
			// Something answered, but not hive (a proxy, another server).
			return nil, &UnreachableError{fmt.Errorf("%s answered %d, not as a hive server", c.URL, resp.StatusCode)}
		}
		return nil, fmt.Errorf("bad response (status %d): %w", resp.StatusCode, err)
	}
	if out.Error != nil {
		if c.Remote() && out.Error.Code == "forbidden" {
			out.Error.Message = c.URL + " refused: " + out.Error.Message
		}
		return nil, out.Error
	}
	return out.Result, nil
}

// Event is one line of the event stream.
type Event struct {
	Seq     uint64          `json:"seq"`
	Time    time.Time       `json:"time"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	Raw     json.RawMessage `json:"-"`
}

// Events streams events to fn until ctx ends, fn returns false, or the
// stream breaks. sessionID "" and all false means session lifecycle only.
// since < 0 means live events only.
func (c *Client) Events(ctx context.Context, sessionID string, all bool, since int64, fn func(Event) bool) error {
	q := url.Values{}
	if sessionID != "" {
		q.Set("session", sessionID)
	}
	if all {
		q.Set("all", "1")
	}
	if since >= 0 {
		q.Set("since", fmt.Sprint(since))
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", c.URL+"/v1/events?"+q.Encode(), nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return &UnreachableError{err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("events: %s", bytes.TrimSpace(b))
	}
	// The server sends a heartbeat every 15 s: three missed means the
	// connection is gone (a remote one can die without closing).
	var idle *time.Timer
	if c.Remote() {
		idle = time.AfterFunc(45*time.Second, func() { resp.Body.Close() })
		defer idle.Stop()
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		if idle != nil {
			idle.Reset(45 * time.Second)
		}
		var e Event
		line := append([]byte(nil), sc.Bytes()...)
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		e.Raw = line
		if !fn(e) {
			return nil
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := sc.Err(); err != nil {
		return err
	}
	// Not stopped by us: the server went away (stopped, restarted, unreachable).
	return errors.New("events: the server closed the stream")
}
