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
