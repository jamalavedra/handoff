package core

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/db"
)

var Statuses = []string{"open", "in_progress", "blocked", "done", "cancelled"}

type TaskOut struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Body           string     `json:"body,omitempty"`
	Status         string     `json:"status"`
	Priority       int        `json:"priority" jsonschema:"0 (urgent) to 4 (someday)"`
	Project        string     `json:"project"`
	ProjectID      string     `json:"project_id"`
	Assignee       *MemberRef `json:"assignee,omitempty"`
	Due            string     `json:"due,omitempty"`
	Labels         []string   `json:"labels"`
	Parent         string     `json:"parent,omitempty"`
	ExternalID     string     `json:"external_id,omitempty"`
	ClaimedBy      *MemberRef `json:"claimed_by,omitempty"`
	ClaimExpiresAt string     `json:"claim_expires_at,omitempty"`
	CreatedBy      *MemberRef `json:"created_by,omitempty"`
	OnBehalfOf     *MemberRef `json:"on_behalf_of,omitempty"`
	CreatedAt      string     `json:"created_at"`
	UpdatedAt      string     `json:"updated_at"`
	ClosedAt       string     `json:"closed_at,omitempty"`
	CloseReason    string     `json:"close_reason,omitempty"`
	DeletedAt      string     `json:"deleted_at,omitempty"`
	BlockedBy      []string   `json:"blocked_by"`
	Blocks         []string   `json:"blocks"`
	Ready          bool       `json:"ready"`
	Unseen         bool       `json:"unseen,omitempty" jsonschema:"someone else changed it since you last opened it"`
	URL            string     `json:"url"`
}

type taskRow struct {
	ID, ProjectID, Title, Body, Status                         string
	Priority                                                   int
	AssigneeID, Parent, ExternalID, ClaimedBy                  string
	CreatedBy, OnBehalfOf, CloseReason                         string
	Due, ClaimExpires, ClosedAt, DeletedAt, CreatedAt, Updated *time.Time
	Labels                                                     []string
	BlockedBy, Blocks                                          []string
	OpenBlockers                                               int
	Unseen                                                     bool
}

// openBlockers counts unfinished blockers of t; the same expression drives "ready".
const openBlockers = `(SELECT count(*) FROM task_deps d JOIN tasks b ON b.org_id = d.org_id AND b.id = d.blocker_id
	WHERE d.org_id = t.org_id AND d.blocked_id = t.id AND b.status NOT IN ('done', 'cancelled') AND b.deleted_at IS NULL)`

const taskSelect = `SELECT t.id, t.project_id::text, t.title, t.body, t.status, t.priority,
	coalesce(t.assignee_id::text, ''), coalesce(t.parent_id, ''), coalesce(t.external_id, ''), coalesce(t.claimed_by::text, ''),
	t.created_by::text, coalesce(t.on_behalf_of::text, ''), t.close_reason,
	t.due_at, t.claim_expires_at, t.closed_at, t.deleted_at, t.created_at, t.updated_at, t.labels,
	coalesce((SELECT array_agg(d.blocker_id ORDER BY d.blocker_id) FROM task_deps d WHERE d.org_id = t.org_id AND d.blocked_id = t.id), '{}'),
	coalesce((SELECT array_agg(d.blocked_id ORDER BY d.blocked_id) FROM task_deps d WHERE d.org_id = t.org_id AND d.blocker_id = t.id), '{}'),
	` + openBlockers + `, ` + unseen + `
	FROM tasks t`

// unseen: someone else changed the task after the caller last opened it.
const unseen = `EXISTS (SELECT 1 FROM events e WHERE e.org_id = t.org_id AND e.task_id = t.id
	AND e.actor_id::text <> current_setting('app.member_id', true) AND e.created_at > coalesce(
		(SELECT s.seen_at FROM task_seen s WHERE s.org_id = t.org_id AND s.task_id = t.id AND s.member_id::text = current_setting('app.member_id', true)),
		(SELECT m.seen_baseline FROM members m WHERE m.org_id = t.org_id AND m.id::text = current_setting('app.member_id', true))))`

// readyCond matches tasks someone can start now.
const readyCond = `t.deleted_at IS NULL
	AND (t.status = 'open' OR (t.status = 'in_progress' AND t.claim_expires_at < now()))
	AND (t.claimed_by IS NULL OR t.claim_expires_at < now())
	AND ` + openBlockers + ` = 0`

func (c *call) queryTasks(where string, args ...any) ([]*taskRow, error) {
	rows, err := c.tx.Query(c.ctx, taskSelect+" WHERE "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*taskRow
	for rows.Next() {
		r := &taskRow{}
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Title, &r.Body, &r.Status, &r.Priority,
			&r.AssigneeID, &r.Parent, &r.ExternalID, &r.ClaimedBy, &r.CreatedBy, &r.OnBehalfOf, &r.CloseReason,
			&r.Due, &r.ClaimExpires, &r.ClosedAt, &r.DeletedAt, &r.CreatedAt, &r.Updated, &r.Labels,
			&r.BlockedBy, &r.Blocks, &r.OpenBlockers, &r.Unseen); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func claimActive(r *taskRow) bool {
	return r.ClaimedBy != "" && r.ClaimExpires != nil && r.ClaimExpires.After(time.Now())
}

func (c *call) out(r *taskRow, withBody bool) TaskOut {
	o := TaskOut{
		ID: r.ID, Title: r.Title, Status: r.Status, Priority: r.Priority, ProjectID: r.ProjectID,
		Assignee: c.ref(r.AssigneeID), Labels: nonNil(r.Labels), Parent: r.Parent, ExternalID: r.ExternalID,
		CreatedBy: c.ref(r.CreatedBy), OnBehalfOf: c.ref(r.OnBehalfOf), CreatedAt: ts(r.CreatedAt), UpdatedAt: ts(r.Updated),
		ClosedAt: ts(r.ClosedAt), CloseReason: r.CloseReason, DeletedAt: ts(r.DeletedAt),
		BlockedBy: nonNil(r.BlockedBy), Blocks: nonNil(r.Blocks), Unseen: r.Unseen,
		URL: c.s.Cfg.OrgURL(c.p.Org.Slug) + "/tasks/" + r.ID,
	}
	if p := c.projects[r.ProjectID]; p != nil {
		o.Project = p.Name
	}
	if r.Due != nil {
		o.Due = r.Due.UTC().Format("2006-01-02")
	}
	if claimActive(r) {
		o.ClaimedBy = c.ref(r.ClaimedBy)
		o.ClaimExpiresAt = ts(r.ClaimExpires)
	}
	if withBody {
		o.Body = r.Body
	}
	o.Ready = r.DeletedAt == nil && r.OpenBlockers == 0 && !claimActive(r) &&
		(r.Status == "open" || (r.Status == "in_progress" && r.ClaimExpires != nil))
	return o
}

func (c *call) outs(rs []*taskRow) []TaskOut {
	out := make([]TaskOut, 0, len(rs))
	for _, r := range rs {
		out = append(out, c.out(r, false))
	}
	return out
}

// getTask loads a visible task; trashed tasks only when includeDeleted.
func (c *call) getTask(id string, includeDeleted bool, forUpdate bool) (*taskRow, error) {
	q := "t.id = $1 AND t.project_id::text = ANY($2)"
	if !includeDeleted {
		q += " AND t.deleted_at IS NULL"
	}
	if forUpdate {
		// Lock the row so concurrent writers serialize.
		if _, err := c.tx.Exec(c.ctx, `SELECT 1 FROM tasks WHERE id = $1 FOR UPDATE`, strings.TrimSpace(id)); err != nil {
			return nil, err
		}
	}
	rs, err := c.queryTasks(q, strings.TrimSpace(id), c.visible)
	if err != nil {
		return nil, err
	}
	if len(rs) == 0 {
		return nil, errf(ErrNotFound, "no task %q (or you can't see it)", id)
	}
	return rs[0], nil
}

const idAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

func randomID(prefix string, n int) string {
	var sb strings.Builder
	sb.WriteString(prefix)
	sb.WriteByte('-')
	for i := 0; i < n; i++ {
		k, _ := rand.Int(rand.Reader, big.NewInt(int64(len(idAlphabet))))
		sb.WriteByte(idAlphabet[k.Int64()])
	}
	return sb.String()
}

func parseDue(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t, nil
		}
	}
	return nil, errf(ErrInvalid, "due must be YYYY-MM-DD or RFC 3339, got %q", s)
}

func cleanLabels(ls []string) []string {
	out := []string{}
	for _, l := range ls {
		l = strings.TrimSpace(l)
		if l != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// ---- tools ----

type ContextOut struct {
	You          MemberOut    `json:"you"`
	Org          Org          `json:"org"`
	Projects     []ProjectOut `json:"projects"`
	AssignedToMe []TaskOut    `json:"assigned_to_me"`
	OwnerTasks   []TaskOut    `json:"owner_tasks,omitempty" jsonschema:"for agents: open tasks assigned to the human you act for"`
	ClaimedByMe  []TaskOut    `json:"claimed_by_me"`
	Ready        []TaskOut    `json:"ready"`
	InboxUnread  int          `json:"inbox_unread" jsonschema:"unread @mentions, assignments and approval requests; read them with inbox"`
	Guidelines   string       `json:"guidelines"`
}

const guidelines = `Handoff is the shared task list for this company's people and agents.
- Start with list_ready, then claim_task before working so nobody else picks it up; close_task when finished.
- Ask the user before creating more than a few tasks at once. Use external_id so a retried create_tasks never duplicates.
- Call what_changed to catch up on what others changed since your last call. Tasks marked unseen changed since you last opened them.
- Mention someone with @Name in a comment. Check inbox for mentions, assignments and approval requests, and mark_read once handled.
- update_task appends to the body by default. Replacing the whole body needs confirm=true; only do it when the user asked.
- trash moves a task to a 30-day trash (restore brings it back). Some agents need a human to approve deletes.
- Every change you make is recorded under your agent name, on behalf of the person you act for.`

func (s *Service) GetContext(ctx context.Context, p *Principal, _ struct{}) (ContextOut, error) {
	var out ContextOut
	err := s.run(ctx, p, func(c *call) error {
		out.You = c.memberOut(p.Member)
		out.Org = *p.Org
		out.Guidelines = guidelines
		var err error
		if out.InboxUnread, err = c.unreadCount(); err != nil {
			return err
		}
		if out.Projects, err = c.projectOuts(); err != nil {
			return err
		}
		open := " AND t.deleted_at IS NULL AND t.status NOT IN ('done', 'cancelled') AND t.project_id::text = ANY($2) ORDER BY t.priority, t.due_at NULLS LAST, t.created_at LIMIT 25"
		rs, err := c.queryTasks("t.assignee_id = $1"+open, p.Member.ID, c.visible)
		if err != nil {
			return err
		}
		out.AssignedToMe = c.outs(rs)
		if p.Owner != nil {
			rs, err = c.queryTasks("t.assignee_id = $1"+open, p.Owner.ID, c.visible)
			if err != nil {
				return err
			}
			out.OwnerTasks = c.outs(rs)
		}
		rs, err = c.queryTasks("t.claimed_by = $1 AND t.claim_expires_at > now()"+open, p.Member.ID, c.visible)
		if err != nil {
			return err
		}
		out.ClaimedByMe = c.outs(rs)
		rs, err = c.queryTasks(readyCond+" AND t.project_id::text = ANY($1) ORDER BY t.priority, t.due_at NULLS LAST, t.created_at LIMIT 10", c.visible)
		if err != nil {
			return err
		}
		out.Ready = c.outs(rs)
		return nil
	})
	return out, err
}

type ListReadyIn struct {
	Project  string `json:"project,omitempty" jsonschema:"project name or id; omit for all"`
	Assignee string `json:"assignee,omitempty" jsonschema:"'me', 'unassigned', a name, email or id"`
	Limit    int    `json:"limit,omitempty" jsonschema:"max results, default 20, max 100"`
}

type TasksOut struct {
	Tasks []TaskOut `json:"tasks"`
}

func clampLimit(n, def, max int) int {
	if n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func (s *Service) ListReady(ctx context.Context, p *Principal, in ListReadyIn) (TasksOut, error) {
	out := TasksOut{Tasks: []TaskOut{}}
	err := s.run(ctx, p, func(c *call) error {
		where := readyCond + " AND t.project_id::text = ANY($1)"
		args := []any{c.visible}
		if in.Project != "" {
			pr, err := c.resolveProject(in.Project)
			if err != nil {
				return err
			}
			args = append(args, pr.ID)
			where += fmt.Sprintf(" AND t.project_id = $%d", len(args))
		}
		w, a, err := c.assigneeFilter(in.Assignee, len(args))
		if err != nil {
			return err
		}
		where += w
		args = append(args, a...)
		args = append(args, clampLimit(in.Limit, 20, 100))
		where += fmt.Sprintf(" ORDER BY t.priority, t.due_at NULLS LAST, t.created_at LIMIT $%d", len(args))
		rs, err := c.queryTasks(where, args...)
		if err != nil {
			return err
		}
		out.Tasks = c.outs(rs)
		return nil
	})
	return out, err
}

func (c *call) assigneeFilter(s string, n int) (string, []any, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return "", nil, nil
	case "unassigned", "none":
		return " AND t.assignee_id IS NULL", nil, nil
	}
	m, err := c.resolveMember(s)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf(" AND t.assignee_id = $%d", n+1), []any{m.ID}, nil
}

type SearchTasksIn struct {
	Query       string   `json:"query,omitempty" jsonschema:"text to find in title or body, or a task id"`
	Project     string   `json:"project,omitempty"`
	Status      []string `json:"status,omitempty" jsonschema:"filter by status; default excludes done and cancelled"`
	Assignee    string   `json:"assignee,omitempty" jsonschema:"'me', 'unassigned', a name, email or id"`
	Label       string   `json:"label,omitempty"`
	IncludeDone bool     `json:"include_done,omitempty"`
	Trashed     bool     `json:"trashed,omitempty" jsonschema:"list trashed tasks instead"`
	IncludeBody bool     `json:"include_body,omitempty" jsonschema:"return each task's full body"`
	Limit       int      `json:"limit,omitempty" jsonschema:"default 50, max 500"`
	Offset      int      `json:"offset,omitempty"`
	Sort        string   `json:"sort,omitempty" jsonschema:"empty sorts by priority then due date; recent puts the most recently closed or changed first"`
}

func (s *Service) SearchTasks(ctx context.Context, p *Principal, in SearchTasksIn) (TasksOut, error) {
	out := TasksOut{Tasks: []TaskOut{}}
	err := s.run(ctx, p, func(c *call) error {
		where := "t.project_id::text = ANY($1)"
		args := []any{c.visible}
		add := func(cond string, v any) {
			args = append(args, v)
			where += fmt.Sprintf(cond, len(args))
		}
		if in.Trashed {
			where += " AND t.deleted_at IS NOT NULL"
		} else {
			where += " AND t.deleted_at IS NULL"
		}
		if q := strings.TrimSpace(in.Query); q != "" {
			add(" AND (t.id = $%[1]d OR t.title ILIKE '%%' || $%[1]d || '%%' OR t.body ILIKE '%%' || $%[1]d || '%%')", q)
		}
		if in.Project != "" {
			pr, err := c.resolveProject(in.Project)
			if err != nil {
				return err
			}
			add(" AND t.project_id = $%d", pr.ID)
		}
		if len(in.Status) > 0 {
			for _, st := range in.Status {
				if !slices.Contains(Statuses, st) {
					return errf(ErrInvalid, "unknown status %q", st)
				}
			}
			add(" AND t.status = ANY($%d)", in.Status)
		} else if !in.IncludeDone && !in.Trashed {
			where += " AND t.status NOT IN ('done', 'cancelled')"
		}
		if in.Label != "" {
			add(" AND $%d = ANY(t.labels)", in.Label)
		}
		w, a, err := c.assigneeFilter(in.Assignee, len(args))
		if err != nil {
			return err
		}
		where += w
		args = append(args, a...)
		order := "t.status IN ('done', 'cancelled'), t.priority, t.due_at NULLS LAST, t.created_at"
		switch in.Sort {
		case "":
		case "recent":
			order = "coalesce(t.closed_at, t.updated_at) DESC, t.created_at DESC"
		default:
			return errf(ErrInvalid, "sort must be empty or recent")
		}
		add(" ORDER BY "+order+" LIMIT $%d", clampLimit(in.Limit, 50, 500))
		add(" OFFSET $%d", max(in.Offset, 0))
		rs, err := c.queryTasks(where, args...)
		if err != nil {
			return err
		}
		for _, r := range rs {
			out.Tasks = append(out.Tasks, c.out(r, in.IncludeBody))
		}
		return nil
	})
	return out, err
}

type TaskIDIn struct {
	ID string `json:"id" jsonschema:"task id, e.g. acme-7f3a"`
}

type CommentOut struct {
	ID         string     `json:"id"`
	Author     *MemberRef `json:"author"`
	OnBehalfOf *MemberRef `json:"on_behalf_of,omitempty"`
	Body       string     `json:"body"`
	CreatedAt  string     `json:"created_at"`
}

type TaskDetailOut struct {
	Task     TaskOut      `json:"task"`
	Comments []CommentOut `json:"comments"`
	Events   []EventOut   `json:"events"`
	Here     []HereOut    `json:"here" jsonschema:"others who have this task open right now"`
}

func (s *Service) GetTask(ctx context.Context, p *Principal, in TaskIDIn) (TaskDetailOut, error) {
	out := TaskDetailOut{Comments: []CommentOut{}, Events: []EventOut{}}
	err := s.run(ctx, p, func(c *call) error {
		r, err := c.getTask(in.ID, true, false)
		if err != nil {
			return err
		}
		out.Task = c.out(r, true)
		out.Task.Unseen = false
		if err := c.markSeen(r.ID); err != nil {
			return err
		}
		if _, err := c.tx.Exec(c.ctx, `UPDATE notifications SET read_at = now() WHERE member_id = $1 AND task_id = $2 AND read_at IS NULL`, p.Member.ID, r.ID); err != nil {
			return err
		}
		if p.Member.IsAgent() {
			if err := c.markHere(r.ID, agentHere, nil); err != nil {
				return err
			}
		}
		if out.Here, err = c.here(r.ID); err != nil {
			return err
		}
		rows, err := c.tx.Query(c.ctx, `SELECT id, author_id::text, coalesce(on_behalf_of::text, ''), body, created_at
			FROM comments WHERE task_id = $1 ORDER BY created_at`, r.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var co CommentOut
			var author, obo string
			var at time.Time
			if err := rows.Scan(&co.ID, &author, &obo, &co.Body, &at); err != nil {
				rows.Close()
				return err
			}
			co.Author, co.OnBehalfOf, co.CreatedAt = c.ref(author), c.ref(obo), ts(&at)
			out.Comments = append(out.Comments, co)
		}
		rows.Close()
		out.Events, err = c.events("e.task_id = $1", []any{r.ID}, 200, true)
		return err
	})
	return out, err
}

type NewTask struct {
	Title      string   `json:"title"`
	Body       string   `json:"body,omitempty"`
	Project    string   `json:"project,omitempty" jsonschema:"project name or id; default Inbox"`
	Priority   *int     `json:"priority,omitempty" jsonschema:"0 (urgent) to 4 (someday), default 2"`
	Assignee   string   `json:"assignee,omitempty" jsonschema:"'me', 'owner', a name, email or id"`
	Due        string   `json:"due,omitempty" jsonschema:"YYYY-MM-DD"`
	Labels     []string `json:"labels,omitempty"`
	Parent     string   `json:"parent,omitempty" jsonschema:"parent task id"`
	BlockedBy  []string `json:"blocked_by,omitempty" jsonschema:"ids of tasks that must finish first; may reference external_ids from this batch"`
	ExternalID string   `json:"external_id,omitempty" jsonschema:"your stable id for this task; repeating it returns the existing task instead of a duplicate"`
}

type CreateTasksIn struct {
	Tasks []NewTask `json:"tasks" jsonschema:"1 to 50 tasks"`
}

type CreateTasksOut struct {
	Tasks   []TaskOut `json:"tasks"`
	Created int       `json:"created"`
	Existed int       `json:"existed" jsonschema:"tasks returned because their external_id already existed"`
	// Fresh[i] reports whether Tasks[i] was created by this call.
	Fresh []bool `json:"-"`
}

func (s *Service) CreateTasks(ctx context.Context, p *Principal, in CreateTasksIn) (CreateTasksOut, error) {
	out := CreateTasksOut{Tasks: []TaskOut{}}
	if len(in.Tasks) == 0 || len(in.Tasks) > 50 {
		return out, errf(ErrInvalid, "send between 1 and 50 tasks")
	}
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActCreate); err != nil {
			return err
		}
		ids := make([]string, len(in.Tasks))
		byExt := map[string]string{}
		fresh := make([]bool, len(in.Tasks))
		for i, nt := range in.Tasks {
			id, created, err := c.createOne(nt)
			if err != nil {
				return fmt.Errorf("task %d (%q): %w", i+1, nt.Title, err)
			}
			ids[i], fresh[i] = id, created
			if nt.ExternalID != "" {
				byExt[nt.ExternalID] = id
			}
		}
		for i, nt := range in.Tasks {
			if !fresh[i] {
				continue
			}
			for _, b := range nt.BlockedBy {
				if id, ok := byExt[b]; ok {
					b = id
				}
				if err := c.link(b, ids[i]); err != nil {
					return fmt.Errorf("task %d (%q): %w", i+1, nt.Title, err)
				}
			}
		}
		for i, id := range ids {
			r, err := c.getTask(id, false, false)
			if err != nil {
				return err
			}
			out.Tasks = append(out.Tasks, c.out(r, true))
			out.Fresh = append(out.Fresh, fresh[i])
			if fresh[i] {
				out.Created++
			} else {
				out.Existed++
			}
		}
		return nil
	})
	return out, err
}

func (c *call) createOne(nt NewTask) (string, bool, error) {
	title := strings.TrimSpace(nt.Title)
	if title == "" || len(title) > 300 {
		return "", false, errf(ErrInvalid, "title must be 1-300 characters")
	}
	if nt.ExternalID != "" {
		id, err := c.existingExternal(nt.ExternalID)
		if err != nil || id != "" {
			return id, false, err
		}
	}
	pr, err := c.resolveProject(nt.Project)
	if err != nil {
		return "", false, err
	}
	prio := 2
	if nt.Priority != nil {
		prio = *nt.Priority
	}
	if prio < 0 || prio > 4 {
		return "", false, errf(ErrInvalid, "priority must be 0-4")
	}
	var assignee *string
	if nt.Assignee != "" {
		m, err := c.resolveMember(nt.Assignee)
		if err != nil {
			return "", false, err
		}
		assignee = &m.ID
	}
	due, err := parseDue(nt.Due)
	if err != nil {
		return "", false, err
	}
	var parent, ext *string
	if nt.Parent != "" {
		pt, err := c.getTask(nt.Parent, false, false)
		if err != nil {
			return "", false, err
		}
		parent = &pt.ID
	}
	if nt.ExternalID != "" {
		ext = &nt.ExternalID
	}
	for n := 4; n <= 10; n++ {
		id := randomID(c.p.Org.IDPrefix, n)
		_, err := c.tx.Exec(c.ctx, `SAVEPOINT create_task`)
		if err != nil {
			return "", false, err
		}
		_, err = c.tx.Exec(c.ctx, `INSERT INTO tasks (org_id, id, project_id, title, body, priority, assignee_id, due_at, labels,
			parent_id, external_id, created_by, on_behalf_of) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			c.p.Org.ID, id, pr.ID, title, nt.Body, prio, assignee, due, cleanLabels(nt.Labels), parent, ext, c.p.Member.ID, c.p.onBehalfOf())
		if db.IsUniqueViolation(err) {
			if _, err := c.tx.Exec(c.ctx, `ROLLBACK TO SAVEPOINT create_task`); err != nil {
				return "", false, err
			}
			if ext != nil {
				// A concurrent call may have just created the same external_id.
				if id, err := c.existingExternal(*ext); err != nil || id != "" {
					return id, false, err
				}
			}
			continue
		}
		if err != nil {
			return "", false, err
		}
		if _, err := c.tx.Exec(c.ctx, `RELEASE SAVEPOINT create_task`); err != nil {
			return "", false, err
		}
		if assignee != nil {
			if err := c.notify(c.members[*assignee], "assigned", id, title); err != nil {
				return "", false, err
			}
		}
		return id, true, c.event(id, "created", map[string]any{"title": title, "project": pr.Name})
	}
	return "", false, errors.New("could not allocate a task id")
}

// existingExternal returns the task already holding externalID, or "" if none.
// A task outside the caller's projects is reported as a conflict, never returned.
func (c *call) existingExternal(externalID string) (string, error) {
	var id string
	err := c.tx.QueryRow(c.ctx, `SELECT id FROM tasks WHERE external_id = $1`, externalID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if _, err := c.getTask(id, true, false); err != nil {
		return "", errf(ErrConflict, "external_id %q is already used by a task you can't see", externalID)
	}
	return id, nil
}

type UpdateTaskIn struct {
	ID       string   `json:"id"`
	Title    *string  `json:"title,omitempty"`
	Body     *string  `json:"body,omitempty" jsonschema:"text to append (default) or the new body when body_mode=replace"`
	BodyMode string   `json:"body_mode,omitempty" jsonschema:"append (default) or replace"`
	Confirm  bool     `json:"confirm,omitempty" jsonschema:"required with body_mode=replace"`
	Status   string   `json:"status,omitempty" jsonschema:"open, in_progress or blocked; use close_task to finish"`
	Priority *int     `json:"priority,omitempty"`
	Assignee *string  `json:"assignee,omitempty" jsonschema:"'me', 'owner', a name, email or id; empty string unassigns"`
	Due      *string  `json:"due,omitempty" jsonschema:"YYYY-MM-DD; empty string clears"`
	Labels   []string `json:"labels,omitempty" jsonschema:"replaces the label set"`
	Project  string   `json:"project,omitempty" jsonschema:"move to this project"`
}

type TaskResult struct {
	Task TaskOut `json:"task"`
}

func (s *Service) UpdateTask(ctx context.Context, p *Principal, in UpdateTaskIn) (TaskResult, error) {
	var out TaskResult
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActUpdate); err != nil {
			return err
		}
		r, err := c.getTask(in.ID, false, true)
		var newAssignee *Member
		if err != nil {
			return err
		}
		changed := map[string]any{}
		set := []string{}
		args := []any{r.ID}
		put := func(col string, v any) {
			args = append(args, v)
			set = append(set, fmt.Sprintf("%s = $%d", col, len(args)))
		}
		if in.Title != nil {
			t := strings.TrimSpace(*in.Title)
			if t == "" || len(t) > 300 {
				return errf(ErrInvalid, "title must be 1-300 characters")
			}
			put("title", t)
			changed["title"] = t
		}
		if in.Body != nil {
			switch in.BodyMode {
			case "", "append":
				nb := r.Body
				if strings.TrimSpace(nb) != "" {
					nb += "\n\n"
				}
				put("body", nb+*in.Body)
				changed["body"] = "appended"
			case "replace":
				if !in.Confirm {
					return errf(ErrInvalid, "replacing the whole body discards the existing text; ask the user, then call again with confirm=true")
				}
				put("body", *in.Body)
				changed["body"] = "replaced"
			default:
				return errf(ErrInvalid, "body_mode must be append or replace")
			}
		}
		if in.Status != "" {
			switch in.Status {
			case "open", "in_progress", "blocked":
			case "done", "cancelled":
				return errf(ErrInvalid, "use close_task to mark a task %s", in.Status)
			default:
				return errf(ErrInvalid, "unknown status %q", in.Status)
			}
			if in.Status != r.Status {
				put("status", in.Status)
				changed["status"] = in.Status
				if r.Status == "done" || r.Status == "cancelled" {
					set = append(set, "closed_at = NULL", "close_reason = ''")
				}
			}
		}
		if in.Priority != nil {
			if *in.Priority < 0 || *in.Priority > 4 {
				return errf(ErrInvalid, "priority must be 0-4")
			}
			put("priority", *in.Priority)
			changed["priority"] = *in.Priority
		}
		if in.Assignee != nil {
			if strings.TrimSpace(*in.Assignee) == "" {
				put("assignee_id", nil)
				changed["assignee"] = nil
			} else {
				m, err := c.resolveMember(*in.Assignee)
				if err != nil {
					return err
				}
				put("assignee_id", m.ID)
				changed["assignee"] = m.Name
				if m.ID != r.AssigneeID {
					newAssignee = m
				}
			}
		}
		if in.Due != nil {
			d, err := parseDue(*in.Due)
			if err != nil {
				return err
			}
			put("due_at", d)
			changed["due"] = *in.Due
		}
		if in.Labels != nil {
			put("labels", cleanLabels(in.Labels))
			changed["labels"] = cleanLabels(in.Labels)
		}
		if in.Project != "" {
			pr, err := c.resolveProject(in.Project)
			if err != nil {
				return err
			}
			put("project_id", pr.ID)
			changed["project"] = pr.Name
			if pr.ID != r.ProjectID {
				changed["from_project_id"] = r.ProjectID
			}
		}
		if len(set) == 0 {
			return errf(ErrInvalid, "nothing to update")
		}
		if _, err := c.tx.Exec(c.ctx, "UPDATE tasks SET "+strings.Join(set, ", ")+", updated_at = now() WHERE id = $1", args...); err != nil {
			return err
		}
		if err := c.notify(newAssignee, "assigned", r.ID, r.Title); err != nil {
			return err
		}
		if err := c.event(r.ID, "updated", changed); err != nil {
			return err
		}
		r, err = c.getTask(r.ID, false, false)
		out.Task = c.out(r, true)
		return err
	})
	return out, err
}

type ClaimTaskIn struct {
	ID           string `json:"id"`
	LeaseMinutes int    `json:"lease_minutes,omitempty" jsonschema:"how long you hold it before others may take it; default 60, max 1440"`
}

func (s *Service) ClaimTask(ctx context.Context, p *Principal, in ClaimTaskIn) (TaskResult, error) {
	var out TaskResult
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActUpdate); err != nil {
			return err
		}
		r, err := c.getTask(in.ID, false, false)
		if err != nil {
			return err
		}
		lease := clampLimit(in.LeaseMinutes, 60, 1440)
		tag, err := c.tx.Exec(c.ctx, `UPDATE tasks t SET claimed_by = $2, claim_expires_at = now() + make_interval(mins => $3),
			status = 'in_progress', updated_at = now()
			WHERE t.id = $1 AND t.deleted_at IS NULL AND t.status IN ('open', 'in_progress')
			AND (t.claimed_by IS NULL OR t.claimed_by = $2 OR t.claim_expires_at < now())
			AND `+openBlockers+` = 0`, r.ID, p.Member.ID, lease)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			r, err = c.getTask(r.ID, false, false)
			if err != nil {
				return err
			}
			switch {
			case r.Status == "done" || r.Status == "cancelled":
				return errf(ErrConflict, "task %s is already %s", r.ID, r.Status)
			case r.Status == "blocked":
				return errf(ErrConflict, "task %s is marked blocked; set it to open first", r.ID)
			case claimActive(r):
				return errf(ErrConflict, "task %s is claimed by %s until %s", r.ID, c.ref(r.ClaimedBy).Name, ts(r.ClaimExpires))
			case r.OpenBlockers > 0:
				return errf(ErrConflict, "task %s is waiting on %s", r.ID, strings.Join(r.BlockedBy, ", "))
			}
			return errf(ErrConflict, "task %s can't be claimed right now", r.ID)
		}
		if err := c.event(r.ID, "claimed", map[string]any{"lease_minutes": lease}); err != nil {
			return err
		}
		if p.Member.IsAgent() {
			if err := c.markHere(r.ID, agentHere, nil); err != nil {
				return err
			}
		}
		r, err = c.getTask(r.ID, false, false)
		out.Task = c.out(r, true)
		return err
	})
	return out, err
}

type ReleaseTaskIn struct {
	ID   string `json:"id"`
	Note string `json:"note,omitempty" jsonschema:"optional comment explaining where you left it"`
}

func (s *Service) ReleaseTask(ctx context.Context, p *Principal, in ReleaseTaskIn) (TaskResult, error) {
	var out TaskResult
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActUpdate); err != nil {
			return err
		}
		r, err := c.getTask(in.ID, false, true)
		if err != nil {
			return err
		}
		if claimActive(r) && r.ClaimedBy != p.Member.ID && !p.IsAdmin() && !(p.Owner == nil && c.members[r.ClaimedBy] != nil && c.members[r.ClaimedBy].OwnerID == p.Member.ID) {
			return errf(ErrForbidden, "task %s is claimed by %s", r.ID, c.ref(r.ClaimedBy).Name)
		}
		if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET claimed_by = NULL, claim_expires_at = NULL,
			status = CASE WHEN status = 'in_progress' THEN 'open' ELSE status END, updated_at = now() WHERE id = $1`, r.ID); err != nil {
			return err
		}
		if err := c.event(r.ID, "released", nil); err != nil {
			return err
		}
		if strings.TrimSpace(in.Note) != "" {
			if err := c.addComment(r.ID, in.Note); err != nil {
				return err
			}
		}
		r, err = c.getTask(r.ID, false, false)
		out.Task = c.out(r, true)
		return err
	})
	return out, err
}

type CloseTaskIn struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty" jsonschema:"done (default) or cancelled"`
	Reason string `json:"reason,omitempty" jsonschema:"short note on the outcome"`
}

type CloseTaskOut struct {
	Task      TaskOut   `json:"task"`
	Unblocked []TaskOut `json:"unblocked" jsonschema:"tasks that became ready because this one closed"`
}

func (s *Service) CloseTask(ctx context.Context, p *Principal, in CloseTaskIn) (CloseTaskOut, error) {
	out := CloseTaskOut{Unblocked: []TaskOut{}}
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActClose); err != nil {
			return err
		}
		st := in.Status
		if st == "" {
			st = "done"
		}
		if st != "done" && st != "cancelled" {
			return errf(ErrInvalid, "status must be done or cancelled")
		}
		r, err := c.getTask(in.ID, false, true)
		if err != nil {
			return err
		}
		if p.Member.IsAgent() && claimActive(r) && r.ClaimedBy != p.Member.ID {
			return errf(ErrConflict, "task %s is claimed by %s", r.ID, c.ref(r.ClaimedBy).Name)
		}
		if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET status = $2, close_reason = $3, closed_at = now(),
			claimed_by = NULL, claim_expires_at = NULL, updated_at = now() WHERE id = $1`, r.ID, st, in.Reason); err != nil {
			return err
		}
		if err := c.event(r.ID, "closed", map[string]any{"status": st, "reason": in.Reason}); err != nil {
			return err
		}
		r, err = c.getTask(r.ID, false, false)
		if err != nil {
			return err
		}
		out.Task = c.out(r, true)
		if len(r.Blocks) > 0 {
			rs, err := c.queryTasks(readyCond+" AND t.id = ANY($1) AND t.project_id::text = ANY($2)", r.Blocks, c.visible)
			if err != nil {
				return err
			}
			out.Unblocked = c.outs(rs)
		}
		return nil
	})
	return out, err
}

type CommentIn struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}

func (s *Service) Comment(ctx context.Context, p *Principal, in CommentIn) (TaskDetailOut, error) {
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActUpdate); err != nil {
			return err
		}
		r, err := c.getTask(in.ID, false, false)
		if err != nil {
			return err
		}
		return c.addComment(r.ID, in.Body)
	})
	if err != nil {
		return TaskDetailOut{}, err
	}
	return s.GetTask(ctx, p, TaskIDIn{ID: in.ID})
}

func (c *call) addComment(taskID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 20000 {
		return errf(ErrInvalid, "comment must be 1-20000 characters")
	}
	if _, err := c.tx.Exec(c.ctx, `INSERT INTO comments (org_id, task_id, author_id, on_behalf_of, body) VALUES ($1, $2, $3, $4, $5)`,
		c.p.Org.ID, taskID, c.p.Member.ID, c.p.onBehalfOf(), body); err != nil {
		return err
	}
	if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET updated_at = now() WHERE id = $1`, taskID); err != nil {
		return err
	}
	for _, m := range c.mentioned(body) {
		if err := c.notify(m, "mention", taskID, body); err != nil {
			return err
		}
	}
	return c.event(taskID, "commented", map[string]any{"excerpt": excerpt(body)})
}

func excerpt(s string) string {
	if r := []rune(s); len(r) > 140 {
		return string(r[:140]) + "…"
	}
	return s
}

type LinkIn struct {
	Blocker string `json:"blocker" jsonschema:"task that must finish first"`
	Blocked string `json:"blocked" jsonschema:"task that waits for it"`
	Remove  bool   `json:"remove,omitempty" jsonschema:"remove the dependency instead"`
}

type LinkOut struct {
	Blocker TaskOut `json:"blocker"`
	Blocked TaskOut `json:"blocked"`
}

func (s *Service) Link(ctx context.Context, p *Principal, in LinkIn) (LinkOut, error) {
	var out LinkOut
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActUpdate); err != nil {
			return err
		}
		if in.Remove {
			a, err := c.getTask(in.Blocker, false, false)
			if err != nil {
				return err
			}
			b, err := c.getTask(in.Blocked, false, false)
			if err != nil {
				return err
			}
			if _, err := c.tx.Exec(c.ctx, `DELETE FROM task_deps WHERE blocker_id = $1 AND blocked_id = $2`, a.ID, b.ID); err != nil {
				return err
			}
			if err := c.event(b.ID, "unlinked", map[string]any{"blocker": a.ID}); err != nil {
				return err
			}
		} else if err := c.link(in.Blocker, in.Blocked); err != nil {
			return err
		}
		a, err := c.getTask(in.Blocker, false, false)
		if err != nil {
			return err
		}
		b, err := c.getTask(in.Blocked, false, false)
		if err != nil {
			return err
		}
		out.Blocker, out.Blocked = c.out(a, false), c.out(b, false)
		return nil
	})
	return out, err
}

func (c *call) link(blocker, blocked string) error {
	a, err := c.getTask(blocker, false, false)
	if err != nil {
		return err
	}
	b, err := c.getTask(blocked, false, false)
	if err != nil {
		return err
	}
	if a.ID == b.ID {
		return errf(ErrInvalid, "a task can't block itself")
	}
	// Serialize dependency changes per workspace so concurrent links can't form a cycle.
	if _, err := c.tx.Exec(c.ctx, `SELECT pg_advisory_xact_lock(hashtext('task_deps:' || $1))`, c.p.Org.ID); err != nil {
		return err
	}
	var cycle bool
	if err := c.tx.QueryRow(c.ctx, `WITH RECURSIVE up(id) AS (
			SELECT blocker_id FROM task_deps WHERE blocked_id = $1
			UNION SELECT d.blocker_id FROM task_deps d JOIN up ON d.blocked_id = up.id)
		SELECT EXISTS (SELECT 1 FROM up WHERE id = $2)`, a.ID, b.ID).Scan(&cycle); err != nil {
		return err
	}
	if cycle {
		return errf(ErrInvalid, "%s already waits on %s; linking would create a cycle", a.ID, b.ID)
	}
	tag, err := c.tx.Exec(c.ctx, `INSERT INTO task_deps (org_id, blocker_id, blocked_id, created_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, c.p.Org.ID, a.ID, b.ID, c.p.Member.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return c.event(b.ID, "linked", map[string]any{"blocker": a.ID})
}

type TrashIn struct {
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
}

type TrashOut struct {
	Status     string   `json:"status" jsonschema:"trashed, or pending_approval when a human must approve"`
	ApprovalID string   `json:"approval_id,omitempty"`
	Task       *TaskOut `json:"task,omitempty"`
	Message    string   `json:"message"`
}

func (s *Service) Trash(ctx context.Context, p *Principal, in TrashIn) (TrashOut, error) {
	var out TrashOut
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActDelete); err != nil {
			return err
		}
		r, err := c.getTask(in.ID, false, true)
		if err != nil {
			return err
		}
		if p.needsApproval(ActDelete) {
			var id string
			b := fmt.Sprintf(`{"reason": %q}`, in.Reason)
			if err := c.tx.QueryRow(c.ctx, `INSERT INTO approvals (org_id, agent_id, action, task_id, payload)
				VALUES ($1, $2, 'trash', $3, $4) RETURNING id`, p.Org.ID, p.Member.ID, r.ID, b).Scan(&id); err != nil {
				return err
			}
			why := "wants to move this task to the trash"
			if in.Reason != "" {
				why += ": " + in.Reason
			}
			if err := c.notify(p.Owner, "approval", r.ID, why); err != nil {
				return err
			}
			out = TrashOut{Status: "pending_approval", ApprovalID: id,
				Message: fmt.Sprintf("Deleting needs approval from %s; it's queued in Handoff's Agents page.", p.ownerName())}
			return c.event(r.ID, "approval_requested", map[string]any{"action": "trash", "approval_id": id, "reason": in.Reason})
		}
		if err := c.trash(r.ID, in.Reason); err != nil {
			return err
		}
		r, err = c.getTask(r.ID, true, false)
		if err != nil {
			return err
		}
		o := c.out(r, false)
		out = TrashOut{Status: "trashed", Task: &o, Message: "Moved to trash; restore within 30 days."}
		return nil
	})
	return out, err
}

func (c *call) trash(id, reason string) error {
	if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET deleted_at = now(), deleted_by = $2, claimed_by = NULL,
		claim_expires_at = NULL, updated_at = now() WHERE id = $1`, id, c.p.Member.ID); err != nil {
		return err
	}
	return c.event(id, "trashed", map[string]any{"reason": reason})
}

func (s *Service) Restore(ctx context.Context, p *Principal, in TaskIDIn) (TaskResult, error) {
	var out TaskResult
	err := s.run(ctx, p, func(c *call) error {
		if err := p.require(ActDelete); err != nil {
			return err
		}
		r, err := c.getTask(in.ID, true, true)
		if err != nil {
			return err
		}
		if r.DeletedAt == nil {
			return errf(ErrInvalid, "task %s is not in the trash", r.ID)
		}
		if _, err := c.tx.Exec(c.ctx, `UPDATE tasks SET deleted_at = NULL, deleted_by = NULL, updated_at = now() WHERE id = $1`, r.ID); err != nil {
			return err
		}
		if err := c.event(r.ID, "restored", nil); err != nil {
			return err
		}
		r, err = c.getTask(r.ID, false, false)
		out.Task = c.out(r, true)
		return err
	})
	return out, err
}
