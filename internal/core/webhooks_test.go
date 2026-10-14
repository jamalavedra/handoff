package core_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/core"
	"github.com/joalavedra/handoff/internal/testutil"
)

type hookInbox struct {
	mu     sync.Mutex
	got    []map[string]any
	fail   int // answer 500 this many times first
	secret string
	t      *testing.T
}

func (h *hookInbox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	// Verify the signature the way a receiver would.
	parts := map[string]string{}
	for _, kv := range strings.Split(r.Header.Get("X-Handoff-Signature"), ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			parts[k] = v
		}
	}
	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write([]byte(parts["t"] + "." + string(body)))
	if !hmac.Equal([]byte(parts["v1"]), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		h.t.Errorf("bad signature on %s", body)
	}
	if h.fail > 0 {
		h.fail--
		http.Error(w, "busy", http.StatusInternalServerError)
		return
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	h.got = append(h.got, m)
}

func (h *hookInbox) types() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, m := range h.got {
		out = append(out, m["type"].(string))
	}
	return out
}

func TestWebhooks(t *testing.T) {
	inbox := &hookInbox{t: t}
	srv := httptest.NewServer(inbox)
	t.Cleanup(srv.Close)
	base, _ := url.Parse("http://handoff.test")
	d := testutil.DB(t)
	s := core.New(d, &config.Config{BaseURL: base, Secret: []byte("s")})
	s.OutboundHTTP = srv.Client()
	e := &env{s: s, ctx: context.Background()}
	admin := e.org(t, "hooks")
	bot := e.agent(t, admin, "Report bot", nil)

	// Only the owner or an admin; the secret comes back once.
	if _, err := s.SetWebhook(e.ctx, bot, core.SetWebhookIn{Agent: "Report bot", URL: srv.URL}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("agent set its own webhook: %v", err)
	}
	hook, err := s.SetWebhook(e.ctx, admin, core.SetWebhookIn{Agent: "Report bot", URL: srv.URL + "/hook"})
	if err != nil || !strings.HasPrefix(hook.Secret, "whsec_") {
		t.Fatalf("set webhook: %+v %v", hook, err)
	}
	inbox.secret = hook.Secret
	again, _ := s.SetWebhook(e.ctx, admin, core.SetWebhookIn{Agent: "Report bot", URL: srv.URL + "/hook"})
	if again.Secret != "" {
		t.Fatal("secret returned again without rotate")
	}

	// Assigned and mentioned; and a task due today.
	task := e.create(t, admin, core.NewTask{Title: "Weekly report", Assignee: "Report bot", Repeat: "weekly", Due: "2000-01-07"})[0]
	if _, err := s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Report bot include the churn numbers"}); err != nil {
		t.Fatal(err)
	}
	inbox.fail = 1
	s.DeliverWebhooks(e.ctx) // first attempt of the first delivery fails...
	// ...so make its retry due now and run again.
	if err := d.InOrg(e.ctx, admin.Org.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.ctx, `UPDATE webhook_deliveries SET next_attempt_at = now()`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s.DeliverWebhooks(e.ctx)
	got := inbox.types()
	want := map[string]bool{"task.assigned": true, "comment.mention": true, "task.due": true}
	if len(got) != 3 {
		t.Fatalf("deliveries: %v", got)
	}
	for _, g := range got {
		if !want[g] {
			t.Fatalf("unexpected delivery %q in %v", g, got)
		}
	}
	for _, m := range inbox.got {
		task := m["task"].(map[string]any)
		if task["title"] != "Weekly report" || m["workspace"] != "hooks" || m["agent"].(map[string]any)["name"] != "Report bot" {
			t.Fatalf("payload: %v", m)
		}
	}
	// The same due date isn't sent twice.
	s.DeliverWebhooks(e.ctx)
	if n := len(inbox.types()); n != 3 {
		t.Fatalf("redelivered: %d", n)
	}

	// The owner sees the last status on the agent.
	ms, _ := s.ListMembers(e.ctx, admin, struct{}{})
	for _, m := range ms.Members {
		if m.Name == "Report bot" && (m.Webhook == nil || m.Webhook.LastStatus != 200 || m.Webhook.Secret != "") {
			t.Fatalf("webhook on member: %+v", m.Webhook)
		}
	}

	// A test ping, then removal stops deliveries.
	if out, err := s.TestWebhook(e.ctx, admin, core.AgentIn{Agent: "Report bot"}); err != nil || out.Status != 200 {
		t.Fatalf("ping: %+v %v", out, err)
	}
	if _, err := s.SetWebhook(e.ctx, admin, core.SetWebhookIn{Agent: "Report bot", URL: ""}); err != nil {
		t.Fatal(err)
	}
	e.create(t, admin, core.NewTask{Title: "after removal", Assignee: "Report bot"})
	s.DeliverWebhooks(e.ctx)
	if n := len(inbox.types()); n != 4 { // 3 + the ping
		t.Fatalf("delivered after removal: %v", inbox.types())
	}
}
