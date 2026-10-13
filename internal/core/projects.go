package core

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/db"
)

type Project struct {
	ID          string
	Name        string
	Description string
	Visibility  string
	ArchivedAt  *time.Time
	createdBy   string
	memberIDs   []string
}

type ProjectOut struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Visibility  string   `json:"visibility"`
	Members     []string `json:"members,omitempty"`
	OpenTasks   int      `json:"open_tasks"`
}

func listProjects(ctx context.Context, tx pgx.Tx) ([]*Project, error) {
	rows, err := tx.Query(ctx, `SELECT p.id, p.name, p.description, p.visibility, p.archived_at, coalesce(p.created_by::text, ''),
		coalesce(array_agg(pm.member_id::text) FILTER (WHERE pm.member_id IS NOT NULL), '{}')
		FROM projects p LEFT JOIN project_members pm ON pm.org_id = p.org_id AND pm.project_id = p.id
		GROUP BY p.id ORDER BY p.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		p := &Project{}
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Visibility, &p.ArchivedAt, &p.createdBy, &p.memberIDs); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (c *call) projectOuts() ([]ProjectOut, error) {
	counts := map[string]int{}
	rows, err := c.tx.Query(c.ctx, `SELECT project_id::text, count(*) FROM tasks
		WHERE deleted_at IS NULL AND status NOT IN ('done', 'cancelled') GROUP BY project_id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		counts[id] = n
	}
	rows.Close()
	out := []ProjectOut{}
	for _, id := range c.visible {
		p := c.projects[id]
		po := ProjectOut{ID: p.ID, Name: p.Name, Description: p.Description, Visibility: p.Visibility, OpenTasks: counts[p.ID]}
		if p.Visibility == "private" {
			for _, m := range p.memberIDs {
				if r := c.ref(m); r != nil {
					po.Members = append(po.Members, r.Name)
				}
			}
		}
		out = append(out, po)
	}
	return out, nil
}

// resolveProject accepts a project id or name; empty picks the default.
func (c *call) resolveProject(s string) (*Project, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		for _, id := range c.visible {
			if strings.EqualFold(c.projects[id].Name, DefaultProject) {
				return c.projects[id], nil
			}
		}
		if len(c.visible) > 0 {
			return c.projects[c.visible[0]], nil
		}
		return nil, errf(ErrForbidden, "no project is visible to you")
	}
	for _, id := range c.visible {
		p := c.projects[id]
		if p.ID == s || strings.EqualFold(p.Name, s) {
			return p, nil
		}
	}
	return nil, errf(ErrNotFound, "no project %q (or you can't see it)", s)
}

const DefaultProject = "Inbox"

type ListProjectsOut struct {
	Projects []ProjectOut `json:"projects"`
}

func (s *Service) ListProjects(ctx context.Context, p *Principal, _ struct{}) (ListProjectsOut, error) {
	var out ListProjectsOut
	err := s.run(ctx, p, func(c *call) error {
		var err error
		out.Projects, err = c.projectOuts()
		return err
	})
	return out, err
}

type CreateProjectIn struct {
	Name        string   `json:"name" jsonschema:"project name, unique in the company"`
	Description string   `json:"description,omitempty"`
	Visibility  string   `json:"visibility,omitempty" jsonschema:"company (default) or private"`
	Members     []string `json:"members,omitempty" jsonschema:"for private projects: member names, emails or ids"`
}

func (s *Service) CreateProject(ctx context.Context, p *Principal, in CreateProjectIn) (ProjectOut, error) {
	var out ProjectOut
	err := s.run(ctx, p, func(c *call) error {
		if p.Member.IsAgent() {
			return errf(ErrForbidden, "only people can create projects")
		}
		name := strings.TrimSpace(in.Name)
		if name == "" || len(name) > 80 {
			return errf(ErrInvalid, "project name must be 1-80 characters")
		}
		vis := in.Visibility
		if vis == "" {
			vis = "company"
		}
		if vis != "company" && vis != "private" {
			return errf(ErrInvalid, "visibility must be company or private")
		}
		var id string
		err := c.tx.QueryRow(c.ctx, `INSERT INTO projects (org_id, name, description, visibility, created_by)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`, p.Org.ID, name, in.Description, vis, p.Member.ID).Scan(&id)
		if db.IsUniqueViolation(err) {
			return errf(ErrConflict, "a project called %q already exists", name)
		}
		if err != nil {
			return err
		}
		ids := []string{p.Member.ID}
		for _, m := range in.Members {
			mm, err := c.resolveMember(m)
			if err != nil {
				return err
			}
			ids = append(ids, mm.ID)
		}
		if vis == "private" {
			for _, mid := range ids {
				if _, err := c.tx.Exec(c.ctx, `INSERT INTO project_members (org_id, project_id, member_id) VALUES ($1, $2, $3)
					ON CONFLICT DO NOTHING`, p.Org.ID, id, mid); err != nil {
					return err
				}
			}
		}
		if err := c.event("", "project_created", map[string]any{"project_id": id, "name": name, "visibility": vis}); err != nil {
			return err
		}
		if err := c.loadProjects(); err != nil {
			return err
		}
		outs, err := c.projectOuts()
		for _, po := range outs {
			if po.ID == id {
				out = po
			}
		}
		return err
	})
	return out, err
}
