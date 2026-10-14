package core_test

import (
	"slices"
	"testing"
	"time"

	"github.com/joalavedra/handoff/internal/core"
)

func TestAgentHandsBackForInput(t *testing.T) {
	model, _ := scriptedModel(t,
		calls(toolCall("c1", "ask_person", map[string]any{"options": []string{"Classic", "Funny", "  "}})),
		say("Which direction do you want: classic or funny?"),
		say("Great, classic it is. I'll shortlist some."),
	)
	e, admin, bot := assistantEnv(t, model, "askback")
	task := e.create(t, admin, core.NewTask{Title: "Buy Halloween costume"})[0]
	waitTriage(t, e, admin, task.ID)
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot help me pick a costume"}); err != nil {
		t.Fatal(err)
	}
	// The agent asks: the task moves to the person, waiting on them, and the question offers answers.
	var d core.TaskDetailOut
	for range 200 {
		d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		if n := len(d.Comments); n > 0 && d.Comments[n-1].Author.ID == bot.Member.ID && d.Task.WaitingOn != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	q := d.Comments[len(d.Comments)-1]
	if d.Task.WaitingOn == nil || d.Task.WaitingOn.ID != admin.Member.ID || d.Task.Assignee == nil || d.Task.Assignee.ID != admin.Member.ID {
		t.Fatalf("not handed back: assignee %+v, waiting on %+v", d.Task.Assignee, d.Task.WaitingOn)
	}
	if !slices.Equal(q.Options, []string{"Classic", "Funny"}) || q.Author.ID != bot.Member.ID {
		t.Fatalf("question: %+v", q)
	}

	// Answering hands it back to the agent, which carries on.
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "Classic"}); err != nil {
		t.Fatal(err)
	}
	d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	if d.Task.WaitingOn != nil || d.Task.Assignee == nil || d.Task.Assignee.ID != bot.Member.ID {
		t.Fatalf("answer didn't hand it back: assignee %+v, waiting on %+v", d.Task.Assignee, d.Task.WaitingOn)
	}
	for range 200 {
		d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		if n := len(d.Comments); d.Comments[n-1].Author.ID == bot.Member.ID && d.Comments[n-1].Body != q.Body {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if last := d.Comments[len(d.Comments)-1]; last.Author.ID != bot.Member.ID || last.Body == q.Body {
		t.Fatalf("agent didn't carry on: %+v", last)
	}
}

// A reply that ends with a question waits on the person even when the model didn't hand it back,
// and reassigning the task ends the wait.
func TestWaitEndsWhenReassigned(t *testing.T) {
	model, _ := scriptedModel(t, say("Happy to. Which size are you, **42 or 43?**"))
	e, admin, _ := assistantEnv(t, model, "askreassign")
	e.agent(t, admin, "Other Bot", nil)
	task := e.create(t, admin, core.NewTask{Title: "Order shoes"})[0]
	waitTriage(t, e, admin, task.ID)
	acme := "Acme Bot"
	if _, err := e.s.UpdateTask(e.ctx, admin, core.UpdateTaskIn{ID: task.ID, Assignee: &acme}); err != nil {
		t.Fatal(err)
	}
	var d core.TaskDetailOut
	for range 200 {
		if d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID}); d.Task.WaitingOn != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if d.Task.WaitingOn == nil || d.Task.WaitingOn.ID != admin.Member.ID || d.Task.Assignee.ID != admin.Member.ID {
		t.Fatalf("a question didn't hand it back: assignee %+v, waiting on %+v", d.Task.Assignee, d.Task.WaitingOn)
	}
	other := "Other Bot"
	if _, err := e.s.UpdateTask(e.ctx, admin, core.UpdateTaskIn{ID: task.ID, Assignee: &other}); err != nil {
		t.Fatal(err)
	}
	if d, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID}); d.Task.WaitingOn != nil {
		t.Fatalf("still waiting after reassigning: %+v", d.Task.WaitingOn)
	}
}
