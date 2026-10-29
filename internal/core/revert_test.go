package core_test

import (
	"testing"

	"github.com/joalavedra/handoff/internal/core"
)

// lastEvent is the newest event of a kind on a task.
func (e *env) lastEvent(t *testing.T, p *core.Principal, id, kind string) core.EventOut {
	t.Helper()
	d, err := e.s.GetTask(e.ctx, p, core.TaskIDIn{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	for i := len(d.Events) - 1; i >= 0; i-- {
		if d.Events[i].Kind == kind {
			return d.Events[i]
		}
	}
	t.Fatalf("no %s event on %s", kind, id)
	return core.EventOut{}
}

func TestRevertChange(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	tk := e.create(t, acme, core.NewTask{Title: "Old title", Due: "2026-11-02"})[0]

	if _, err := e.s.UpdateTask(e.ctx, acme, core.UpdateTaskIn{ID: tk.ID, Title: ptr("New title"), Due: ptr("2026-11-05"), Priority: ptr(2)}); err != nil {
		t.Fatal(err)
	}
	ev := e.lastEvent(t, acme, tk.ID, "updated")
	if ev.Data["from_title"] != "Old title" || ev.Data["from_due"] != "2026-11-02" || ev.Data["due"] != "2026-11-05" {
		t.Fatalf("old values not recorded: %v", ev.Data)
	}
	if _, ok := ev.Data["from_priority"]; ok {
		t.Fatalf("unchanged priority recorded as changed: %v", ev.Data)
	}

	// A read-only agent can't revert.
	reader := e.agent(t, acme, "Reader", &core.Permissions{Projects: []string{"*"}, Actions: []string{core.ActRead}})
	if _, err := e.s.RevertChange(e.ctx, reader, core.RevertChangeIn{EventID: ev.ID}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("read-only agent reverted: %v", err)
	}

	res, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: ev.ID})
	if err != nil {
		t.Fatal(err)
	}
	if res.Task.Title != "Old title" || res.Task.Due != "2026-11-02" {
		t.Fatalf("not reverted: %+v", res.Task)
	}
	if rv := e.lastEvent(t, acme, tk.ID, "reverted"); rv.Data["event_id"] != float64(ev.ID) {
		t.Fatalf("reverted event: %v", rv.Data)
	}

	// After a newer change to the same field, the old change can't be reverted.
	if _, err := e.s.UpdateTask(e.ctx, acme, core.UpdateTaskIn{ID: tk.ID, Title: ptr("Newer")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: ev.ID}); !core.IsCode(err, core.ErrConflict) {
		t.Fatalf("reverted over a newer change: %v", err)
	}

	// Closing reverts to the status before; trashing reverts to restored, and that back again.
	if _, err := e.s.CloseTask(e.ctx, acme, core.CloseTaskIn{ID: tk.ID}); err != nil {
		t.Fatal(err)
	}
	if res, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: e.lastEvent(t, acme, tk.ID, "closed").ID}); err != nil || res.Task.Status != "open" {
		t.Fatalf("reopen: %+v %v", res.Task, err)
	}
	// Reverting the reopen closes it again.
	if res, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: e.lastEvent(t, acme, tk.ID, "updated").ID}); err != nil || res.Task.Status != "done" {
		t.Fatalf("redo close: %+v %v", res.Task, err)
	}
	if _, err := e.s.Trash(e.ctx, acme, core.TrashIn{ID: tk.ID}); err != nil {
		t.Fatal(err)
	}
	if res, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: e.lastEvent(t, acme, tk.ID, "trashed").ID}); err != nil || res.Task.DeletedAt != "" {
		t.Fatalf("untrash: %+v %v", res.Task, err)
	}

	// Todos: a tick reverts to unticked.
	td, err := e.s.AddTodos(e.ctx, acme, core.AddTodosIn{Task: tk.ID, Items: []string{"Step"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateTodo(e.ctx, acme, core.UpdateTodoIn{ID: td.Todos[0].ID, Done: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: e.lastEvent(t, acme, tk.ID, "todo_updated").ID}); err != nil {
		t.Fatal(err)
	}
	d, _ := e.s.GetTask(e.ctx, acme, core.TaskIDIn{ID: tk.ID})
	if d.Task.Todos[0].Done {
		t.Fatal("todo still ticked")
	}
}
