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
	// FromProject is set when the task moved; viewers of the old project need to hear it left.
	FromProject string `json:"from_project,omitempty"`
	// Progress from the agent at work (see core/activity.go). Member keeps a step to that member alone.
	Member string          `json:"member,omitempty"`
	Turn   string          `json:"turn,omitempty"`
	Step   json.RawMessage `json:"step,omitempty"`
	Done   bool            `json:"done,omitempty"`
}

// liveOut is what a browser gets: which task changed and who changed it, or the agent's progress.
type liveOut struct {
	Kind  string          `json:"kind"`
	Task  string          `json:"task"`
	Actor string          `json:"actor"`
	Turn  string          `json:"turn,omitempty"`
	Step  json.RawMessage `json:"step,omitempty"`
	Done  bool            `json:"done,omitempty"`
}

// forViewer is what one viewer may see of m, if anything.
func forViewer(m liveMsg, member string, visible map[string]bool) (liveOut, bool) {
	out := liveOut{Kind: m.Kind, Task: m.Task, Actor: m.Actor}
	if m.Member != "" && m.Member != member {
		return out, false
	}
	if m.Kind == "progress" && m.Member == "" && (m.Project == "" || !visible[m.Project]) {
		return out, false // task progress only goes to people who can see the task
	}
	if m.Project != "" && !visible[m.Project] {
		if !visible[m.FromProject] {
			return out, false
		}
		// Moved somewhere this viewer can't see: just tell them to refresh.
		out.Task, out.Actor = "", ""
	}
	if m.Kind == "progress" {
		out.Turn, out.Step, out.Done = m.Turn, m.Step, m.Done
	}
	return out, true
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
	recheck := time.NewTicker(15 * time.Second)
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
			out, ok := forViewer(m, p.Member.ID, visible)
			if !ok {
				continue
			}
			b, _ := json.Marshal(out)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fl.Flush()
	}
}
