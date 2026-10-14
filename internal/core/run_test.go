package core_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/core"
	"github.com/joalavedra/handoff/internal/testutil"
)

func TestRunAutomation(t *testing.T) {
	inbox := &hookInbox{t: t}
	srv := httptest.NewServer(inbox)
	t.Cleanup(srv.Close)
	base, _ := url.Parse("http://handoff.test")
	d := testutil.DB(t)
	// No workers: requests for the workspace's agent stay queued, so the test can look at them.
	s := core.New(d, &config.Config{BaseURL: base, Secret: []byte("s")})
	s.OutboundHTTP = srv.Client()
	e := &env{s: s, ctx: context.Background()}
	admin := e.org(t, "runnow")
	acme := e.agent(t, admin, "Acme Bot", nil)
	e.agent(t, admin, "Report bot", nil)
	e.agent(t, admin, "Lazy bot", nil)
	if _, err := s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	hook, err := s.SetWebhook(e.ctx, admin, core.SetWebhookIn{Agent: "Report bot", URL: srv.URL + "/hook"})
	if err != nil {
		t.Fatal(err)
	}
	inbox.secret = hook.Secret
	// Due far ahead, so only Run now sets anything off.
	mk := func(title, assignee, repeat string) core.TaskOut {
		return e.create(t, admin, core.NewTask{Title: title, Assignee: assignee, Repeat: repeat, Due: "2999-01-01"})[0]
	}
	mine := mk("Water the plants", "me", "weekly")
	lazy := mk("Lazy report", "Lazy bot", "weekly")
	once := mk("One-off", "Report bot", "")
	report := mk("Weekly report", "Report bot", "weekly")
	digest := mk("Daily digest", "Acme Bot", "daily")
	s.DeliverWebhooks(e.ctx) // the "assigned" alerts, out of the way
	before := len(inbox.types())

	run := func(p *core.Principal, id string) (core.RunAutomationOut, error) {
		return s.RunAutomation(e.ctx, p, core.TaskIDIn{ID: id})
	}
	if _, err := run(acme, digest.ID); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("an agent ran an automation: %v", err)
	}
	for _, id := range []string{mine.ID, lazy.ID, once.ID} {
		if _, err := run(admin, id); !core.IsCode(err, core.ErrInvalid) {
			t.Fatalf("%s: %v", id, err)
		}
	}

	// The workspace's agent: one take-on request, not two.
	out, err := run(admin, digest.ID)
	if err != nil || out.Via != "agent" || out.Agent == nil || out.Agent.Name != "Acme Bot" {
		t.Fatalf("run workspace agent: %+v %v", out, err)
	}
	if _, err := run(admin, digest.ID); !core.IsCode(err, core.ErrConflict) {
		t.Fatalf("queued twice: %v", err)
	}
	var queued int
	if err := d.InOrg(e.ctx, admin.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, `SELECT count(*) FROM chat_replies WHERE task_id = $1 AND kind = 'take_on' AND state = 'pending'`, digest.ID).Scan(&queued)
	}); err != nil || queued != 1 {
		t.Fatalf("take-on requests: %d %v", queued, err)
	}

	// An agent with a webhook: task.due now, once per run.
	out, err = run(admin, report.ID)
	if err != nil || out.Via != "webhook" {
		t.Fatalf("run webhook agent: %+v %v", out, err)
	}
	if _, err := run(admin, report.ID); !core.IsCode(err, core.ErrConflict) {
		t.Fatalf("called twice: %v", err)
	}
	s.DeliverWebhooks(e.ctx)
	got := inbox.types()[before:]
	if len(got) != 1 || got[0] != "task.due" {
		t.Fatalf("deliveries: %v", got)
	}
	last := inbox.got[len(inbox.got)-1]
	if last["task"].(map[string]any)["id"] != report.ID || last["actor"].(map[string]any)["id"] != admin.Member.ID || last["excerpt"] != nil {
		t.Fatalf("payload: %v", last)
	}
	// Delivered: it can run again.
	if _, err := run(admin, report.ID); err != nil {
		t.Fatal(err)
	}
	s.DeliverWebhooks(e.ctx)
	if n := len(inbox.types()) - before; n != 2 {
		t.Fatalf("second run: %d deliveries", n)
	}

	// Run now is the upcoming occurrence done early: finishing it schedules the next one interval after
	// its original due date, not after today.
	soon := time.Now().UTC().AddDate(0, 0, 3)
	weekly := e.create(t, admin, core.NewTask{Title: "Weekly sync notes", Assignee: "Report bot", Repeat: "weekly", Due: soon.Format("2006-01-02")})[0]
	if _, err := run(admin, weekly.ID); err != nil {
		t.Fatal(err)
	}
	closed, err := s.CloseTask(e.ctx, admin, core.CloseTaskIn{ID: weekly.ID})
	if err != nil || closed.Next == nil || closed.Next.Due != soon.AddDate(0, 0, 7).Format("2006-01-02") {
		t.Fatalf("next after an early run: %+v %v", closed.Next, err)
	}

	// Activity shows who ran what.
	act, err := s.Activity(e.ctx, admin, core.ActivityIn{})
	if err != nil {
		t.Fatal(err)
	}
	runs := map[string]int{}
	for _, ev := range act.Events {
		if ev.Kind == "run_now" {
			runs[ev.TaskID]++
		}
	}
	if runs[digest.ID] != 1 || runs[report.ID] != 2 {
		t.Fatalf("run_now events: %v", runs)
	}
}

func TestRunAutomationAgentDoesIt(t *testing.T) {
	srv := fakeModel(t, http.StatusOK, map[string]any{"title": "Daily digest"}, new(atomic.Value))
	e := triageEnv(t, srv)
	admin := e.org(t, "runnowe2e")
	bot := e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "daily digest", Assignee: "Acme Bot", Repeat: "daily", Due: "2999-01-01"})[0]
	d := waitTriage(t, e, admin, task.ID)
	count := func() int {
		d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		return len(d.Comments)
	}
	before := count()
	if _, err := e.s.RunAutomation(e.ctx, admin, core.TaskIDIn{ID: task.ID}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200 && count() == before; i++ {
		time.Sleep(25 * time.Millisecond)
	}
	if n := len(d.Comments); n != before+1 || d.Comments[n-1].Author.ID != bot.Member.ID || strings.TrimSpace(d.Comments[n-1].Body) == "" {
		t.Fatalf("the agent didn't run it: %+v", d.Comments)
	}
	// It lets go once its reply is posted.
	for i := 0; i < 200 && d.Task.ClaimedBy != nil; i++ {
		time.Sleep(25 * time.Millisecond)
		count()
	}
	if d.Task.ClaimedBy != nil {
		t.Fatalf("still claimed: %+v", d.Task.ClaimedBy)
	}
}
