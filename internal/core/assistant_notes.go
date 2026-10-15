package core

import (
	"context"
	"encoding/json"
	"fmt"
)

// The assistant's tools for a task's todos and a project's notes: it ticks steps off with a one-line
// result, reads notes as context, and writes one only when the person asks.

type aAddTodosIn struct {
	Task  string   `json:"task" jsonschema:"task id"`
	Items []string `json:"items" jsonschema:"1 to 10 steps in order, each a short action of at most 8 words, with a deadline after a colon when it matters"`
}

type aTodoIn struct {
	ID     string  `json:"id" jsonschema:"todo id, from get_task"`
	Done   *bool   `json:"done,omitempty" jsonschema:"tick (true) or untick (false)"`
	Result *string `json:"result,omitempty" jsonschema:"one line: what you found or decided for this step, e.g. \"6 ideas (Dolly, Ghost Face…)\"; details go in your reply"`
	Text   *string `json:"text,omitempty" jsonschema:"rename the step"`
}

type aTodoIDIn struct {
	ID string `json:"id" jsonschema:"todo id, from get_task"`
}

type aNoteIDIn struct {
	ID string `json:"id" jsonschema:"note id, from get_task or list_notes"`
}

type aListNotesIn struct {
	Project string `json:"project,omitempty" jsonschema:"project name"`
	Query   string `json:"query,omitempty" jsonschema:"text to find"`
}

type aCreateNoteIn struct {
	Project string `json:"project" jsonschema:"project name: notes live inside a project"`
	Title   string `json:"title"`
	Body    string `json:"body" jsonschema:"markdown"`
}

type aUpdateNoteIn struct {
	ID    string  `json:"id" jsonschema:"note id"`
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty" jsonschema:"the whole note as markdown, which replaces the current one: the current best result. Fold in what's still relevant, drop what's outdated; no questions, no changelog. The previous version is kept in the note's history."`
}

// taskTitle names a task for a step, falling back to its id.
func (a *assistantRun) taskTitle(ctx context.Context, id string) AssistantTaskRef {
	if d, err := a.s.GetTask(ctx, a.agent, TaskIDIn{ID: id}); err == nil {
		return AssistantTaskRef{ID: d.Task.ID, Title: d.Task.Title}
	}
	return AssistantTaskRef{ID: id, Title: id}
}

func (a *assistantRun) addTodos(ctx context.Context, raw json.RawMessage) (any, *AssistantStep, error) {
	var in aAddTodosIn
	if err := decodeArgs(raw, &in); err != nil {
		return nil, nil, err
	}
	if len(in.Items) > 10 {
		return nil, nil, errf(ErrInvalid, "add at most 10 todos at a time")
	}
	res, err := a.s.AddTodos(ctx, a.agent, AddTodosIn{Task: in.Task, Items: in.Items})
	if err != nil {
		return nil, &AssistantStep{Summary: "Couldn't add todos to " + clip(in.Task, 30)}, err
	}
	t := a.taskTitle(ctx, res.Task)
	return map[string]any{"todos": res.Todos}, &AssistantStep{Summary: fmt.Sprintf("Added %d todos to “%s”", len(in.Items), clip(t.Title, 60)), Tasks: []AssistantTaskRef{t}}, nil
}

func (a *assistantRun) updateTodo(ctx context.Context, raw json.RawMessage) (any, *AssistantStep, error) {
	var in aTodoIn
	if err := decodeArgs(raw, &in); err != nil {
		return nil, nil, err
	}
	res, err := a.s.UpdateTodo(ctx, a.agent, UpdateTodoIn{ID: in.ID, Done: in.Done, Result: in.Result, Text: in.Text})
	if err != nil {
		return nil, &AssistantStep{Summary: "Couldn't update a todo"}, err
	}
	var text string
	for _, td := range res.Todos {
		if td.ID == in.ID {
			text = td.Text
		}
	}
	summary := "Updated “" + clip(text, 60) + "”"
	switch {
	case in.Done != nil && *in.Done:
		summary = "Ticked “" + clip(text, 60) + "”"
	case in.Done != nil:
		summary = "Unticked “" + clip(text, 60) + "”"
	}
	if in.Result != nil && *in.Result != "" {
		summary += " → " + clip(*in.Result, 60)
	}
	return map[string]any{"todos": res.Todos}, &AssistantStep{Summary: summary, Tasks: []AssistantTaskRef{a.taskTitle(ctx, res.Task)}}, nil
}

func (a *assistantRun) removeTodo(ctx context.Context, raw json.RawMessage) (any, *AssistantStep, error) {
	var in aTodoIDIn
	if err := decodeArgs(raw, &in); err != nil {
		return nil, nil, err
	}
	res, err := a.s.RemoveTodo(ctx, a.agent, TodoIDIn{ID: in.ID})
	if err != nil {
		return nil, &AssistantStep{Summary: "Couldn't remove a todo"}, err
	}
	return map[string]any{"todos": res.Todos}, &AssistantStep{Summary: "Removed a todo", Tasks: []AssistantTaskRef{a.taskTitle(ctx, res.Task)}}, nil
}

// noteBrief is the note shape the model reads after a write: no body, it just sent it.
func noteBrief(n NoteOut) map[string]any {
	return map[string]any{"id": n.ID, "title": n.Title, "project": n.Project}
}

func (a *assistantRun) getNote(ctx context.Context, raw json.RawMessage) (any, *AssistantStep, error) {
	var in aNoteIDIn
	if err := decodeArgs(raw, &in); err != nil {
		return nil, nil, err
	}
	d, err := a.s.GetNote(ctx, a.agent, NoteIDIn{ID: in.ID})
	if err != nil {
		return nil, &AssistantStep{Summary: "Couldn't open note " + clip(in.ID, 30)}, err
	}
	b := noteBrief(d.Note)
	b["body"] = clipBytes(d.Note.Body, 6000)
	return b, &AssistantStep{Summary: "Read the note “" + clip(d.Note.Title, 60) + "”"}, nil
}

func (a *assistantRun) listNotes(ctx context.Context, raw json.RawMessage) (any, *AssistantStep, error) {
	var in aListNotesIn
	if err := decodeArgs(raw, &in); err != nil {
		return nil, nil, err
	}
	res, err := a.s.ListNotes(ctx, a.agent, ListNotesIn{Project: in.Project, Query: in.Query, Limit: 30})
	if err != nil {
		return nil, &AssistantStep{Summary: "Couldn't list notes"}, err
	}
	notes := make([]map[string]any, 0, len(res.Notes))
	for _, n := range res.Notes {
		notes = append(notes, noteBrief(n))
	}
	return map[string]any{"notes": notes}, &AssistantStep{Summary: fmt.Sprintf("Listed %d notes", len(notes))}, nil
}

func (a *assistantRun) createNote(ctx context.Context, raw json.RawMessage) (any, *AssistantStep, error) {
	var in aCreateNoteIn
	if err := decodeArgs(raw, &in); err != nil {
		return nil, nil, err
	}
	res, err := a.s.CreateNote(ctx, a.agent, CreateNoteIn{Project: in.Project, Title: in.Title, Body: in.Body})
	if err != nil {
		return nil, &AssistantStep{Summary: "Couldn't write the note"}, err
	}
	return map[string]any{"note": noteBrief(res.Note)}, &AssistantStep{Summary: "Wrote the note “" + clip(res.Note.Title, 60) + "”"}, nil
}

func (a *assistantRun) updateNote(ctx context.Context, raw json.RawMessage) (any, *AssistantStep, error) {
	var in aUpdateNoteIn
	if err := decodeArgs(raw, &in); err != nil {
		return nil, nil, err
	}
	// A pointer, so an empty body clears the note rather than being left out.
	res, err := a.s.UpdateNote(ctx, a.agent, UpdateNoteIn{ID: in.ID, Title: in.Title, Body: in.Body})
	if err != nil {
		return nil, &AssistantStep{Summary: "Couldn't update note " + clip(in.ID, 30)}, err
	}
	return map[string]any{"note": noteBrief(res.Note)}, &AssistantStep{Summary: "Updated the note “" + clip(res.Note.Title, 60) + "”"}, nil
}
