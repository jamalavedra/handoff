package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type RevertChangeIn struct {
	EventID int64 `json:"event_id" jsonschema:"id of the change to undo, from a task's or note's events"`
}

type RevertChangeOut struct {
	Task     *TaskOut `json:"task,omitempty"`
	Note     *NoteOut `json:"note,omitempty"`
	Reverted []string `json:"reverted" jsonschema:"the fields put back"`
	Message  string   `json:"message,omitempty" jsonschema:"anything the revert left as it was, and why"`
}

// revertFields are the update fields revert_change puts back, in the order it checks them.
var revertFields = []string{"title", "status", "priority", "assignee_id", "due", "labels", "repeat", "project_id"}

// bodyHash fingerprints a text, so a revert can tell whether it changed since.
func bodyHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// RevertChange undoes one change to a task or note through the normal update paths, so permissions,
// notifications and history apply as if the caller made the inverse change. It refuses when a field
// has changed again since. Agents may only revert their own changes; people any they may make.
func (s *Service) RevertChange(ctx context.Context, p *Principal, in RevertChangeIn) (RevertChangeOut, error) {
	out := RevertChangeOut{Reverted: []string{}}
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActUpdate); err != nil {
			return err
		}
		var taskID, kind, actor string
		var raw []byte
		err := c.tx.QueryRow(c.ctx, `SELECT coalesce(task_id, ''), kind, data, actor_id::text FROM events WHERE org_id = $1 AND id = $2`, p.Org.ID, in.EventID).
			Scan(&taskID, &kind, &raw, &actor)
		if errors.Is(err, pgx.ErrNoRows) {
			return errf(ErrNotFound, "no change %d", in.EventID)
		}
		if err != nil {
			return err
		}
		if p.Member.IsAgent() && actor != p.Member.ID {
			return errf(ErrForbidden, "an agent can only revert its own changes; ask a person to revert this one")
		}
		var d map[string]any
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		if kind == "note_updated" {
			return c.revertNote(in.EventID, d, &out)
		}
		if taskID == "" {
			return errf(ErrInvalid, "a %q change can't be reverted", kind)
		}
		r, err := c.getTask(taskID, true, true)
		if err != nil {
			return err
		}
		switch kind {
		case "updated":
			out.Reverted, err = c.revertUpdate(r, d)
		case "closed":
			if r.Status != d["status"] {
				return errf(ErrConflict, "the status changed again since; it's now %s", r.Status)
			}
			from, _ := d["from_status"].(string)
			if from == "" {
				from = "open"
			}
			out.Reverted = []string{"status"}
			if err = c.setStatus(r.ID, from); err == nil && d["status"] == "done" {
				out.Message, err = c.dropNext(r.ID)
			}
		case "trashed":
			if err := p.require(ActDelete); err != nil {
				return err
			}
			out.Reverted = []string{"trash"}
			err = c.restore(r)
		case "restored":
			if r.DeletedAt != nil {
				return errf(ErrConflict, "the task is in the trash again")
			}
			if err := p.require(ActDelete); err != nil {
				return err
			}
			if p.needsApproval(ActDelete) {
				return errf(ErrForbidden, "trashing needs approval; use trash_task")
			}
			out.Reverted = []string{"trash"}
			err = c.trash(r.ID, "reverted")
		case "todo_updated":
			out.Reverted, err = c.revertTodo(d)
		case "todo_removed":
			out.Reverted, err = c.restoreTodo(r, d)
		default:
			return errf(ErrInvalid, "a %q change can't be reverted", kind)
		}
		if err != nil {
			return err
		}
		if err := c.event(r.ID, "reverted", map[string]any{"event_id": in.EventID, "kind": kind, "fields": out.Reverted}); err != nil {
			return err
		}
		r, err = c.getTask(r.ID, true, false)
		if err != nil {
			return err
		}
		o := c.out(r, true)
		out.Task = &o
		return nil
	})
	return out, err
}

// sameJSON compares a value read from the database with one decoded from an event.
func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// previousBody is the text a body change replaced, when the change kept it whole (body_hash),
// after checking the text hasn't changed since.
func previousBody(current string, d map[string]any) (string, bool, error) {
	h, ok := d["body_hash"].(string)
	if !ok {
		return "", false, nil
	}
	if bodyHash(current) != h {
		return "", false, errf(ErrConflict, "the text changed again since this change")
	}
	prev, _ := d["previous_body"].(string) // absent: it was empty
	return prev, true, nil
}

func (c *call) revertUpdate(r *taskRow, d map[string]any) ([]string, error) {
	if r.DeletedAt != nil {
		return nil, errf(ErrInvalid, "task %s is in the trash; restore it first", r.ID)
	}
	cur := r.revertable()
	in := UpdateTaskIn{ID: r.ID}
	fields, closeTo := []string{}, ""
	for _, k := range revertFields {
		from, ok := d["from_"+k]
		to, has := d[k]
		if !ok || !has {
			continue
		}
		if !sameJSON(cur[k], to) {
			return nil, errf(ErrConflict, "%s changed again since this change; it's now %v", k, cur[k])
		}
		fields = append(fields, k)
		s, _ := from.(string)
		switch k {
		case "title":
			in.Title = &s
		case "status":
			if s == "done" || s == "cancelled" {
				closeTo = s
			} else {
				in.Status = s
			}
		case "priority":
			n, _ := from.(float64)
			pr := int(n)
			in.Priority = &pr
		case "assignee_id":
			in.Assignee = &s
		case "due":
			in.Due = &s
		case "labels":
			in.Labels = []string{}
			for _, l := range from.([]any) {
				in.Labels = append(in.Labels, fmt.Sprint(l))
			}
		case "repeat":
			in.Repeat = &s
		case "project_id":
			in.Project = s
		}
	}
	prev, ok, err := previousBody(r.Body, d)
	if err != nil {
		return nil, err
	}
	if ok {
		in.Body, in.BodyMode, in.Confirm = &prev, "replace", true
		fields = append(fields, "body")
	}
	if len(fields) == 0 {
		return nil, errf(ErrInvalid, "this change has nothing that can be reverted")
	}
	if len(fields) > 1 || closeTo == "" {
		if _, err := c.updateTask(in); err != nil {
			return nil, err
		}
	}
	if closeTo != "" {
		return fields, c.setStatus(r.ID, closeTo)
	}
	return fields, nil
}

// setStatus moves a task to any status: closing goes through close_task, the rest through update_task.
func (c *call) setStatus(id, st string) error {
	if st == "done" || st == "cancelled" {
		out := CloseTaskOut{}
		return c.closeTask(CloseTaskIn{ID: id, Status: st}, &out)
	}
	_, err := c.updateTask(UpdateTaskIn{ID: id, Status: st})
	return err
}

// dropNext trashes the next occurrence a repeating task made when it was closed, if nobody has touched
// it since; otherwise it stays, and the returned message says so.
func (c *call) dropNext(id string) (string, error) {
	var next, title string
	var untouched bool
	err := c.tx.QueryRow(c.ctx, `SELECT n.id, n.title, n.status = 'open' AND n.deleted_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM events e WHERE e.org_id = n.org_id AND e.task_id = n.id AND e.kind NOT IN ('created', 'triaged', 'triage_failed'))
		AND NOT EXISTS (SELECT 1 FROM comments cm WHERE cm.org_id = n.org_id AND cm.task_id = n.id)
		FROM tasks t JOIN tasks n ON n.org_id = t.org_id AND n.id = t.next_id WHERE t.id = $1`, id).Scan(&next, &title, &untouched)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	switch {
	case !untouched:
		return fmt.Sprintf("The next occurrence, %q, was changed since, so it stays.", title), nil
	case !c.p.can(ActDelete) || c.p.needsApproval(ActDelete):
		return fmt.Sprintf("The next occurrence, %q, stays: trashing it needs a person.", title), nil
	}
	if _, err := c.getTask(next, false, true); IsCode(err, ErrNotFound) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return fmt.Sprintf("Moved the next occurrence, %q, to the trash.", title), c.trash(next, "its repeating task was reopened")
}

func (c *call) revertTodo(d map[string]any) ([]string, error) {
	id, _ := d["todo_id"].(string)
	_, td, err := c.todoTask(id)
	if err != nil {
		return nil, err
	}
	in := UpdateTodoIn{ID: td.ID}
	fields := []string{}
	if from, ok := d["from"].(string); ok {
		if td.Text != d["text"] {
			return nil, errf(ErrConflict, "the todo was renamed again since")
		}
		in.Text, fields = &from, append(fields, "text")
	}
	if from, ok := d["from_done"].(bool); ok {
		if td.Done != d["done"] {
			return nil, errf(ErrConflict, "the todo was ticked or unticked again since")
		}
		in.Done, fields = &from, append(fields, "done")
	}
	if from, ok := d["from_result"].(string); ok {
		if td.Result != d["result"] {
			return nil, errf(ErrConflict, "the todo's result changed again since")
		}
		in.Result, fields = &from, append(fields, "result")
	}
	if from, ok := d["from_position"].(float64); ok {
		at, err := c.todoIndex(td.ID)
		if err != nil {
			return nil, err
		}
		if float64(at) != d["position"] {
			return nil, errf(ErrConflict, "the todo was moved again since")
		}
		pos := int(from)
		in.Position, fields = &pos, append(fields, "position")
	}
	if len(fields) == 0 {
		return nil, errf(ErrInvalid, "this change has nothing that can be reverted")
	}
	_, err = c.updateTodo(in)
	return fields, err
}

// restoreTodo puts a removed todo back as it was: same id, text, tick, result and place.
func (c *call) restoreTodo(r *taskRow, d map[string]any) ([]string, error) {
	if err := c.p.require(ActUpdate); err != nil {
		return nil, err
	}
	if r.DeletedAt != nil {
		return nil, errf(ErrInvalid, "task %s is in the trash; restore it first", r.ID)
	}
	id, _ := d["todo_id"].(string)
	text, _ := d["text"].(string)
	done, _ := d["done"].(bool)
	result, _ := d["result"].(string)
	pos, _ := d["position"].(float64)
	if strings.TrimSpace(text) == "" {
		return nil, errf(ErrInvalid, "this change has nothing that can be reverted")
	}
	if r.TodosTotal >= maxTodos {
		return nil, errf(ErrInvalid, "a task can have at most %d todos", maxTodos)
	}
	tag, err := c.tx.Exec(c.ctx, `INSERT INTO task_todos (id, org_id, task_id, text, done, done_by, done_at, result)
		VALUES ($1::uuid, $2, $3, $4, $5, CASE WHEN $5 THEN $6::uuid END, CASE WHEN $5 THEN now() END, $7) ON CONFLICT (id) DO NOTHING`,
		id, c.p.Org.ID, r.ID, text, done, c.p.Member.ID, result)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, errf(ErrConflict, "the todo is already back")
	}
	if _, err := c.moveTodo(r.ID, id, int(pos)); err != nil {
		return nil, err
	}
	if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET updated_at = now() WHERE id = $1`, r.ID); err != nil {
		return nil, err
	}
	return []string{"todo"}, c.event(r.ID, "todos_added", map[string]any{"todos": []string{text}})
}

func (c *call) revertNote(eventID int64, d map[string]any, out *RevertChangeOut) error {
	id, _ := d["note_id"].(string)
	r, err := c.getNote(id, false, true)
	if err != nil {
		return err
	}
	in := UpdateNoteIn{ID: r.ID}
	if from, ok := d["from_title"].(string); ok {
		if r.Title != d["title"] {
			return errf(ErrConflict, "the note's title changed again since")
		}
		in.Title, out.Reverted = &from, append(out.Reverted, "title")
	}
	prev, ok, err := previousBody(r.Body, d)
	if err != nil {
		return err
	}
	if ok {
		in.Body, in.Mode, out.Reverted = &prev, "replace", append(out.Reverted, "body")
	}
	if len(out.Reverted) == 0 {
		return errf(ErrInvalid, "this change has nothing that can be reverted")
	}
	if r, err = c.updateNote(in); err != nil {
		return err
	}
	if err := c.noteEvent(r, "reverted", map[string]any{"event_id": eventID, "kind": "note_updated", "fields": out.Reverted}); err != nil {
		return err
	}
	o := c.noteOut(r, true)
	out.Note = &o
	return nil
}
