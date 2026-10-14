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
