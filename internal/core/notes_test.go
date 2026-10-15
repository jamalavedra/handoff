package core_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/joalavedra/handoff/internal/core"
)

func texts(ts []core.TodoOut) []string {
	out := []string{}
	for _, td := range ts {
		out = append(out, td.Text)
	}
	return out
}

func TestTodos(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "todos")
	task := e.create(t, admin, core.NewTask{Title: "Buy Halloween costume", Body: "Get it sorted early.", Todos: []string{"Check what's trending", "  "}})[0]
	if task.TodoProgress == nil || task.TodoProgress.Total != 1 || task.TodoProgress.Done != 0 {
		t.Fatalf("progress on create: %+v", task.TodoProgress)
	}
	res, err := e.s.AddTodos(e.ctx, admin, core.AddTodosIn{Task: task.ID, Items: []string{"Pick a direction", "Order:\n by Oct 24", "Try it on"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Check what's trending", "Pick a direction", "Order: by Oct 24", "Try it on"}; !slices.Equal(texts(res.Todos), want) {
		t.Fatalf("todos: %v", texts(res.Todos))
	}
	first := res.Todos[0].ID

	// An agent ticks a step with its one-line result.
	bot := e.agent(t, admin, "Helper", nil)
	res, err = e.s.UpdateTodo(e.ctx, bot, core.UpdateTodoIn{ID: first, Done: ptr(true), Result: ptr("6 ideas (Dolly,\nGhost Face…)")})
	if err != nil {
		t.Fatal(err)
	}
	if td := res.Todos[0]; !td.Done || td.Result != "6 ideas (Dolly, Ghost Face…)" || td.DoneBy == nil || td.DoneBy.ID != bot.Member.ID || td.DoneAt == "" {
		t.Fatalf("ticked: %+v", td)
	}
	// Moving and renaming.
	if res, err = e.s.UpdateTodo(e.ctx, admin, core.UpdateTodoIn{ID: res.Todos[3].ID, Position: ptr(0), Text: ptr("Try the old one on")}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"Try the old one on", "Check what's trending", "Pick a direction", "Order: by Oct 24"}; !slices.Equal(texts(res.Todos), want) {
		t.Fatalf("moved: %v", texts(res.Todos))
	}
	for i, td := range res.Todos {
		if td.Position != i {
			t.Fatalf("positions: %+v", res.Todos)
		}
	}
	if _, err := e.s.UpdateTodo(e.ctx, admin, core.UpdateTodoIn{ID: first, Text: ptr(" ")}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("empty text: %v", err)
	}
	// Unticking clears who did it.
	if res, err = e.s.UpdateTodo(e.ctx, admin, core.UpdateTodoIn{ID: first, Done: ptr(false)}); err != nil || res.Todos[1].Done || res.Todos[1].DoneBy != nil {
		t.Fatalf("unticked: %+v %v", res.Todos, err)
	}
	if res, err = e.s.RemoveTodo(e.ctx, admin, core.TodoIDIn{ID: res.Todos[0].ID}); err != nil || len(res.Todos) != 3 {
		t.Fatalf("removed: %+v %v", res.Todos, err)
	}
	if _, err := e.s.UpdateTodo(e.ctx, admin, core.UpdateTodoIn{ID: first, Done: ptr(true)}); err != nil {
		t.Fatal(err)
	}

	// get_task has them in order, lists have the progress, and the history says what happened.
	d, err := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(texts(d.Task.Todos), []string{"Check what's trending", "Pick a direction", "Order: by Oct 24"}) || d.Task.Body != "Get it sorted early." {
		t.Fatalf("task: %q %v", d.Task.Body, texts(d.Task.Todos))
	}
	kinds := map[string]int{}
	for _, ev := range d.Events {
		kinds[ev.Kind]++
	}
	if kinds["todos_added"] != 1 || kinds["todo_updated"] != 4 || kinds["todo_removed"] != 1 {
		t.Fatalf("events: %v", kinds)
	}
	list, err := e.s.SearchTasks(e.ctx, admin, core.SearchTasksIn{Query: "Halloween"})
	if err != nil || len(list.Tasks) != 1 || list.Tasks[0].TodoProgress == nil || *list.Tasks[0].TodoProgress != (core.TodoProgress{Done: 1, Total: 3}) || list.Tasks[0].Todos != nil {
		t.Fatalf("list: %+v %v", list.Tasks, err)
	}

	// Permissions: read-only agents can't touch them, other workspaces can't see them.
	reader := e.agent(t, admin, "Reader", &core.Permissions{Projects: []string{"*"}, Actions: []string{core.ActRead}})
	if _, err := e.s.AddTodos(e.ctx, reader, core.AddTodosIn{Task: task.ID, Items: []string{"x"}}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("reader added: %v", err)
	}
	if _, err := e.s.UpdateTodo(e.ctx, reader, core.UpdateTodoIn{ID: first, Done: ptr(false)}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("reader ticked: %v", err)
	}
	other := e.org(t, "todos2")
	if _, err := e.s.UpdateTodo(e.ctx, other, core.UpdateTodoIn{ID: first, Done: ptr(false)}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("other org ticked: %v", err)
	}
	if _, err := e.s.AddTodos(e.ctx, other, core.AddTodosIn{Task: task.ID, Items: []string{"x"}}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("other org added: %v", err)
	}
	// Nor can someone who can't see the task's project.
	board, err := e.s.CreateProject(e.ctx, admin, core.CreateProjectIn{Name: "Board", Visibility: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateTask(e.ctx, admin, core.UpdateTaskIn{ID: task.ID, Project: board.ID}); err != nil {
		t.Fatal(err)
	}
	narrow := e.agent(t, admin, "Narrow", &core.Permissions{Projects: []string{}, Actions: core.AllActions})
	if _, err := e.s.RemoveTodo(e.ctx, narrow, core.TodoIDIn{ID: first}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("agent outside the project removed: %v", err)
	}
}

func TestRepeatingTaskKeepsItsTodos(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "todorep")
	task := e.create(t, admin, core.NewTask{Title: "Water plants", Repeat: "weekly", Todos: []string{"Kitchen", "Balcony"}})[0]
	d, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	if _, err := e.s.UpdateTodo(e.ctx, admin, core.UpdateTodoIn{ID: d.Task.Todos[0].ID, Done: ptr(true), Result: ptr("done")}); err != nil {
		t.Fatal(err)
	}
	out, err := e.s.CloseTask(e.ctx, admin, core.CloseTaskIn{ID: task.ID})
	if err != nil || out.Next == nil {
		t.Fatalf("close: %+v %v", out, err)
	}
	next, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: out.Next.ID})
	if !slices.Equal(texts(next.Task.Todos), []string{"Kitchen", "Balcony"}) || next.Task.Todos[0].Done || next.Task.Todos[0].Result != "" {
		t.Fatalf("next occurrence todos: %+v", next.Task.Todos)
	}
}

func TestNotes(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "notes")
	board, err := e.s.CreateProject(e.ctx, admin, core.CreateProjectIn{Name: "Board", Visibility: "private"})
	if err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "Halloween costume", Project: "Board"})[0]
	inbox := e.create(t, admin, core.NewTask{Title: "Elsewhere"})[0]

	// A task's note goes in the task's project and links back to it.
	n, err := e.s.CreateNote(e.ctx, admin, core.CreateNoteIn{Task: task.ID, Title: "Halloween costume — research", Body: "## Ideas\n- Dolly"})
	if err != nil {
		t.Fatal(err)
	}
	if n.Note.ProjectID != board.ID || n.Note.Project != "Board" || n.Note.Task == nil || n.Note.Task.ID != task.ID || !strings.HasPrefix(n.Note.ID, "notes-n") ||
		!strings.HasSuffix(n.Note.URL, "/notes/"+n.Note.ID) || n.Note.CreatedBy.ID != admin.Member.ID {
		t.Fatalf("note: %+v", n.Note)
	}
	if _, err := e.s.CreateNote(e.ctx, admin, core.CreateNoteIn{Task: task.ID, Project: inbox.ProjectID, Title: "x"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("task's note in another project: %v", err)
	}
	if _, err := e.s.CreateNote(e.ctx, admin, core.CreateNoteIn{Title: "x"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("note without a project: %v", err)
	}
	if _, err := e.s.CreateNote(e.ctx, admin, core.CreateNoteIn{Project: "Board", Title: " "}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("note without a title: %v", err)
	}
	loose, err := e.s.CreateNote(e.ctx, admin, core.CreateNoteIn{Project: "Inbox", Title: "Team offsite ideas", Body: "Lisbon?"})
	if err != nil || loose.Note.Task != nil {
		t.Fatalf("loose note: %+v %v", loose.Note, err)
	}

	// Rewriting keeps the previous version in the note's history, for "Restore previous version".
	up, err := e.s.UpdateNote(e.ctx, admin, core.UpdateNoteIn{ID: n.Note.ID, Body: ptr("## Ideas\n- Dolly\n- Ghost Face"), Title: ptr("Costume — research")})
	if err != nil || up.Note.Title != "Costume — research" || up.Note.Body != "## Ideas\n- Dolly\n- Ghost Face" {
		t.Fatalf("update: %+v %v", up.Note, err)
	}
	if up, err = e.s.UpdateNote(e.ctx, admin, core.UpdateNoteIn{ID: n.Note.ID, Body: ptr("Pick by Friday."), Mode: "append"}); err != nil || !strings.HasSuffix(up.Note.Body, "Ghost Face\n\nPick by Friday.") {
		t.Fatalf("append: %+v %v", up.Note, err)
	}
	g, err := e.s.GetNote(e.ctx, admin, core.NoteIDIn{ID: n.Note.ID})
	if err != nil {
		t.Fatal(err)
	}
	var prev []any
	for _, ev := range g.Events {
		if ev.Kind == "note_updated" {
			prev = append(prev, ev.Data["previous_body"])
		}
	}
	if len(g.Events) != 3 || g.Events[0].Kind != "note_created" || !slices.Equal(prev, []any{"## Ideas\n- Dolly", "## Ideas\n- Dolly\n- Ghost Face"}) {
		t.Fatalf("history: %+v", g.Events)
	}
	// The task lists it, and its history shows the note's changes.
	d, err := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	if err != nil || len(d.Notes) != 1 || d.Notes[0].ID != n.Note.ID || d.Notes[0].Body != "" {
		t.Fatalf("task notes: %+v %v", d.Notes, err)
	}
	if !slices.ContainsFunc(d.Events, func(ev core.EventOut) bool { return ev.Kind == "note_updated" }) {
		t.Fatalf("task history: %+v", d.Events)
	}
	l, err := e.s.ListNotes(e.ctx, admin, core.ListNotesIn{Project: "Board"})
	if err != nil || len(l.Notes) != 1 || l.Notes[0].ID != n.Note.ID {
		t.Fatalf("board notes: %+v %v", l.Notes, err)
	}
	if l, err = e.s.ListNotes(e.ctx, admin, core.ListNotesIn{}); err != nil || len(l.Notes) != 2 || l.Notes[0].ID != n.Note.ID {
		t.Fatalf("all notes, newest first: %+v %v", l.Notes, err)
	}
	if l, err = e.s.ListNotes(e.ctx, admin, core.ListNotesIn{Query: "lisbon"}); err != nil || len(l.Notes) != 1 || l.Notes[0].ID != loose.Note.ID {
		t.Fatalf("search notes: %+v %v", l.Notes, err)
	}

	// Someone outside the private project sees nothing of it: not the note, its list, or its history.
	if _, err := e.s.Invite(e.ctx, admin, core.InviteIn{Email: "bob@notes.test"}); err != nil {
		t.Fatal(err)
	}
	bobM, err := e.s.MemberForLogin(e.ctx, admin.Org, "bob@notes.test", "Bob")
	if err != nil {
		t.Fatal(err)
	}
	bob, _ := e.s.Principal(e.ctx, admin.Org, bobM.ID, "web")
	if _, err := e.s.GetNote(e.ctx, bob, core.NoteIDIn{ID: n.Note.ID}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("bob read the board's note: %v", err)
	}
	if _, err := e.s.UpdateNote(e.ctx, bob, core.UpdateNoteIn{ID: n.Note.ID, Body: ptr("x")}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("bob changed the board's note: %v", err)
	}
	if _, err := e.s.CreateNote(e.ctx, bob, core.CreateNoteIn{Project: board.ID, Title: "planted"}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("bob wrote into the board: %v", err)
	}
	if l, err = e.s.ListNotes(e.ctx, bob, core.ListNotesIn{}); err != nil || len(l.Notes) != 1 || l.Notes[0].ID != loose.Note.ID {
		t.Fatalf("bob's notes: %+v %v", l.Notes, err)
	}
	act, err := e.s.Activity(e.ctx, bob, core.ActivityIn{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range act.Events {
		if ev.Data["note_id"] == n.Note.ID {
			t.Fatalf("bob saw the board's note in activity: %+v", ev)
		}
	}
	if !slices.ContainsFunc(act.Events, func(ev core.EventOut) bool { return ev.Data["note_id"] == loose.Note.ID }) {
		t.Fatalf("bob doesn't see the Inbox note's activity: %+v", act.Events)
	}

	// Agents: a task's note needs update; a note of its own needs create.
	updater := e.agent(t, admin, "Updater", &core.Permissions{Projects: []string{"*"}, Actions: []string{core.ActRead, core.ActUpdate}})
	if _, err := e.s.CreateNote(e.ctx, updater, core.CreateNoteIn{Task: inbox.ID, Title: "Elsewhere — notes"}); err != nil {
		t.Fatalf("updater wrote a task's note: %v", err)
	}
	if _, err := e.s.CreateNote(e.ctx, updater, core.CreateNoteIn{Project: "Inbox", Title: "loose"}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("updater created a loose note: %v", err)
	}
	reader := e.agent(t, admin, "Reader", &core.Permissions{Projects: []string{"*"}, Actions: []string{core.ActRead}})
	if _, err := e.s.UpdateNote(e.ctx, reader, core.UpdateNoteIn{ID: loose.Note.ID, Body: ptr("x")}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("reader changed a note: %v", err)
	}
	// Agents whose deletes need approval can't trash notes on their own.
	careful := e.agent(t, admin, "Careful", nil)
	if _, err := e.s.TrashNote(e.ctx, careful, core.NoteIDIn{ID: loose.Note.ID}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("agent trashed without approval: %v", err)
	}

	// Trash and restore.
	if _, err := e.s.TrashNote(e.ctx, admin, core.NoteIDIn{ID: loose.Note.ID}); err != nil {
		t.Fatal(err)
	}
	if l, _ = e.s.ListNotes(e.ctx, bob, core.ListNotesIn{}); len(l.Notes) != 1 || l.Notes[0].ID == loose.Note.ID {
		t.Fatalf("trashed note listed: %+v", l.Notes)
	}
	if l, _ = e.s.ListNotes(e.ctx, admin, core.ListNotesIn{Trashed: true}); len(l.Notes) != 1 {
		t.Fatalf("trash: %+v", l.Notes)
	}
	if _, err := e.s.UpdateNote(e.ctx, admin, core.UpdateNoteIn{ID: loose.Note.ID, Body: ptr("x")}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("updated a trashed note: %v", err)
	}
	if r, err := e.s.RestoreNote(e.ctx, admin, core.NoteIDIn{ID: loose.Note.ID}); err != nil || r.Note.Body != "Lisbon?" {
		t.Fatalf("restore: %+v %v", r, err)
	}
	// Another workspace can't reach it at all.
	other := e.org(t, "notes2")
	if _, err := e.s.GetNote(e.ctx, other, core.NoteIDIn{ID: loose.Note.ID}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("other org read a note: %v", err)
	}
}

func TestChecklistConversion(t *testing.T) {
	e := setup(t)
	type todo struct {
		Text   string `json:"text"`
		Done   bool   `json:"done"`
		Result string `json:"result"`
	}
	split := func(body string) ([]todo, string) {
		t.Helper()
		var raw []byte
		var rest string
		if err := e.s.DB.Pool.QueryRow(e.ctx, `SELECT todos, rest FROM checklist_split($1)`, body).Scan(&raw, &rest); err != nil {
			t.Fatal(err)
		}
		var out []todo
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out, rest
	}
	todos, rest := split("Get it sorted early.\n\n## Plan\n\n- [x] Check what's trending\n  - 6 ideas\n  - Dolly, Ghost Face\n* [ ] Pick a direction\n1. [X] Order: by Oct 24\n- [ ]\n\nKeep the receipt.\n\n```\n- [ ] not a todo\n```")
	want := []todo{{"Check what's trending", true, "6 ideas; Dolly, Ghost Face"}, {"Pick a direction", false, ""}, {"Order: by Oct 24", true, ""}}
	if !slices.Equal(todos, want) {
		t.Fatalf("todos: %+v", todos)
	}
	if rest != "Get it sorted early.\n\n## Plan\n\nKeep the receipt.\n\n```\n- [ ] not a todo\n```" {
		t.Fatalf("rest: %q", rest)
	}
	if todos, rest = split("Just text\n- a plain list"); len(todos) != 0 || rest != "Just text\n- a plain list" {
		t.Fatalf("no checklist: %+v %q", todos, rest)
	}
}

func TestAssistantWorksThroughTodos(t *testing.T) {
	srv, reqs := scriptedModel(t,
		calls(toolCall("c1", "get_task", map[string]any{"id": "TASK"})),
		calls(
			toolCall("c2", "update_todo", map[string]any{"id": "TODO", "done": true, "result": "6 ideas (Dolly, Ghost Face…)"}),
			toolCall("c3", "create_note", map[string]any{"task": "TASK", "title": "Halloween costume — research", "body": "## Ideas\n- Dolly\n- Ghost Face"}),
		),
		say("Ticked the first step; the ideas are in the note."),
		calls(toolCall("c4", "update_note", map[string]any{"id": "NOTE", "body": "## Ideas\n- Dolly"})),
		say("Narrowed it down."),
	)
	e, admin, bot := assistantEnv(t, srv, "assttodo")
	task := e.create(t, admin, core.NewTask{Title: "Buy Halloween costume", Todos: []string{"Check what's trending", "Pick a direction"}})[0]
	d := waitTriage(t, e, admin, task.ID) // nothing else calls the model from here on
	swap := map[string]string{"TASK": task.ID, "TODO": d.Task.Todos[0].ID}
	e.s.OutboundHTTP = &http.Client{Transport: rewrite{swap, srv.Client().Transport}}
	in := ask("go through the todos")
	in.Task = task.ID
	out, err := e.s.AssistantChat(e.ctx, admin, in)
	if err != nil || out.Error != "" {
		t.Fatalf("chat: %+v %v", out, err)
	}
	// The note's id is only known once it's written; the next turn rewrites it.
	l, err := e.s.ListNotes(e.ctx, admin, core.ListNotesIn{Task: task.ID})
	if err != nil || len(l.Notes) != 1 {
		t.Fatalf("notes: %+v %v", l.Notes, err)
	}
	swap["NOTE"] = l.Notes[0].ID
	in.Messages = append(in.Messages, core.AssistantMessage{Role: "assistant", Content: out.Reply}, core.AssistantMessage{Role: "user", Content: "only Dolly"})
	in.Turn = "second"
	out2, err := e.s.AssistantChat(e.ctx, admin, in)
	if err != nil || out2.Error != "" || len(out2.Steps) != 1 {
		t.Fatalf("second chat: %+v %v", out2, err)
	}
	out.Steps = append(out.Steps, out2.Steps...)
	if res := toolResult(t, reqs(), "c1"); !strings.Contains(res, `"todos":[{"id":"`+d.Task.Todos[0].ID+`","text":"Check what's trending","done":false}`) {
		t.Fatalf("get_task result: %s", res)
	}
	d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	if td := d.Task.Todos[0]; !td.Done || td.Result != "6 ideas (Dolly, Ghost Face…)" || td.DoneBy.ID != bot.Member.ID {
		t.Fatalf("todo: %+v", td)
	}
	if len(d.Notes) != 1 || d.Notes[0].Title != "Halloween costume — research" || d.Notes[0].CreatedBy.ID != bot.Member.ID || d.Notes[0].OnBehalfOf.ID != admin.Member.ID {
		t.Fatalf("notes: %+v", d.Notes)
	}
	g, _ := e.s.GetNote(e.ctx, admin, core.NoteIDIn{ID: d.Notes[0].ID})
	if g.Note.Body != "## Ideas\n- Dolly" || g.Events[len(g.Events)-1].Data["previous_body"] != "## Ideas\n- Dolly\n- Ghost Face" {
		t.Fatalf("note: %q %+v", g.Note.Body, g.Events)
	}
	if want := []string{"Read “Buy Halloween costume”", "Ticked “Check what's trending” → 6 ideas (Dolly, Ghost Face…)", "Wrote the note “Halloween costume — research”", "Updated the note “Halloween costume — research”"}; len(out.Steps) != 4 ||
		!slices.Equal([]string{out.Steps[0].Summary, out.Steps[1].Summary, out.Steps[2].Summary, out.Steps[3].Summary}, want) {
		t.Fatalf("steps: %+v", out.Steps)
	}
	if sys := reqs()[0].Messages[0]["content"].(string); !strings.Contains(sys, "tick each one you finish with update_todo") {
		t.Fatal("system prompt doesn't say how to work through todos")
	}
}
