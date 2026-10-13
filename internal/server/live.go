package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// liveMsg is the handoff_live NOTIFY payload (see migration 003).
type liveMsg struct {
	Org     string `json:"org"`
	Kind    string `json:"kind"`
	Task    string `json:"task,omitempty"`
	Actor   string `json:"actor,omitempty"`
	Project string `json:"project,omitempty"`
}

type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan liveMsg]struct{}
}

func (h *hub) sub(org string) chan liveMsg {
	ch := make(chan liveMsg, 64)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[org] == nil {
		h.subs[org] = map[chan liveMsg]struct{}{}
	}
	h.subs[org][ch] = struct{}{}
	return ch
}

func (h *hub) unsub(org string, ch chan liveMsg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[org], ch)
}

// publish drops messages for slow clients; a dropped client catches up on its next refresh.
func (h *hub) publish(m liveMsg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for org, subs := range h.subs {
		if m.Org != "" && m.Org != org {
			continue
		}
		for ch := range subs {
			select {
			case ch <- m:
			default:
			}
		}
	}
}

// listen relays Postgres notifications to the hub, reconnecting forever.
func (s *Server) listen(ctx context.Context) {
	for ctx.Err() == nil {
		if err := s.listenOnce(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("live updates: listener lost, reconnecting", "err", err)
		}
		s.live.publish(liveMsg{Kind: "resync"})
		time.Sleep(time.Second)
	}
}

func (s *Server) listenOnce(ctx context.Context) error {
	conn, err := s.Svc.DB.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN handoff_live"); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			_ = conn.Conn().Close(context.Background())
			return err
		}
		var m liveMsg
		if json.Unmarshal([]byte(n.Payload), &m) == nil && m.Org != "" {
			s.live.publish(m)
		}
	}
}

// liveStream is a Server-Sent Events feed of changes in the caller's workspace.
// Each message only says which task changed; the web app refetches what it shows.
func (s *Server) liveStream(w http.ResponseWriter, r *http.Request) {
	p, err := s.sessionPrincipal(r)
	if err != nil || p == nil {
		writeErr(w, 401, "sign in first")
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	visible, err := s.Svc.VisibleProjects(r.Context(), p)
	if err != nil {
		writeCoreErr(w, err)
		return
	}
	s.liveOnce.Do(func() { go s.listen(context.Background()) })
	ch := s.live.sub(p.Org.ID)
	defer s.live.unsub(p.Org.ID, ch)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, "retry: 3000\n\n")
	fl.Flush()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	recheck := time.NewTicker(time.Minute)
	defer recheck.Stop()
	refresh := func() bool {
		if p, err = s.sessionPrincipal(r); err != nil || p == nil {
			return false
		}
		v, err := s.Svc.VisibleProjects(r.Context(), p)
		if err != nil {
			return false
		}
		visible = v
		return true
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
		case <-recheck.C:
			if !refresh() {
				return
			}
			continue
		case m := <-ch:
			if m.Kind == "event" && m.Task == "" && !refresh() {
				return
			}
			if m.Project != "" && !visible[m.Project] {
				continue
			}
			b, _ := json.Marshal(map[string]string{"kind": m.Kind, "task": m.Task, "actor": m.Actor})
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fl.Flush()
	}
}
