package core_test

import (
	"slices"
	"testing"

	"github.com/joalavedra/handoff/internal/core"
)

func TestRepeatingTask(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "rep")
	one := 1
	task := e.create(t, admin, core.NewTask{Title: "Send the weekly report", Repeat: "weekly", Due: "2099-01-02", Priority: &one, Labels: []string{"ops"}, Assignee: "me"})[0]
	if task.Repeat != "weekly" {
		t.Fatalf("repeat = %q", task.Repeat)
	}
	out, err := e.s.CloseTask(e.ctx, admin, core.CloseTaskIn{ID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	n := out.Next
	if n == nil || n.ID == task.ID || n.Title != task.Title || n.Due != "2099-01-09" || n.Repeat != "weekly" || n.Priority != 1 ||
		!slices.Equal(n.Labels, []string{"ops"}) || n.Assignee == nil || n.Assignee.ID != admin.Member.ID || n.Status != "open" {
		t.Fatalf("next occurrence: %+v", n)
	}
	// Closing it again (a retry) doesn't make another one.
	again, err := e.s.CloseTask(e.ctx, admin, core.CloseTaskIn{ID: task.ID})
	if err != nil || again.Next != nil {
		t.Fatalf("retried close created another occurrence: %+v %v", again.Next, err)
	}
	// Reopened and closed again: still one next task.
	open := "open"
	if _, err := e.s.UpdateTask(e.ctx, admin, core.UpdateTaskIn{ID: task.ID, Status: open}); err != nil {
		t.Fatal(err)
	}
	if again, err = e.s.CloseTask(e.ctx, admin, core.CloseTaskIn{ID: task.ID}); err != nil || again.Next != nil {
		t.Fatalf("reopen and close created another occurrence: %+v %v", again.Next, err)
	}
	// An agent that may only close still continues the series, as the series' creator.
	closer := e.agent(t, admin, "Closer", &core.Permissions{Projects: []string{"*"}, Actions: []string{core.ActRead, core.ActClose}})
	byAgent, err := e.s.CloseTask(e.ctx, closer, core.CloseTaskIn{ID: n.ID})
	if err != nil || byAgent.Next == nil || byAgent.Next.CreatedBy.ID != admin.Member.ID {
		t.Fatalf("close-only agent: next %+v err %v", byAgent.Next, err)
	}
	n = byAgent.Next
	// Cancelling ends the series; turning repeat off means done is just done.
	out, err = e.s.CloseTask(e.ctx, admin, core.CloseTaskIn{ID: n.ID, Status: "cancelled"})
	if err != nil || out.Next != nil {
		t.Fatalf("cancel created a next one: %+v %v", out.Next, err)
	}
	other := e.create(t, admin, core.NewTask{Title: "Water plants", Repeat: "daily"})[0]
	off := ""
	if _, err := e.s.UpdateTask(e.ctx, admin, core.UpdateTaskIn{ID: other.ID, Repeat: &off}); err != nil {
		t.Fatal(err)
	}
	if out, err = e.s.CloseTask(e.ctx, admin, core.CloseTaskIn{ID: other.ID}); err != nil || out.Next != nil {
		t.Fatalf("non-repeating close created a next one: %+v %v", out.Next, err)
	}
	if _, err := e.s.CreateTasks(e.ctx, admin, core.CreateTasksIn{Tasks: []core.NewTask{{Title: "x", Repeat: "hourly"}}}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("bad repeat accepted: %v", err)
	}
}

func TestRepeatingTaskWithRevokedAssignee(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "reprev")
	bot := e.agent(t, admin, "Weekly bot", nil)
	task := e.create(t, admin, core.NewTask{Title: "Weekly digest", Repeat: "weekly", Assignee: "Weekly bot"})[0]
	if _, err := e.s.UpdateAgent(e.ctx, admin, core.UpdateAgentIn{ID: bot.Member.ID, Revoke: true}); err != nil {
		t.Fatal(err)
	}
	out, err := e.s.CloseTask(e.ctx, admin, core.CloseTaskIn{ID: task.ID})
	if err != nil || out.Task.Status != "done" || out.Next == nil || out.Next.Assignee != nil {
		t.Fatalf("close with revoked assignee: %+v next %+v err %v", out.Task.Status, out.Next, err)
	}
}
