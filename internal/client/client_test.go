package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeServer answers whoami with the protocol it was reached over.
func fakeServer(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/call" || r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "bad request", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if status == 403 {
			w.WriteHeader(403)
			io.WriteString(w, `{"error":{"status":403,"code":"forbidden","message":"tailscale:bob@github may not use this server"}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"principal": "tailscale:alice@github", "proto": r.Proto, "host": r.Host}})
	})
}

func TestNewURL(t *testing.T) {
	for _, c := range []struct {
		in, err string
	}{
		{"https://box.example.ts.net", ""},
		{"http://100.64.0.1:4483", ""},
		{"https://box.example.ts.net/", ""},
		{"https://root@box.example.ts.net", "account"},
		{"ftp://box", "https:// or http://"},
		{"https://box/v1", "path"},
		{"https://", "host"},
	} {
		_, err := NewURL(c.in)
		if (err == nil) != (c.err == "") || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("NewURL(%q) = %v, want an error mentioning %q", c.in, err, c.err)
		}
	}
}

func TestCallOverHTTPAndTLS(t *testing.T) {
	plain := httptest.NewServer(fakeServer(200))
	defer plain.Close()
	tlsSrv := httptest.NewUnstartedServer(fakeServer(200))
	tlsSrv.EnableHTTP2 = true // it would speak h2 if the client offered it
	tlsSrv.StartTLS()
	defer tlsSrv.Close()

	for _, srv := range []*httptest.Server{plain, tlsSrv} {
		c, err := NewURL(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		if srv == tlsSrv {
			c.trustForTests(srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs)
		}
		var out map[string]string
		if err := c.Call(context.Background(), "whoami", nil, &out); err != nil {
			t.Fatalf("%s: %v", srv.URL, err)
		}
		// The attach stream is an HTTP/1.1 upgrade: never h2.
		if out["proto"] != "HTTP/1.1" {
			t.Errorf("%s: spoke %s, want HTTP/1.1", srv.URL, out["proto"])
		}
		if !c.Remote() || !c.Ping() {
			t.Errorf("%s: Remote %v, Ping %v", srv.URL, c.Remote(), c.Ping())
		}
	}
}

// A server whose certificate isn't trusted is not reached: verification is
// never skipped.
func TestUntrustedCertificateRefused(t *testing.T) {
	srv := httptest.NewTLSServer(fakeServer(200))
	defer srv.Close()
	c, _ := NewURL(srv.URL)
	var u *UnreachableError
	if err := c.Call(context.Background(), "whoami", nil, nil); !errors.As(err, &u) || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("err = %v, want a certificate error", err)
	}
}

// A server that accepts the connection and never answers doesn't hang a
// command forever, and a silent event stream ends.
func TestRemoteCallHasADeadline(t *testing.T) {
	if callTimeout > 5*time.Minute || callTimeout < time.Minute {
		t.Fatalf("callTimeout %v", callTimeout)
	}
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	defer srv.Close()
	defer close(block)
	c, _ := NewURL(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := c.Call(ctx, "whoami", nil, nil); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
}

// A server that refuses us still answered: reachable, and the error says
// who it refused.
func TestForbiddenIsReachable(t *testing.T) {
	srv := httptest.NewServer(fakeServer(403))
	defer srv.Close()
	c, _ := NewURL(srv.URL)
	if !c.Ping() {
		t.Fatal("a server that answered 403 isn't reachable?")
	}
	err := c.Call(context.Background(), "whoami", nil, nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != "forbidden" || !strings.Contains(e.Message, "bob@github") {
		t.Fatalf("err = %v", err)
	}
}

func TestUnreachableRemote(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	c, _ := NewURL("http://" + addr)
	if c.Ping() {
		t.Fatal("nothing listens there")
	}
	var u *UnreachableError
	if err := c.Call(context.Background(), "whoami", nil, nil); !errors.As(err, &u) {
		t.Fatalf("err = %v, want UnreachableError", err)
	}
}

// A bare host tries https on 443, then https and http on hive's port.
func TestCandidates(t *testing.T) {
	got := Candidates("box.example.ts.net")
	want := []string{"https://box.example.ts.net", "https://box.example.ts.net:4483", "http://box.example.ts.net:4483"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Candidates = %v, want %v", got, want)
	}
	got = Candidates("fd7a:115c:a1e0::1")
	want = []string{"https://[fd7a:115c:a1e0::1]", "https://[fd7a:115c:a1e0::1]:4483", "http://[fd7a:115c:a1e0::1]:4483"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("IPv6: %v, want %v", got, want)
	}
	for _, u := range want {
		if _, err := NewURL(u); err != nil {
			t.Errorf("candidate %s: %v", u, err)
		}
	}
	if got := Candidates("[fd7a:115c:a1e0::1]"); got[1] != "https://[fd7a:115c:a1e0::1]:4483" {
		t.Fatalf("bracketed IPv6: %v", got)
	}
	got = Candidates("box:9000")
	want = []string{"https://box:9000", "http://box:9000"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("with a port: %v, want %v", got, want)
	}
}

func TestProbeTakesTheFirstThatAnswers(t *testing.T) {
	srv := httptest.NewServer(fakeServer(200))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	// https on that port fails (it's plain HTTP); http answers.
	c, err := Probe(context.Background(), "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "http://127.0.0.1:"+port {
		t.Fatalf("picked %s", c.URL)
	}
}

func TestProbeNothing(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := Probe(ctx, addr); err == nil || !strings.Contains(err.Error(), addr) {
		t.Fatalf("err = %v", err)
	}
}
