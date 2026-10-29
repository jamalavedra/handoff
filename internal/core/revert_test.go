package core_test

import (
	"fmt"
	"strings"
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

func TestRevertAgentsOwnChangesOnly(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	bot := e.agent(t, acme, "Bot", nil)
	tk := e.create(t, acme, core.NewTask{Title: "Draft"})[0]

	if _, err := e.s.UpdateTask(e.ctx, acme, core.UpdateTaskIn{ID: tk.ID, Title: ptr("By a person")}); err != nil {
		t.Fatal(err)
	}
	theirs := e.lastEvent(t, acme, tk.ID, "updated")
	if _, err := e.s.RevertChange(e.ctx, bot, core.RevertChangeIn{EventID: theirs.ID}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("agent reverted a person's change: %v", err)
	}
	if _, err := e.s.UpdateTask(e.ctx, bot, core.UpdateTaskIn{ID: tk.ID, Title: ptr("By the bot")}); err != nil {
		t.Fatal(err)
	}
	if res, err := e.s.RevertChange(e.ctx, bot, core.RevertChangeIn{EventID: e.lastEvent(t, acme, tk.ID, "updated").ID}); err != nil || res.Task.Title != "By a person" {
		t.Fatalf("agent couldn't revert its own change: %+v %v", res.Task, err)
	}
	// A person may revert an agent's change (here the agent's revert, which redoes the rename).
	if res, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: e.lastEvent(t, acme, tk.ID, "updated").ID}); err != nil || res.Task.Title != "By the bot" {
		t.Fatalf("person couldn't revert the agent's change: %+v %v", res.Task, err)
	}
}

func TestRevertTextTodosAndNotes(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	tk := e.create(t, acme, core.NewTask{Title: "T", Body: "First version"})[0]

	// Text: a replace and an append both revert to the version before.
	if _, err := e.s.UpdateTask(e.ctx, acme, core.UpdateTaskIn{ID: tk.ID, Body: ptr("Second"), BodyMode: "replace", Confirm: true}); err != nil {
		t.Fatal(err)
	}
	replaced := e.lastEvent(t, acme, tk.ID, "updated")
	if _, err := e.s.UpdateTask(e.ctx, acme, core.UpdateTaskIn{ID: tk.ID, Body: ptr("more")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: replaced.ID}); !core.IsCode(err, core.ErrConflict) {
		t.Fatalf("reverted text over a newer change: %v", err)
	}
	if res, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: e.lastEvent(t, acme, tk.ID, "updated").ID}); err != nil || res.Task.Body != "Second" {
		t.Fatalf("append not reverted: %+v %v", res.Task, err)
	}
	if res, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: replaced.ID}); err != nil || res.Task.Body != "First version" {
		t.Fatalf("replace not reverted: %+v %v", res.Task, err)
	}

	// Todos: a move and a removal revert.
	td, err := e.s.AddTodos(e.ctx, acme, core.AddTodosIn{Task: tk.ID, Items: []string{"A", "B", "C"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateTodo(e.ctx, acme, core.UpdateTodoIn{ID: td.Todos[0].ID, Position: ptr(2)}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: e.lastEvent(t, acme, tk.ID, "todo_updated").ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateTodo(e.ctx, acme, core.UpdateTodoIn{ID: td.Todos[1].ID, Done: ptr(true), Result: ptr("done it")}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.RemoveTodo(e.ctx, acme, core.TodoIDIn{ID: td.Todos[1].ID}); err != nil {
		t.Fatal(err)
	}
	removed := e.lastEvent(t, acme, tk.ID, "todo_removed")
	if _, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: removed.ID}); err != nil {
		t.Fatal(err)
	}
	d, _ := e.s.GetTask(e.ctx, acme, core.TaskIDIn{ID: tk.ID})
	var got []string
	for _, x := range d.Task.Todos {
		got = append(got, x.Text)
	}
	if strings.Join(got, ",") != "A,B,C" || !d.Task.Todos[1].Done || d.Task.Todos[1].Result != "done it" || d.Task.Todos[1].ID != td.Todos[1].ID {
		t.Fatalf("todos not restored: %+v", d.Task.Todos)
	}
	if _, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: removed.ID}); !core.IsCode(err, core.ErrConflict) {
		t.Fatalf("restored a todo twice: %v", err)
	}

	// Notes: title and text.
	n, err := e.s.CreateNote(e.ctx, acme, core.CreateNoteIn{Project: core.DefaultProject, Title: "Plan", Body: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateNote(e.ctx, acme, core.UpdateNoteIn{ID: n.Note.ID, Title: ptr("Plan B"), Body: ptr("v2")}); err != nil {
		t.Fatal(err)
	}
	g, err := e.s.GetNote(e.ctx, acme, core.NoteIDIn{ID: n.Note.ID})
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: g.Events[len(g.Events)-1].ID})
	if err != nil || res.Note.Title != "Plan" || res.Note.Body != "v1" {
		t.Fatalf("note not reverted: %+v %v", res.Note, err)
	}
}

func TestRevertCloseOfRepeatingTask(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	tk := e.create(t, acme, core.NewTask{Title: "Water plants", Repeat: "weekly", Due: "2099-01-02"})[0]

	// An untouched next occurrence goes to the trash when the close is reverted.
	out, err := e.s.CloseTask(e.ctx, acme, core.CloseTaskIn{ID: tk.ID})
	if err != nil || out.Next == nil || out.EventID == 0 {
		t.Fatalf("close: %+v %v", out, err)
	}
	res, err := e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: out.EventID})
	if err != nil || res.Task.Status != "open" || !strings.Contains(res.Message, "trash") {
		t.Fatalf("revert close: %+v %v", res, err)
	}
	if d, err := e.s.GetTask(e.ctx, acme, core.TaskIDIn{ID: out.Next.ID}); err != nil || d.Task.DeletedAt == "" {
		t.Fatalf("next occurrence not trashed: %v", err)
	}

	// One someone already changed stays.
	other := e.create(t, acme, core.NewTask{Title: "Pay rent", Repeat: "monthly", Due: "2099-01-01"})[0]
	out, err = e.s.CloseTask(e.ctx, acme, core.CloseTaskIn{ID: other.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateTask(e.ctx, acme, core.UpdateTaskIn{ID: out.Next.ID, Priority: ptr(0)}); err != nil {
		t.Fatal(err)
	}
	res, err = e.s.RevertChange(e.ctx, acme, core.RevertChangeIn{EventID: out.EventID})
	if err != nil || !strings.Contains(res.Message, "stays") {
		t.Fatalf("revert close: %+v %v", res, err)
	}
	if d, err := e.s.GetTask(e.ctx, acme, core.TaskIDIn{ID: out.Next.ID}); err != nil || d.Task.DeletedAt != "" {
		t.Fatalf("changed next occurrence was trashed: %v", err)
	}
}

func TestTaskShowsNewestEvents(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	tk := e.create(t, acme, core.NewTask{Title: "Busy"})[0]
	for i := range 205 {
		if _, err := e.s.UpdateTask(e.ctx, acme, core.UpdateTaskIn{ID: tk.ID, Title: ptr(fmt.Sprint("v", i))}); err != nil {
			t.Fatal(err)
		}
	}
	d, err := e.s.GetTask(e.ctx, acme, core.TaskIDIn{ID: tk.ID})
	if err != nil {
		t.Fatal(err)
	}
	evs := d.Events
	if len(evs) != 200 || evs[len(evs)-1].Data["title"] != "v204" || evs[0].ID > evs[1].ID {
		t.Fatalf("events: %d, last %v", len(evs), evs[len(evs)-1].Data)
	}
}
