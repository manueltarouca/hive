package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/manueltarouca/hive/internal/api"
	"github.com/manueltarouca/hive/internal/events"
)

// CallRequest is the body of POST /v1/call.
type CallRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// CallResponse is its answer: a result or an error.
type CallResponse struct {
	Result any        `json:"result,omitempty"`
	Error  *api.Error `json:"error,omitempty"`
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/call", s.handleCall)
	mux.HandleFunc("GET /v1/events", s.handleEvents)
	mux.HandleFunc("GET /v1/attach", s.handleAttach)
	return noBrowsers(mux)
}

// noBrowsers refuses anything a web page could send. Browsers always send an
// Origin on cross-site requests, and a form or "simple" fetch can't set a
// JSON content type. The socket is local today; this keeps it safe when
// hive listens on a network.
func noBrowsers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			http.Error(w, "browser origins are not allowed", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			if ct, _, _ := strings.Cut(r.Header.Get("Content-Type"), ";"); strings.TrimSpace(ct) != "application/json" {
				http.Error(w, "content type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleCall(w http.ResponseWriter, r *http.Request) {
	var req CallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&req); err != nil {
		writeJSON(w, 400, CallResponse{Error: api.BadRequest("request: %v", err)})
		return
	}
	out, err := s.reg.Call(r.Context(), req.Method, req.Params)
	if err != nil {
		var e *api.Error
		if !errors.As(err, &e) {
			e = api.Internal("%v", err)
		}
		writeJSON(w, e.Status, CallResponse{Error: e})
		return
	}
	writeJSON(w, 200, CallResponse{Result: out})
}

// handleEvents streams events as JSON lines. ?session=ID follows one
// session, ?all=1 everything; ?since=N replays what came after event N.
// A heartbeat line is sent every 15 seconds. When the subscriber falls
// behind, a resync_required line is sent and the stream ends.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := events.Filter{SessionID: q.Get("session"), All: q.Get("all") == "1" || q.Get("all") == "true"}
	since := int64(events.Live)
	if v := q.Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeJSON(w, 400, CallResponse{Error: api.BadRequest("since must be a non-negative integer")})
			return
		}
		since = n
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, CallResponse{Error: api.Internal("streaming unsupported")})
		return
	}
	sub := s.bus.Subscribe(f, since, 1024)
	defer s.bus.Unsubscribe(sub)
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(200)
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]any{"type": "subscribed", "seq": s.bus.Seq()})
	if sub.Gap {
		// Some events after since are gone; the client should re-read state.
		_ = enc.Encode(map[string]any{"type": "resync_required", "reason": "since is older than the event history"})
	}
	fl.Flush()
	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.quit:
			return
		case <-hb.C:
			_ = enc.Encode(map[string]any{"type": "heartbeat", "seq": s.bus.Seq()})
			fl.Flush()
		case e, ok := <-sub.C:
			if !ok {
				if sub.Dropped() {
					_ = enc.Encode(map[string]any{"type": "resync_required"})
					fl.Flush()
				}
				return
			}
			if err := enc.Encode(e); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
