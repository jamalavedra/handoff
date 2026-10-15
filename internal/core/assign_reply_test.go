package core_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joalavedra/handoff/internal/core"
)

func TestNewTasksAreMine(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "mine")
	bot := e.agent(t, admin, "Bot", nil)
	if got := e.create(t, admin, core.NewTask{Title: "mine"})[0]; got.Assignee == nil || got.Assignee.ID != admin.Member.ID {
		t.Fatalf("web-created task assignee: %+v", got.Assignee)
	}
	if got := e.create(t, admin, core.NewTask{Title: "for the bot", Assignee: "Bot"})[0]; got.Assignee == nil || got.Assignee.ID != bot.Member.ID {
		t.Fatalf("explicit assignee: %+v", got.Assignee)
	}
	if got := e.create(t, bot, core.NewTask{Title: "agent's"})[0]; got.Assignee != nil {
		t.Fatalf("agent-created task got an assignee: %+v", got.Assignee)
	}
	api, err := e.s.Principal(e.ctx, admin.Org, admin.Member.ID, "api")
	if err != nil {
		t.Fatal(err)
	}
	if got := e.create(t, api, core.NewTask{Title: "via api"})[0]; got.Assignee != nil {
		t.Fatalf("api-created task got an assignee: %+v", got.Assignee)
	}
}

func TestAgentAnswersInTaskChat(t *testing.T) {
	var auth atomic.Value
	srv := fakeModel(t, http.StatusOK, map[string]any{"title": "Compare phones"}, &auth)
	e := triageEnv(t, srv)
	admin := e.org(t, "chatreply")
	bot := e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "compare phones"})[0]
	waitTriage(t, e, admin, task.ID)
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot which one should I buy?"}); err != nil {
		t.Fatal(err)
	}
	// Triage's note, my question, then the agent's answer.
	var d core.TaskDetailOut
	for i := 0; i < 200; i++ {
		d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		if len(d.Comments) >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(d.Comments) != 3 {
		t.Fatalf("no answer in the chat: %+v", d.Comments)
	}
	last := d.Comments[2]
	if last.Author.ID != bot.Member.ID || last.OnBehalfOf == nil || last.OnBehalfOf.ID != admin.Member.ID || strings.TrimSpace(last.Body) == "" {
		t.Fatalf("answer: %+v", last)
	}
	// Its own answer doesn't set off another one.
	time.Sleep(500 * time.Millisecond)
	if d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID}); len(d.Comments) != 3 {
		t.Fatalf("agent answered itself: %d comments", len(d.Comments))
	}
	// A message without the mention, on a task it doesn't own, gets no answer.
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "thanks, noted"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID}); len(d.Comments) != 4 {
		t.Fatalf("answered an unaddressed message: %d comments", len(d.Comments))
	}
}

func TestUnassignedOnPurpose(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "unassigned")
	if got := e.create(t, admin, core.NewTask{Title: "nobody's yet", Unassigned: true})[0]; got.Assignee != nil {
		t.Fatalf("explicitly unassigned task got %+v", got.Assignee)
	}
}

func TestChatReplyPerRequest(t *testing.T) {
	// Each question gets its own answer, even past 20 messages, and a request whose task the person
	// can no longer see is dropped without reaching the model.
	var calls atomic.Int32
	srv := fakeModel(t, http.StatusOK, map[string]any{"title": "x"}, new(atomic.Value), func() { calls.Add(1) })
	e := triageEnv(t, srv)
	admin := e.org(t, "chatmany")
	e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "long thread"})[0]
	waitTriage(t, e, admin, task.ID)
	for i := 0; i < 22; i++ {
		if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "note " + strings.Repeat("x", i)}); err != nil {
			t.Fatal(err)
		}
	}
	ask := func(n int) {
		t.Helper()
		before := calls.Load()
		if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot question " + strings.Repeat("?", n)}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 200 && calls.Load() == before; i++ {
			time.Sleep(25 * time.Millisecond)
		}
		if calls.Load() == before {
			t.Fatalf("question %d never reached the model (an old answer was reused?)", n)
		}
		time.Sleep(200 * time.Millisecond)
	}
	ask(1)
	ask(2)
}

func TestTriageQuestionsGoToChat(t *testing.T) {
	srv := fakeModel(t, http.StatusOK, map[string]any{
		"title": "Find restaurants above $30 in SF", "work": "## Italian\n- Delfina",
		"questions": []string{"Any neighbourhood?", "Lunch or dinner?"},
	}, new(atomic.Value))
	e := triageEnv(t, srv)
	admin := e.org(t, "triq")
	e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "find restaurants above 30 in sf"})[0]
	d := waitTriage(t, e, admin, task.ID)
	if d.Task.Body != "" {
		t.Fatalf("task text: %q", d.Task.Body)
	}
	if len(d.Comments) != 1 || !strings.Contains(d.Comments[0].Body, "- Any neighbourhood?\n- Lunch or dinner?") {
		t.Fatalf("questions not in the chat: %+v", d.Comments)
	}
}

func TestHandingATaskToTheAgent(t *testing.T) {
	// The "model" answers every call the same way; for a take-on that's a plain reply with no tool calls,
	// so the check is that the agent answers in the chat after being handed the task.
	srv := fakeModel(t, http.StatusOK, map[string]any{"title": "Draft the newsletter"}, new(atomic.Value))
	e := triageEnv(t, srv)
	admin := e.org(t, "takeon")
	bot := e.agent(t, admin, "Acme Bot", nil)
	other := e.agent(t, admin, "Other bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "draft the newsletter"})[0]
	waitTriage(t, e, admin, task.ID)
	count := func() int {
		d, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		return len(d.Comments)
	}
	before := count()
	// Handing it to an agent that isn't the workspace's own does nothing more.
	to := "Other bot"
	if _, err := e.s.UpdateTask(e.ctx, admin, core.UpdateTaskIn{ID: task.ID, Assignee: &to}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if count() != before {
		t.Fatal("a different agent was made to take it on")
	}
	_ = other
	to = "Acme Bot"
	if _, err := e.s.UpdateTask(e.ctx, admin, core.UpdateTaskIn{ID: task.ID, Assignee: &to}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200 && count() == before; i++ {
		time.Sleep(25 * time.Millisecond)
	}
	d, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	if len(d.Comments) != before+1 || d.Comments[len(d.Comments)-1].Author.ID != bot.Member.ID {
		t.Fatalf("the agent didn't take it on: %+v", d.Comments)
	}
	// It let go of the task when it finished (just after posting its reply).
	for i := 0; i < 200 && d.Task.ClaimedBy != nil; i++ {
		time.Sleep(25 * time.Millisecond)
		d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	}
	if d.Task.ClaimedBy != nil {
		t.Fatalf("still claimed: %+v", d.Task.ClaimedBy)
	}
}

func TestTakeOnRespectsOthers(t *testing.T) {
	srv := fakeModel(t, http.StatusOK, map[string]any{"title": "x"}, new(atomic.Value))
	e := triageEnv(t, srv)
	admin := e.org(t, "takeonguard")
	e.agent(t, admin, "Acme Bot", nil)
	worker := e.agent(t, admin, "Worker", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "busy task", Body: "keep me"})[0]
	waitTriage(t, e, admin, task.ID)
	// Someone else is working on it: the agent says it can't start, and changes nothing.
	if _, err := e.s.ClaimTask(e.ctx, worker, core.ClaimTaskIn{ID: task.ID}); err != nil {
		t.Fatal(err)
	}
	to := "Acme Bot"
	if _, err := e.s.UpdateTask(e.ctx, admin, core.UpdateTaskIn{ID: task.ID, Assignee: &to}); err != nil {
		t.Fatal(err)
	}
	var d core.TaskDetailOut
	for i := 0; i < 200; i++ {
		d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		if n := len(d.Comments); n > 0 && strings.HasPrefix(d.Comments[n-1].Body, "I can't start on this yet") {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if n := len(d.Comments); n == 0 || !strings.HasPrefix(d.Comments[n-1].Body, "I can't start on this yet") {
		t.Fatalf("no explanation: %+v", d.Comments)
	}
	if d.Task.ClaimedBy == nil || d.Task.ClaimedBy.ID != worker.Member.ID {
		t.Fatalf("the worker's claim was disturbed: %+v", d.Task.ClaimedBy)
	}
}
