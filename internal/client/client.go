// Package client talks to a hive server over its Unix socket.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
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

// Client is a connection to one server.
type Client struct {
	Socket string
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
	return &Client{Socket: socket, http: &http.Client{Transport: tr}}
}

// Ping reports whether the server answers.
func (c *Client) Ping() bool {
	conn, err := net.DialTimeout("unix", c.Socket, 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
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

// CallRaw calls a method and returns its result as JSON.
func (c *Client) CallRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
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
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://hive/v1/call", bytes.NewReader(body))
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
		return nil, fmt.Errorf("bad response (status %d): %w", resp.StatusCode, err)
	}
	if out.Error != nil {
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
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://hive/v1/events?"+q.Encode(), nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return &UnreachableError{err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("events: %s", bytes.TrimSpace(b))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
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
	return sc.Err()
}
