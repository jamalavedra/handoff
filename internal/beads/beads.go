// Package beads converts between Handoff tasks and beads (bd) JSONL issues,
// so a repo's bd list and a company's Handoff list can trade work.
package beads

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/joalavedra/handoff/internal/core"
)

type Dependency struct {
	IssueID     string `json:"issue_id"`
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"`
}

type Comment struct {
	Author    string    `json:"author"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type Issue struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Description  string       `json:"description,omitempty"`
	Status       string       `json:"status"`
	Priority     int          `json:"priority"`
	IssueType    string       `json:"issue_type,omitempty"`
	Assignee     string       `json:"assignee,omitempty"`
	Labels       []string     `json:"labels,omitempty"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
	Comments     []Comment    `json:"comments,omitempty"`
	ExternalRef  string       `json:"external_ref,omitempty"`
	DueAt        *time.Time   `json:"due_at,omitempty"`
	CreatedAt    *time.Time   `json:"created_at,omitempty"`
	UpdatedAt    *time.Time   `json:"updated_at,omitempty"`
	ClosedAt     *time.Time   `json:"closed_at,omitempty"`
	CloseReason  string       `json:"close_reason,omitempty"`
}

// ExtPrefix marks tasks that came from beads, so re-importing is idempotent.
const ExtPrefix = "bd:"

// CancelledLabel marks a closed bd issue as cancelled, since bd has no cancelled status.
const CancelledLabel = "cancelled"

type ImportResult struct {
	Created int `json:"created"`
	Existed int `json:"existed"`
	Linked  int `json:"linked"`
	Closed  int `json:"closed"`
}

// Import reads bd JSONL and creates the tasks in project as principal p.
func Import(ctx context.Context, s *core.Service, p *core.Principal, project string, r io.Reader) (ImportResult, error) {
	var res ImportResult
	var issues []Issue
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var is Issue
		if err := json.Unmarshal([]byte(line), &is); err != nil {
			return res, fmt.Errorf("line %d: %w", n, err)
		}
		if is.ID == "" || is.Title == "" {
			return res, fmt.Errorf("line %d: id and title are required", n)
		}
		issues = append(issues, is)
	}
	if err := sc.Err(); err != nil {
		return res, err
	}
	members, err := s.ListMembers(ctx, p, struct{}{})
	if err != nil {
		return res, err
	}
	known := map[string]bool{}
	for _, m := range members.Members {
		known[strings.ToLower(m.Name)], known[strings.ToLower(m.Email)] = true, true
	}
	ids := map[string]string{}
	for i := 0; i < len(issues); i += 50 {
		batch := issues[i:min(i+50, len(issues))]
		var nts []core.NewTask
		for _, is := range batch {
			prio := min(max(is.Priority, 0), 4)
			labels := append([]string{}, is.Labels...)
			if is.IssueType != "" && is.IssueType != "task" {
				labels = append(labels, is.IssueType)
			}
			nt := core.NewTask{Title: is.Title, Body: is.Description, Project: project, Priority: &prio, Labels: labels, ExternalID: ExtPrefix + is.ID}
			if a := strings.ToLower(is.Assignee); a != "" && known[a] {
				nt.Assignee = is.Assignee
			} else if is.Assignee != "" {
				nt.Labels = append(nt.Labels, "bd-assignee:"+is.Assignee)
			}
			if is.DueAt != nil {
				nt.Due = is.DueAt.Format("2006-01-02")
			}
			nts = append(nts, nt)
		}
		out, err := s.CreateTasks(ctx, p, core.CreateTasksIn{Tasks: nts})
		if err != nil {
			return res, err
		}
		res.Created += out.Created
		res.Existed += out.Existed
		for j, t := range out.Tasks {
			ids[batch[j].ID] = t.ID
			if !out.Fresh[j] {
				continue
			}
			for _, c := range batch[j].Comments {
				body := c.Text
				if c.Author != "" {
					body = "**" + c.Author + "** (from beads): " + body
				}
				if _, err := s.Comment(ctx, p, core.CommentIn{ID: t.ID, Body: body}); err != nil {
					return res, err
				}
			}
		}
	}
	for _, is := range issues {
		for _, d := range is.Dependencies {
			if d.Type != "" && d.Type != "blocks" {
				continue
			}
			blocker, ok1 := ids[d.DependsOnID]
			blocked, ok2 := ids[d.IssueID]
			if d.IssueID == "" {
				blocked, ok2 = ids[is.ID], true
			}
			if !ok1 || !ok2 {
				continue
			}
			if _, err := s.Link(ctx, p, core.LinkIn{Blocker: blocker, Blocked: blocked}); err != nil && !core.IsCode(err, core.ErrInvalid) {
				return res, err
			}
			res.Linked++
		}
	}
	for _, is := range issues {
		st := mapStatusIn(is.Status)
		if st == "done" && slices.Contains(is.Labels, CancelledLabel) {
			st = "cancelled"
		}
		if st != "done" && st != "cancelled" {
			if st == "in_progress" {
				_, _ = s.UpdateTask(ctx, p, core.UpdateTaskIn{ID: ids[is.ID], Status: st})
			}
			continue
		}
		d, err := s.GetTask(ctx, p, core.TaskIDIn{ID: ids[is.ID]})
		if err != nil {
			return res, err
		}
		if d.Task.Status == st {
			continue
		}
		if _, err := s.CloseTask(ctx, p, core.CloseTaskIn{ID: ids[is.ID], Status: st, Reason: is.CloseReason}); err != nil {
			return res, err
		}
		res.Closed++
	}
	return res, nil
}

func mapStatusIn(s string) string {
	switch s {
	case "closed":
		return "done"
	case "tombstone":
		return "cancelled"
	case "in_progress":
		return s
	}
	return "open" // blocked too: waiting is a dependency here, not a status
}

// Export writes every task the principal can see (including done) as bd JSONL.
func Export(ctx context.Context, s *core.Service, p *core.Principal, project string, w io.Writer) (int, error) {
	var all []core.TaskOut
	for offset := 0; ; offset += 500 {
		res, err := s.SearchTasks(ctx, p, core.SearchTasksIn{Project: project, IncludeDone: true, IncludeBody: true, Limit: 500, Offset: offset})
		if err != nil {
			return 0, err
		}
		all = append(all, res.Tasks...)
		if len(res.Tasks) < 500 {
			break
		}
	}
	ids := map[string]string{}
	for _, t := range all {
		ids[t.ID] = bdID(t)
	}
	enc := json.NewEncoder(w)
	for _, t := range all {
		is := toIssue(t)
		d, err := s.GetTask(ctx, p, core.TaskIDIn{ID: t.ID})
		if err != nil {
			return 0, err
		}
		for _, c := range d.Comments {
			cm := Comment{Text: c.Body}
			if c.Author != nil {
				cm.Author = c.Author.Name
			}
			if at, err := time.Parse(time.RFC3339, c.CreatedAt); err == nil {
				cm.CreatedAt = at
			}
			is.Comments = append(is.Comments, cm)
		}
		for _, b := range t.BlockedBy {
			if id, ok := ids[b]; ok {
				b = id
			}
			is.Dependencies = append(is.Dependencies, Dependency{IssueID: is.ID, DependsOnID: b, Type: "blocks"})
		}
		if err := enc.Encode(is); err != nil {
			return 0, err
		}
	}
	return len(all), nil
}

func bdID(t core.TaskOut) string {
	if id, ok := strings.CutPrefix(t.ExternalID, ExtPrefix); ok {
		return id
	}
	return t.ID
}

func toIssue(t core.TaskOut) Issue {
	is := Issue{ID: bdID(t), Title: t.Title, Description: t.Body, Status: t.Status, Priority: t.Priority, IssueType: "task", Labels: t.Labels,
		ExternalRef: t.URL, CloseReason: t.CloseReason}
	if t.Status == "done" || t.Status == "cancelled" {
		is.Status = "closed"
	}
	if t.Status == "cancelled" && !slices.Contains(is.Labels, CancelledLabel) {
		is.Labels = append(slices.Clone(is.Labels), CancelledLabel)
	}
	if t.Assignee != nil {
		is.Assignee = t.Assignee.Name
	}
	parse := func(s string) *time.Time {
		if v, err := time.Parse(time.RFC3339, s); err == nil {
			return &v
		}
		if v, err := time.Parse("2006-01-02", s); err == nil {
			return &v
		}
		return nil
	}
	is.CreatedAt, is.UpdatedAt, is.ClosedAt, is.DueAt = parse(t.CreatedAt), parse(t.UpdatedAt), parse(t.ClosedAt), parse(t.Due)
	return is
}
