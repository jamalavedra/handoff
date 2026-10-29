package core

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Todos are a task's steps: a short line each, ticked off in order. When an agent works through
// them it ticks each one with a one-line result ("6 ideas (Dolly, Ghost Face…)"); anything longer
// goes in the task's note.

const (
	maxTodos      = 100 // per task
	todoTextChars = 300
	resultChars   = 300
)

type TodoOut struct {
	ID       string     `json:"id"`
	Text     string     `json:"text"`
	Done     bool       `json:"done"`
	Result   string     `json:"result,omitempty" jsonschema:"one line: what was found or decided for this step"`
	DoneBy   *MemberRef `json:"done_by,omitempty"`
	DoneAt   string     `json:"done_at,omitempty"`
	Position int        `json:"position"`
}

// TodoProgress is how far a task's todos are along, for lists.
type TodoProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

func (c *call) todos(taskID string) ([]TodoOut, error) {
	rows, err := c.tx.Query(c.ctx, `SELECT id::text, text, done, result, coalesce(done_by::text, ''), done_at, position
		FROM task_todos WHERE task_id = $1 ORDER BY position, created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TodoOut{}
	for rows.Next() {
		var td TodoOut
		var by string
		var at *time.Time
		if err := rows.Scan(&td.ID, &td.Text, &td.Done, &td.Result, &by, &at, &td.Position); err != nil {
			return nil, err
		}
		td.DoneBy, td.DoneAt = c.ref(by), ts(at)
		out = append(out, td)
	}
	return out, rows.Err()
}

// cleanTodo makes one line of a todo's text or result.
func cleanTodo(s string, n int) string {
	return clip(oneLine(s), n)
}

// addTodos appends steps to a task the caller may change, in order.
func (c *call) addTodos(taskID string, items []string) ([]string, error) {
	var texts []string
	for _, it := range items {
		if t := cleanTodo(it, todoTextChars); t != "" {
			texts = append(texts, t)
		}
	}
	if len(texts) == 0 {
		return nil, errf(ErrInvalid, "give at least one todo")
	}
	var n, next int
	if err := c.tx.QueryRow(c.ctx, `SELECT count(*), coalesce(max(position) + 1, 0) FROM task_todos WHERE task_id = $1`, taskID).Scan(&n, &next); err != nil {
		return nil, err
	}
	if n+len(texts) > maxTodos {
		return nil, errf(ErrInvalid, "a task can have at most %d todos", maxTodos)
	}
	for i, t := range texts {
		if _, err := c.tx.Exec(c.ctx, `INSERT INTO task_todos (org_id, task_id, position, text) VALUES ($1, $2, $3, $4)`,
			c.p.Org.ID, taskID, next+i, t); err != nil {
			return nil, err
		}
	}
	if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET updated_at = now() WHERE id = $1`, taskID); err != nil {
		return nil, err
	}
	return texts, c.event(taskID, "todos_added", map[string]any{"todos": texts})
}

type AddTodosIn struct {
	Task  string   `json:"task" jsonschema:"task id"`
	Items []string `json:"items" jsonschema:"1 to 50 steps in order, each a short action (at most 8 words), with a deadline after a colon when it matters (\"Order: by Oct 24\")"`
}

type TodosOut struct {
	Task  string    `json:"task"`
	Todos []TodoOut `json:"todos"`
}

func (s *Service) AddTodos(ctx context.Context, p *Principal, in AddTodosIn) (TodosOut, error) {
	var out TodosOut
	if len(in.Items) > 50 {
		return out, errf(ErrInvalid, "add at most 50 todos at a time")
	}
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActUpdate); err != nil {
			return err
		}
		r, err := c.getTask(in.Task, false, true)
		if err != nil {
			return err
		}
		if _, err := c.addTodos(r.ID, in.Items); err != nil {
			return err
		}
		out.Task = r.ID
		out.Todos, err = c.todos(r.ID)
		return err
	})
	return out, err
}

type UpdateTodoIn struct {
	ID       string  `json:"id" jsonschema:"todo id, from get_task"`
	Text     *string `json:"text,omitempty"`
	Done     *bool   `json:"done,omitempty" jsonschema:"tick or untick it"`
	Result   *string `json:"result,omitempty" jsonschema:"one line: what you found or decided for this step, e.g. \"6 ideas (Dolly, Ghost Face…)\"; details go in the task's note. Empty clears it"`
	Position *int    `json:"position,omitempty" jsonschema:"move it to this place in the list, 0 first"`
}

type TodoIDIn struct {
	ID string `json:"id" jsonschema:"todo id, from get_task"`
}

// todoTask finds a todo's task, if the caller may change it, and locks it.
func (c *call) todoTask(id string) (*taskRow, *TodoOut, error) {
	var taskID string
	var td TodoOut
	err := c.tx.QueryRow(c.ctx, `SELECT task_id, id::text, text, done, result, position FROM task_todos WHERE id::text = $1`, strings.TrimSpace(id)).
		Scan(&taskID, &td.ID, &td.Text, &td.Done, &td.Result, &td.Position)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, errf(ErrNotFound, "no todo %q", id)
	}
	if err != nil {
		return nil, nil, err
	}
	r, err := c.getTask(taskID, false, true)
	if IsCode(err, ErrNotFound) {
		return nil, nil, errf(ErrNotFound, "no todo %q", id)
	}
	return r, &td, err
}

func (s *Service) UpdateTodo(ctx context.Context, p *Principal, in UpdateTodoIn) (TodosOut, error) {
	var out TodosOut
	err := s.run(ctx, p, func(c *call) error {
		id, err := c.updateTodo(in)
		if err != nil {
			return err
		}
		out.Task = id
		out.Todos, err = c.todos(id)
		return err
	})
	return out, err
}

// updateTodo changes a todo inside the caller's transaction and returns its task's id.
func (c *call) updateTodo(in UpdateTodoIn) (string, error) {
	if err := c.p.require(ActUpdate); err != nil {
		return "", err
	}
	r, td, err := c.todoTask(in.ID)
	if err != nil {
		return "", err
	}
	changed := map[string]any{"todo_id": td.ID, "text": td.Text}
	if in.Text != nil {
		t := cleanTodo(*in.Text, todoTextChars)
		if t == "" {
			return "", errf(ErrInvalid, "a todo needs some text; use remove_todo to drop it")
		}
		if t != td.Text {
			if _, err := c.tx.Exec(c.ctx, `UPDATE task_todos SET text = $2, updated_at = now() WHERE id::text = $1`, td.ID, t); err != nil {
				return "", err
			}
			changed["text"], changed["from"] = t, td.Text
		}
	}
	if in.Done != nil && *in.Done != td.Done {
		if _, err := c.tx.Exec(c.ctx, `UPDATE task_todos SET done = $2, done_by = CASE WHEN $2 THEN $3::uuid END,
			done_at = CASE WHEN $2 THEN now() END, updated_at = now() WHERE id::text = $1`, td.ID, *in.Done, c.p.Member.ID); err != nil {
			return "", err
		}
		changed["done"], changed["from_done"] = *in.Done, td.Done
	}
	if in.Result != nil {
		res := cleanTodo(*in.Result, resultChars)
		if res != td.Result {
			if _, err := c.tx.Exec(c.ctx, `UPDATE task_todos SET result = $2, updated_at = now() WHERE id::text = $1`, td.ID, res); err != nil {
				return "", err
			}
			changed["result"], changed["from_result"] = res, td.Result
		}
	}
	if in.Position != nil {
		from, err := c.todoIndex(td.ID)
		if err != nil {
			return "", err
		}
		to, err := c.moveTodo(r.ID, td.ID, *in.Position)
		if err != nil {
			return "", err
		}
		changed["moved"], changed["position"], changed["from_position"] = true, to, from
	}
	if len(changed) > 2 {
		if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET updated_at = now() WHERE id = $1`, r.ID); err != nil {
			return "", err
		}
		if err := c.event(r.ID, "todo_updated", changed); err != nil {
			return "", err
		}
	}
	return r.ID, nil
}

// moveTodo puts a todo at position pos (clamped), renumbers the list 0..n-1 and returns where it went.
func (c *call) moveTodo(taskID, id string, pos int) (int, error) {
	rows, err := c.tx.Query(c.ctx, `SELECT id::text FROM task_todos WHERE task_id = $1 AND id::text <> $2 ORDER BY position, created_at`, taskID, id)
	if err != nil {
		return 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, err
	}
	pos = min(max(pos, 0), len(ids))
	ids = append(ids[:pos], append([]string{id}, ids[pos:]...)...)
	_, err = c.tx.Exec(c.ctx, `UPDATE task_todos t SET position = o.n - 1 FROM unnest($1::text[]) WITH ORDINALITY AS o(id, n)
		WHERE t.id::text = o.id`, ids)
	return pos, err
}

// todoIndex is a todo's place in its list, 0 first, in the order moveTodo counts.
func (c *call) todoIndex(id string) (int, error) {
	var n int
	err := c.tx.QueryRow(c.ctx, `SELECT count(*) FROM task_todos o, task_todos t WHERE t.id::text = $1 AND o.task_id = t.task_id
		AND o.id <> t.id AND (o.position, o.created_at) <= (t.position, t.created_at)`, id).Scan(&n)
	return n, err
}

func (s *Service) RemoveTodo(ctx context.Context, p *Principal, in TodoIDIn) (TodosOut, error) {
	var out TodosOut
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActUpdate); err != nil {
			return err
		}
		r, td, err := c.todoTask(in.ID)
		if err != nil {
			return err
		}
		at, err := c.todoIndex(td.ID)
		if err != nil {
			return err
		}
		if _, err := c.tx.Exec(c.ctx, `DELETE FROM task_todos WHERE id::text = $1`, td.ID); err != nil {
			return err
		}
		if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET updated_at = now() WHERE id = $1`, r.ID); err != nil {
			return err
		}
		// Enough to put it back with revert_change.
		if err := c.event(r.ID, "todo_removed", map[string]any{"todo_id": td.ID, "text": td.Text, "done": td.Done, "result": td.Result, "position": at}); err != nil {
			return err
		}
		out.Task = r.ID
		out.Todos, err = c.todos(r.ID)
		return err
	})
	return out, err
}
