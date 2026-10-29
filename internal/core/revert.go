package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type RevertChangeIn struct {
	EventID int64 `json:"event_id" jsonschema:"id of the change to undo, from a task's events"`
}

type RevertChangeOut struct {
	Task     TaskOut  `json:"task"`
	Reverted []string `json:"reverted" jsonschema:"the fields put back"`
}

// revertFields are the update fields revert_change puts back, in the order it checks them.
var revertFields = []string{"title", "status", "priority", "assignee_id", "due", "labels", "repeat", "project_id"}

// RevertChange undoes one change to a task through the normal update paths, so permissions,
// notifications and history apply as if the caller made the inverse change. It refuses when a field
// has changed again since.
func (s *Service) RevertChange(ctx context.Context, p *Principal, in RevertChangeIn) (RevertChangeOut, error) {
	out := RevertChangeOut{Reverted: []string{}}
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActUpdate); err != nil {
			return err
		}
		var taskID, kind string
		var raw []byte
		err := c.tx.QueryRow(c.ctx, `SELECT coalesce(task_id, ''), kind, data FROM events WHERE org_id = $1 AND id = $2`, p.Org.ID, in.EventID).
			Scan(&taskID, &kind, &raw)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && taskID == "") {
			return errf(ErrNotFound, "no task change %d", in.EventID)
		}
		if err != nil {
			return err
		}
		var d map[string]any
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
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
			err = c.setStatus(r.ID, from)
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
		out.Task = c.out(r, true)
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
	if len(fields) == 0 {
		return nil, errf(ErrInvalid, "this change has nothing that can be reverted")
	}
	_, err = c.updateTodo(in)
	return fields, err
}
