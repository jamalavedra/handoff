package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joalavedra/handoff/internal/db"
)

// Notes are markdown documents inside a project: the place for longer work (research, shortlists,
// drafts, plans). A note can be written for one task, which links to it. Who sees a note follows its
// project, exactly like tasks; agents need update to write a task's note and create for others.

const (
	noteTitleChars = 200
	noteBodyBytes  = 100000
)

type NoteOut struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Body       string     `json:"body,omitempty" jsonschema:"markdown"`
	Project    string     `json:"project"`
	ProjectID  string     `json:"project_id"`
	Task       *NoteTask  `json:"task,omitempty" jsonschema:"the task it was written for"`
	CreatedBy  *MemberRef `json:"created_by,omitempty"`
	OnBehalfOf *MemberRef `json:"on_behalf_of,omitempty"`
	UpdatedBy  *MemberRef `json:"updated_by,omitempty"`
	CreatedAt  string     `json:"created_at"`
	UpdatedAt  string     `json:"updated_at"`
	DeletedAt  string     `json:"deleted_at,omitempty"`
	URL        string     `json:"url"`
}

type NoteTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type noteRow struct {
	ID, ProjectID, TaskID, TaskTitle, TaskProject string
	Title, Body                                   string
	CreatedBy, OnBehalfOf, UpdatedBy              string
	CreatedAt, UpdatedAt                          time.Time
	DeletedAt                                     *time.Time
}

const noteSelect = `SELECT n.id, n.project_id::text, coalesce(n.task_id, ''), coalesce(t.title, ''), coalesce(t.project_id::text, ''), n.title, n.body,
	n.created_by::text, coalesce(n.on_behalf_of::text, ''), coalesce(n.updated_by::text, ''), n.created_at, n.updated_at, n.deleted_at
	FROM notes n LEFT JOIN tasks t ON t.org_id = n.org_id AND t.id = n.task_id AND t.deleted_at IS NULL`

func (c *call) queryNotes(where string, args ...any) ([]*noteRow, error) {
	rows, err := c.tx.Query(c.ctx, noteSelect+" WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*noteRow
	for rows.Next() {
		r := &noteRow{}
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.TaskID, &r.TaskTitle, &r.TaskProject, &r.Title, &r.Body,
			&r.CreatedBy, &r.OnBehalfOf, &r.UpdatedBy, &r.CreatedAt, &r.UpdatedAt, &r.DeletedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (c *call) noteOut(r *noteRow, withBody bool) NoteOut {
	o := NoteOut{ID: r.ID, Title: r.Title, ProjectID: r.ProjectID, CreatedBy: c.ref(r.CreatedBy), OnBehalfOf: c.ref(r.OnBehalfOf),
		UpdatedBy: c.ref(r.UpdatedBy), CreatedAt: ts(&r.CreatedAt), UpdatedAt: ts(&r.UpdatedAt), DeletedAt: ts(r.DeletedAt),
		URL: c.s.Cfg.OrgURL(c.p.Org.Slug) + "/notes/" + r.ID}
	if p := c.projects[r.ProjectID]; p != nil {
		o.Project = p.Name
	}
	// The task shows only while the caller can see it (it may have moved to a private project).
	if r.TaskID != "" && r.TaskProject != "" && c.canSeeProject(r.TaskProject) {
		o.Task = &NoteTask{ID: r.TaskID, Title: r.TaskTitle}
	}
	if withBody {
		o.Body = r.Body
	}
	return o
}

// getNote loads a note in a project the caller can see; trashed ones only when includeDeleted.
func (c *call) getNote(id string, includeDeleted, forUpdate bool) (*noteRow, error) {
	id = strings.TrimSpace(id)
	if forUpdate {
		if _, err := c.tx.Exec(c.ctx, `SELECT 1 FROM notes WHERE id = $1 FOR UPDATE`, id); err != nil {
			return nil, err
		}
	}
	q := "n.id = $1 AND n.project_id::text = ANY($2)"
	if !includeDeleted {
		q += " AND n.deleted_at IS NULL"
	}
	rs, err := c.queryNotes(q, id, c.visible)
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, errf(ErrNotFound, "no note %q (or you can't see it)", id)
	}
	return rs[0], nil
}

// noteEvent records a change to a note. It's on the note's task, when it has one, so the task's
// history and open screens show it too.
func (c *call) noteEvent(r *noteRow, kind string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	data["note_id"], data["project_id"] = r.ID, r.ProjectID
	if _, ok := data["title"]; !ok {
		data["title"] = r.Title
	}
	return c.event(r.TaskID, kind, data)
}

// noteVisible keeps note events to the projects in parameter $n: a task's history never shows a
// note from a project the caller can't see.
func noteVisible(n int) string {
	return fmt.Sprintf("(e.data->>'note_id' IS NULL OR e.data->>'project_id' = ANY($%d))", n)
}

func cleanNoteTitle(s string) (string, error) {
	t := oneLine(s)
	if t == "" || len(t) > noteTitleChars {
		return "", errf(ErrInvalid, "title must be 1-%d characters", noteTitleChars)
	}
	return t, nil
}

type CreateNoteIn struct {
	Project string `json:"project,omitempty" jsonschema:"project name or id; defaults to the task's project"`
	Task    string `json:"task,omitempty" jsonschema:"the task this note is for; it links to it"`
	Title   string `json:"title" jsonschema:"for a task's note: \"<task title> — <kind>\", e.g. \"Halloween costume — research\""`
	Body    string `json:"body,omitempty" jsonschema:"markdown"`
}

type NoteResult struct {
	Note NoteOut `json:"note"`
}

func (s *Service) CreateNote(ctx context.Context, p *Principal, in CreateNoteIn) (NoteResult, error) {
	var out NoteResult
	err := s.run(ctx, p, func(c *call) error {
		r, err := c.createNote(in)
		if err != nil {
			return err
		}
		out.Note = c.noteOut(r, true)
		return nil
	})
	return out, err
}

func (c *call) createNote(in CreateNoteIn) (*noteRow, error) {
	title, err := cleanNoteTitle(in.Title)
	if err != nil {
		return nil, err
	}
	if len(in.Body) > noteBodyBytes {
		return nil, errf(ErrInvalid, "a note can be at most %d characters", noteBodyBytes)
	}
	var task *taskRow
	if strings.TrimSpace(in.Task) != "" {
		// Writing a task's note is part of working on the task.
		if err := c.p.require(ActUpdate); err != nil {
			return nil, err
		}
		if task, err = c.getTask(in.Task, false, false); err != nil {
			return nil, err
		}
	} else if err := c.p.require(ActCreate); err != nil {
		return nil, err
	}
	var pr *Project
	switch {
	case strings.TrimSpace(in.Project) != "":
		if pr, err = c.resolveProject(in.Project); err != nil {
			return nil, err
		}
		if task != nil && pr.ID != task.ProjectID {
			return nil, errf(ErrInvalid, "a task's note goes in the task's project (%s)", c.projects[task.ProjectID].Name)
		}
	case task != nil:
		pr = c.projects[task.ProjectID]
	default:
		return nil, errf(ErrInvalid, "say which project the note goes in")
	}
	var taskID *string
	if task != nil {
		taskID = &task.ID
	}
	for n := 4; n <= 10; n++ {
		id := c.p.Org.IDPrefix + "-n" + strings.TrimPrefix(randomID("", n), "-")
		if _, err := c.tx.Exec(c.ctx, `SAVEPOINT create_note`); err != nil {
			return nil, err
		}
		_, err := c.tx.Exec(c.ctx, `INSERT INTO notes (org_id, id, project_id, task_id, title, body, created_by, on_behalf_of, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $7)`, c.p.Org.ID, id, pr.ID, taskID, title, in.Body, c.p.Member.ID, c.p.onBehalfOf())
		if db.IsUniqueViolation(err) {
			if _, err := c.tx.Exec(c.ctx, `ROLLBACK TO SAVEPOINT create_note`); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if _, err := c.tx.Exec(c.ctx, `RELEASE SAVEPOINT create_note`); err != nil {
			return nil, err
		}
		r, err := c.getNote(id, false, false)
		if err != nil {
			return nil, err
		}
		if task != nil {
			if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET updated_at = now() WHERE id = $1`, task.ID); err != nil {
				return nil, err
			}
		}
		return r, c.noteEvent(r, "note_created", nil)
	}
	return nil, errors.New("could not allocate a note id")
}

type UpdateNoteIn struct {
	ID    string  `json:"id" jsonschema:"note id"`
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty" jsonschema:"markdown: the whole note, which replaces the current one (the previous version stays in its history); with mode=append, text to add at the end"`
	Mode  string  `json:"mode,omitempty" jsonschema:"replace (default) or append"`
}

func (s *Service) UpdateNote(ctx context.Context, p *Principal, in UpdateNoteIn) (NoteResult, error) {
	var out NoteResult
	err := s.run(ctx, p, func(c *call) error {
		r, err := c.updateNote(in)
		if err != nil {
			return err
		}
		out.Note = c.noteOut(r, true)
		return nil
	})
	return out, err
}

func (c *call) updateNote(in UpdateNoteIn) (*noteRow, error) {
	if err := c.p.require(ActUpdate); err != nil {
		return nil, err
	}
	r, err := c.getNote(in.ID, false, true)
	if err != nil {
		return nil, err
	}
	changed := map[string]any{}
	title, body := r.Title, r.Body
	if in.Title != nil {
		if title, err = cleanNoteTitle(*in.Title); err != nil {
			return nil, err
		}
		if title != r.Title {
			changed["title"], changed["from_title"] = title, r.Title
		}
	}
	if in.Body != nil {
		switch in.Mode {
		case "", "replace":
			body = *in.Body
		case "append":
			if strings.TrimSpace(body) != "" {
				body += "\n\n"
			}
			body += *in.Body
		default:
			return nil, errf(ErrInvalid, "mode must be replace or append")
		}
		if len(body) > noteBodyBytes {
			return nil, errf(ErrInvalid, "a note can be at most %d characters", noteBodyBytes)
		}
		if body != r.Body {
			changed["body"] = "replaced"
			if in.Mode == "append" {
				changed["body"] = "appended"
			}
			// The version it replaces stays in the note's history.
			if strings.TrimSpace(r.Body) != "" {
				changed["previous_body"] = clipBytes(r.Body, 20000)
			}
		}
	}
	if len(changed) == 0 {
		return r, nil
	}
	if _, err := c.tx.Exec(c.ctx, `UPDATE notes SET title = $2, body = $3, updated_by = $4, updated_at = now() WHERE id = $1`,
		r.ID, title, body, c.p.Member.ID); err != nil {
		return nil, err
	}
	if r.TaskID != "" {
		if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET updated_at = now() WHERE id = $1`, r.TaskID); err != nil {
			return nil, err
		}
	}
	if err := c.noteEvent(r, "note_updated", changed); err != nil {
		return nil, err
	}
	return c.getNote(r.ID, false, false)
}

type NoteIDIn struct {
	ID string `json:"id" jsonschema:"note id, e.g. acme-n7f3a"`
}

type NoteDetailOut struct {
	Note   NoteOut    `json:"note"`
	Events []EventOut `json:"events" jsonschema:"who changed it, oldest first"`
}

func (s *Service) GetNote(ctx context.Context, p *Principal, in NoteIDIn) (NoteDetailOut, error) {
	out := NoteDetailOut{Events: []EventOut{}}
	err := s.run(ctx, p, func(c *call) error {
		r, err := c.getNote(in.ID, true, false)
		if err != nil {
			return err
		}
		out.Note = c.noteOut(r, true)
		out.Events, err = c.events("e.data->>'note_id' = $1", []any{r.ID}, 200, true)
		return err
	})
	return out, err
}

type ListNotesIn struct {
	Project string `json:"project,omitempty" jsonschema:"project name or id; omit for every project you can see"`
	Task    string `json:"task,omitempty" jsonschema:"only notes written for this task"`
	Query   string `json:"query,omitempty" jsonschema:"text to find in the title or body"`
	Trashed bool   `json:"trashed,omitempty" jsonschema:"list trashed notes instead"`
	Limit   int    `json:"limit,omitempty" jsonschema:"default 50, max 200"`
}

type NotesOut struct {
	Notes []NoteOut `json:"notes"`
}

func (s *Service) ListNotes(ctx context.Context, p *Principal, in ListNotesIn) (NotesOut, error) {
	out := NotesOut{Notes: []NoteOut{}}
	err := s.run(ctx, p, func(c *call) error {
		var err error
		out.Notes, err = c.listNotes(in)
		return err
	})
	return out, err
}

func (c *call) listNotes(in ListNotesIn) ([]NoteOut, error) {
	where := "n.project_id::text = ANY($1)"
	args := []any{c.visible}
	add := func(cond string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(cond, len(args))
	}
	if in.Trashed {
		where += " AND n.deleted_at IS NOT NULL"
	} else {
		where += " AND n.deleted_at IS NULL"
	}
	if strings.TrimSpace(in.Project) != "" {
		pr, err := c.resolveProject(in.Project)
		if err != nil {
			return nil, err
		}
		add(" AND n.project_id = $%d", pr.ID)
	}
	if strings.TrimSpace(in.Task) != "" {
		r, err := c.getTask(in.Task, true, false)
		if err != nil {
			return nil, err
		}
		add(" AND n.task_id = $%d", r.ID)
	}
	if q := strings.TrimSpace(in.Query); q != "" {
		add(" AND (n.title ILIKE '%%' || $%[1]d || '%%' OR n.body ILIKE '%%' || $%[1]d || '%%')", q)
	}
	add(" ORDER BY n.updated_at DESC LIMIT $%d", clampLimit(in.Limit, 50, 200))
	rs, err := c.queryNotes(where, args...)
	if err != nil {
		return nil, err
	}
	out := make([]NoteOut, 0, len(rs))
	for _, r := range rs {
		out = append(out, c.noteOut(r, false))
	}
	return out, nil
}

func (s *Service) TrashNote(ctx context.Context, p *Principal, in NoteIDIn) (NoteResult, error) {
	var out NoteResult
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActDelete); err != nil {
			return err
		}
		if p.needsApproval(ActDelete) {
			return errf(ErrForbidden, "this agent needs a person's approval to delete; ask %s to move the note to the trash", p.ownerName())
		}
		r, err := c.getNote(in.ID, false, true)
		if err != nil {
			return err
		}
		if _, err := c.tx.Exec(c.ctx, `UPDATE notes SET deleted_at = now(), deleted_by = $2 WHERE id = $1`, r.ID, p.Member.ID); err != nil {
			return err
		}
		if err := c.noteEvent(r, "note_trashed", nil); err != nil {
			return err
		}
		r, err = c.getNote(r.ID, true, false)
		if err != nil {
			return err
		}
		out.Note = c.noteOut(r, false)
		return nil
	})
	return out, err
}

func (s *Service) RestoreNote(ctx context.Context, p *Principal, in NoteIDIn) (NoteResult, error) {
	var out NoteResult
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActDelete); err != nil {
			return err
		}
		r, err := c.getNote(in.ID, true, true)
		if err != nil {
			return err
		}
		if r.DeletedAt == nil {
			return errf(ErrInvalid, "note %s is not in the trash", r.ID)
		}
		if _, err := c.tx.Exec(c.ctx, `UPDATE notes SET deleted_at = NULL, deleted_by = NULL WHERE id = $1`, r.ID); err != nil {
			return err
		}
		if err := c.noteEvent(r, "note_restored", nil); err != nil {
			return err
		}
		r, err = c.getNote(r.ID, false, false)
		if err != nil {
			return err
		}
		out.Note = c.noteOut(r, true)
		return nil
	})
	return out, err
}

// taskNotes are the notes written for a task, newest first.
func (c *call) taskNotes(taskID string) ([]NoteOut, error) {
	return c.listNotes(ListNotesIn{Task: taskID, Limit: 20})
}

// agentNoteFor is the note this agent last wrote for a task, if any: where its next work goes.
func (c *call) agentNoteFor(taskID string) (*noteRow, error) {
	rs, err := c.queryNotes("n.task_id = $1 AND n.created_by = $2 AND n.deleted_at IS NULL AND n.project_id::text = ANY($3) ORDER BY n.updated_at DESC LIMIT 1",
		taskID, c.p.Member.ID, c.visible)
	if err != nil || len(rs) == 0 {
		return nil, err
	}
	return rs[0], nil
}
