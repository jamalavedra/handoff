package core_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/core"
	"github.com/joalavedra/handoff/internal/testutil"
)

// fakeModel is an OpenAI-compatible endpoint that answers every request with reply.
// during, if set, runs while the "model" is thinking, to simulate concurrent edits.
func fakeModel(t *testing.T, status int, reply map[string]any, gotAuth *atomic.Value, during ...func()) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		gotAuth.Store(r.Header.Get("Authorization"))
		for _, f := range during {
			f()
		}
		if status != http.StatusOK {
			http.Error(w, `{"error":"bad key"}`, status)
			return
		}
		content, _ := json.Marshal(reply)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": "Here you go:\n" + string(content)}}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func triageEnv(t *testing.T, srv *httptest.Server) *env {
	t.Helper()
	base, _ := url.Parse("http://handoff.test")
	s := core.New(testutil.DB(t), &config.Config{BaseURL: base, Secret: []byte("test secret")})
	s.OutboundHTTP = srv.Client()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.RunTriage(ctx)
	return &env{s: s, ctx: context.Background()}
}

// waitTriage polls until the task leaves the pending state.
func waitTriage(t *testing.T, e *env, p *core.Principal, id string) core.TaskDetailOut {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		d, err := e.s.GetTask(e.ctx, p, core.TaskIDIn{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		if d.Task.Triage != "pending" {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s still pending", id)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestTriage(t *testing.T) {
	var auth atomic.Value
	srv := fakeModel(t, http.StatusOK, map[string]any{
		"title": "Edit the episode 14 intro", "due": "2026-10-16", "priority": 0, "project": "Podcast",
		"labels": []string{"audio"}, "assignee": "Acme Bot", "text": "Done when the intro is under 30s.", "todos": []string{"Trim the intro", " ", "Export: by Thursday"},
	}, &auth)
	e := triageEnv(t, srv)
	admin := e.org(t, "tri")
	bot := e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.CreateProject(e.ctx, admin, core.CreateProjectIn{Name: "Podcast", Visibility: "company"}); err != nil {
		t.Fatal(err)
	}

	// Only admins configure it, and the credential never comes back.
	member := e.agent(t, admin, "Helper", nil)
	if _, err := e.s.SetTriage(e.ctx, member, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("non-admin set triage: %v", err)
	}
	cfg, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1/", Model: "test-model", Credential: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || !cfg.HasCredential || cfg.Agent.ID != bot.Member.ID || cfg.BaseURL != srv.URL+"/v1" {
		t.Fatalf("settings: %+v", cfg)
	}
	if b, _ := json.Marshal(cfg); strings.Contains(string(b), "sk-test") {
		t.Fatal("credential leaked in settings output")
	}

	// A person's task is triaged; the priority they chose stays.
	three := 3
	task := e.create(t, admin, core.NewTask{Title: "can u pls edit the ep 14 intro by thursday", Priority: &three})[0]
	if task.Triage != "pending" {
		t.Fatalf("new task triage = %q, want pending", task.Triage)
	}
	d := waitTriage(t, e, admin, task.ID)
	if auth.Load() != "Bearer sk-test" {
		t.Fatalf("model got Authorization %q", auth.Load())
	}
	got := d.Task
	// The creator made it in the app, so it's theirs; triage leaves the owner alone.
	if got.Triage != "done" || got.Title != "Edit the episode 14 intro" || got.Due != "2026-10-16" || got.Priority != 3 ||
		got.Project != "Podcast" || !slices.Equal(got.Labels, []string{"audio"}) || got.Assignee == nil || got.Assignee.ID != admin.Member.ID {
		t.Fatalf("triaged task: %+v (error %q)", got, got.TriageError)
	}
	// A task is text and todos; the chat gets a one-line note.
	if got.Body != "Done when the intro is under 30s." || len(got.Todos) != 2 || got.Todos[0].Text != "Trim the intro" ||
		got.Todos[1].Text != "Export: by Thursday" || got.Todos[0].Done || got.TodoProgress == nil || got.TodoProgress.Total != 2 {
		t.Fatalf("text and todos: %q %+v %+v", got.Body, got.Todos, got.TodoProgress)
	}
	if len(d.Notes) != 0 {
		t.Fatalf("a note without work: %+v", d.Notes)
	}
	if len(d.Comments) != 1 || d.Comments[0].Author.ID != bot.Member.ID ||
		d.Comments[0].Body != "I tidied the title, set it due Fri, Oct 16, moved it to Podcast, labelled it “audio”, added a line on what it's about and broke it into 2 todos. Mention me here if you want changes." {
		t.Fatalf("triage note: %+v", d.Comments)
	}
	if !strings.HasPrefix(got.AgentNote, "I tidied the title") {
		t.Fatalf("agent note = %q", got.AgentNote)
	}
	if !slices.ContainsFunc(d.Events, func(ev core.EventOut) bool { return ev.Kind == "triaged" && ev.Actor.ID == bot.Member.ID }) {
		t.Fatalf("no triaged event by the agent: %+v", d.Events)
	}

	// Agents' own tasks are taken as written.
	own := e.create(t, bot, core.NewTask{Title: "agent task"})[0]
	if own.Triage != "" {
		t.Fatalf("agent-created task triage = %q", own.Triage)
	}

	// Switching it off leaves new tasks alone.
	if _, err := e.s.DisableTriage(e.ctx, admin, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if off := e.create(t, admin, core.NewTask{Title: "after off"})[0]; off.Triage != "" {
		t.Fatalf("triage after disable = %q", off.Triage)
	}
}

func TestTriageFailure(t *testing.T) {
	var auth atomic.Value
	srv := fakeModel(t, http.StatusUnauthorized, nil, &auth)
	e := triageEnv(t, srv)
	admin := e.org(t, "trifail")
	e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "bad"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "buy milk"})[0]
	d := waitTriage(t, e, admin, task.ID)
	if d.Task.Triage != "failed" || !strings.Contains(d.Task.TriageError, "401") || d.Task.Title != "buy milk" {
		t.Fatalf("failed triage: %+v", d.Task)
	}
}

func TestTriageKeepsConcurrentEdits(t *testing.T) {
	var auth atomic.Value
	var e *env
	var admin *core.Principal
	var id atomic.Value
	edit := func() {
		// The creator renames the task and picks a date while the model is thinking.
		for id.Load() == nil {
			time.Sleep(5 * time.Millisecond) // the worker can call before create returns the id
		}
		title, due := "Draft proposal for Paris", "2026-11-02"
		if _, err := e.s.UpdateTask(e.ctx, admin, core.UpdateTaskIn{ID: id.Load().(string), Title: &title, Due: &due}); err != nil {
			t.Error(err)
		}
	}
	srv := fakeModel(t, http.StatusOK, map[string]any{"title": "Write proposal", "due": "2026-10-20", "priority": 1, "plan": "Done when sent."}, &auth, edit)
	e = triageEnv(t, srv)
	admin = e.org(t, "triedit")
	e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "draft proposal"})[0]
	id.Store(task.ID)
	d := waitTriage(t, e, admin, task.ID)
	if d.Task.Title != "Draft proposal for Paris" || d.Task.Due != "2026-11-02" || d.Task.Priority != 1 || d.Task.Triage != "done" {
		t.Fatalf("concurrent edits lost or triage incomplete: %+v", d.Task)
	}
}

func TestTriageDisabledMidCall(t *testing.T) {
	var auth atomic.Value
	var e *env
	var admin *core.Principal
	off := func() {
		if _, err := e.s.DisableTriage(e.ctx, admin, struct{}{}); err != nil {
			t.Error(err)
		}
	}
	srv := fakeModel(t, http.StatusOK, map[string]any{"title": "Changed", "plan": "Done when changed."}, &auth, off)
	e = triageEnv(t, srv)
	admin = e.org(t, "trioff")
	e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "keep me"})[0]
	d := waitTriage(t, e, admin, task.ID)
	time.Sleep(200 * time.Millisecond) // let a stale worker try to write, if it would
	d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	if d.Task.Title != "keep me" || d.Task.Triage != "" || len(d.Comments) != 0 {
		t.Fatalf("triage applied after it was switched off: %+v, comments %d", d.Task, len(d.Comments))
	}
}

func TestTriageRefusesRedirects(t *testing.T) {
	var leaked atomic.Value
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(r.Header.Get("Authorization"))
	}))
	t.Cleanup(elsewhere.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/steal", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)
	base, _ := url.Parse("http://handoff.test")
	s := core.New(testutil.DB(t), &config.Config{BaseURL: base, Secret: []byte("x"), Dev: true})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.RunTriage(ctx)
	e := &env{s: s, ctx: context.Background()}
	admin := e.org(t, "triredir")
	e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: redirect.URL + "/v1", Model: "m", Credential: "secret-token"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "x"})[0]
	d := waitTriage(t, e, admin, task.ID)
	if d.Task.Triage != "failed" || leaked.Load() != nil {
		t.Fatalf("redirect followed: triage %q, other host saw %v", d.Task.Triage, leaked.Load())
	}
}

func TestTriageAgentNeedsAccess(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "triperm")
	e.agent(t, admin, "Writer", &core.Permissions{Projects: []string{"*"}, Actions: []string{core.ActUpdate}})
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Writer", BaseURL: "https://example.com/v1", Model: "m", Credential: "k"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("agent without read accepted: %v", err)
	}
	projects, err := e.s.ListProjects(e.ctx, admin, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	e.agent(t, admin, "Narrow", &core.Permissions{Projects: []string{projects.Projects[0].ID}, Actions: []string{core.ActRead, core.ActUpdate}})
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Narrow", BaseURL: "https://example.com/v1", Model: "m", Credential: "k"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("agent limited to some projects accepted: %v", err)
	}
}

func TestTriageTestCall(t *testing.T) {
	var auth atomic.Value
	srv := fakeModel(t, http.StatusOK, map[string]any{"title": "Send invoices to accounting", "due": "2026-10-16"}, &auth)
	e := triageEnv(t, srv)
	admin := e.org(t, "tritest")
	out, err := e.s.TestTriage(e.ctx, admin, core.TestTriageIn{BaseURL: srv.URL + "/v1", Model: "m", Credential: "typed-key"})
	if err != nil || !out.OK || out.Reply != "Send invoices to accounting · due 2026-10-16" || auth.Load() != "Bearer typed-key" {
		t.Fatalf("test call: %+v %v (auth %v)", out, err, auth.Load())
	}
	// No key typed and none saved yet.
	if _, err := e.s.TestTriage(e.ctx, admin, core.TestTriageIn{BaseURL: srv.URL + "/v1", Model: "m"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("without a key: %v", err)
	}
	// A saved key never goes to a different endpoint, when testing or saving.
	e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "saved-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.TestTriage(e.ctx, admin, core.TestTriageIn{BaseURL: "https://other.example/v1", Model: "m"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("saved key offered to another host on test: %v", err)
	}
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: "https://other.example/v1", Model: "m"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("saved key kept for another host on save: %v", err)
	}
	if out, err := e.s.TestTriage(e.ctx, admin, core.TestTriageIn{BaseURL: srv.URL + "/v1", Model: "m"}); err != nil || !out.OK || auth.Load() != "Bearer saved-key" {
		t.Fatalf("saved key on its own endpoint: %+v %v", out, err)
	}
	member := e.agent(t, admin, "Helper", nil)
	if _, err := e.s.TestTriage(e.ctx, member, core.TestTriageIn{BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("non-admin: %v", err)
	}
}

func TestTriageTestNeedsATitle(t *testing.T) {
	var auth atomic.Value
	srv := fakeModel(t, http.StatusOK, map[string]any{}, &auth)
	e := triageEnv(t, srv)
	admin := e.org(t, "triempty")
	out, err := e.s.TestTriage(e.ctx, admin, core.TestTriageIn{BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"})
	if err != nil || out.OK || out.Error == "" {
		t.Fatalf("empty answer passed the test: %+v %v", out, err)
	}
}

func TestTriageDoesTheWork(t *testing.T) {
	var auth atomic.Value
	srv := fakeModel(t, http.StatusOK, map[string]any{
		"title": "Compare Pixel 8 and Pixel 9", "labels": []string{"research"},
		"work":      "Here's how they compare.\n\n| | Pixel 8 | Pixel 9 |\n|---|---|---|\n| Chip | Tensor G3 | Tensor G4 |",
		"work_kind": "Comparison", "todos": []string{"Compare specs", "Pick one", "Order: by Friday"},
	}, &auth)
	e := triageEnv(t, srv)
	admin := e.org(t, "triwork")
	e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "hey can u compare pixel 8 vs pixel 9"})[0]
	d := waitTriage(t, e, admin, task.ID)
	// The work is a note in the task's project, linked from the task; the task stays text and todos.
	if d.Task.Body != "" || len(d.Task.Todos) != 3 || d.Task.Todos[2].Text != "Order: by Friday" {
		t.Fatalf("task: %q %+v", d.Task.Body, d.Task.Todos)
	}
	if len(d.Notes) != 1 || d.Notes[0].Title != "Compare Pixel 8 and Pixel 9 — comparison" || d.Notes[0].Task == nil || d.Notes[0].Task.ID != task.ID ||
		d.Notes[0].ProjectID != d.Task.ProjectID || d.Notes[0].CreatedBy.Name != "Acme Bot" {
		t.Fatalf("notes: %+v", d.Notes)
	}
	n, err := e.s.GetNote(e.ctx, admin, core.NoteIDIn{ID: d.Notes[0].ID})
	if err != nil || !strings.Contains(n.Note.Body, "| Chip | Tensor G3 | Tensor G4 |") {
		t.Fatalf("note body: %+v %v", n.Note, err)
	}
	if len(d.Comments) != 1 || !strings.Contains(d.Comments[0].Body, "did what I could: it's in the note “Compare Pixel 8 and Pixel 9 — comparison”") {
		t.Fatalf("triage comment: %+v", d.Comments)
	}
	if !slices.ContainsFunc(d.Events, func(ev core.EventOut) bool { return ev.Kind == "note_created" && ev.Data["note_id"] == n.Note.ID }) {
		t.Fatalf("no note event on the task: %+v", d.Events)
	}
}
