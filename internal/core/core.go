// Package core is Handoff's business layer. Every tool, whether called over MCP
// or by the web app, goes through a Service method with a Principal.
package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/db"
)

// Actions an agent can be granted.
const (
	ActRead   = "read"
	ActCreate = "create"
	ActUpdate = "update"
	ActClose  = "close"
	ActDelete = "delete"
)

var AllActions = []string{ActRead, ActCreate, ActUpdate, ActClose, ActDelete}

// Error is a user-facing error: its message is safe to show to callers.
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func errf(code, format string, a ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

var (
	ErrNotFound  = "not_found"
	ErrForbidden = "forbidden"
	ErrInvalid   = "invalid"
	ErrConflict  = "conflict"
)

// IsCode reports whether err is a core Error with the given code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

type Org struct {
	ID                 string `json:"id"`
	Slug               string `json:"slug"`
	Name               string `json:"name"`
	IDPrefix           string `json:"id_prefix"`
	CustomDomain       string `json:"custom_domain,omitempty"`
	AllowedEmailDomain string `json:"allowed_email_domain,omitempty"`
}

type Permissions struct {
	// Projects is a list of project IDs, or ["*"] for every project the owner can see.
	Projects []string `json:"projects"`
	Actions  []string `json:"actions"`
	// Approval lists actions that are queued for a human instead of executed.
	Approval []string `json:"approval"`
}

func DefaultAgentPermissions() Permissions {
	return Permissions{Projects: []string{"*"}, Actions: []string{ActRead, ActCreate, ActUpdate, ActClose, ActDelete}, Approval: []string{ActDelete}}
}

func (p Permissions) normalized() Permissions {
	if p.Projects == nil {
		p.Projects = []string{}
	}
	if p.Actions == nil {
		p.Actions = []string{}
	}
	if p.Approval == nil {
		p.Approval = []string{}
	}
	return p
}

type Member struct {
	ID          string
	Kind        string
	Name        string
	Email       string
	Role        string
	Status      string
	OwnerID     string
	ClientID    string
	Permissions Permissions
	CreatedAt   time.Time
}

func (m *Member) IsAgent() bool { return m.Kind == "agent" }

// Principal is who is calling: a human (web session) or an agent (MCP/API).
type Principal struct {
	Org    *Org
	Member *Member
	// Owner is the human an agent acts for; nil for humans.
	Owner *Member
	// Via is the channel: web, mcp or api.
	Via string
	// within, when set, further limits the projects this principal sees (the assistant: the person's own).
	within map[string]bool
}

// ViaAssistant marks calls the in-app assistant makes as the triage agent. Its reads leave no trace.
const ViaAssistant = "assistant"

func (p *Principal) IsAdmin() bool { return !p.Member.IsAgent() && p.Member.Role == "admin" }

func (p *Principal) onBehalfOf() *string {
	if p.Owner != nil {
		return &p.Owner.ID
	}
	return nil
}

// can reports whether the principal may perform action at all (project scope is separate).
func (p *Principal) can(action string) bool {
	if !p.Member.IsAgent() {
		return true
	}
	return slices.Contains(p.Member.Permissions.Actions, action)
}

func (p *Principal) require(action string) error {
	if !p.can(action) {
		return errf(ErrForbidden, "this agent is not allowed to %s tasks; ask %s to grant the %q permission", action, p.ownerName(), action)
	}
	return nil
}

func (p *Principal) needsApproval(action string) bool {
	return p.Member.IsAgent() && slices.Contains(p.Member.Permissions.Approval, action)
}

func (p *Principal) ownerName() string {
	if p.Owner != nil {
		return p.Owner.Name
	}
	return "an admin"
}

type Service struct {
	DB  *db.DB
	Cfg *config.Config
	// OutboundHTTP, when set, replaces the client for triage models and agent webhooks (tests).
	OutboundHTTP *http.Client
	triageJobs   chan triageJob
	replyJobs    chan replyJob
	// outboundHTTP is the shared guarded client, built on first use.
	outboundHTTP *http.Client
	outboundOnce sync.Once
	// assistant turns in progress or recently finished, so a retried turn resumes instead of repeating writes.
	turnsMu sync.Mutex
	turns   map[string]*assistantTurn
}

func New(d *db.DB, cfg *config.Config) *Service {
	return &Service{DB: d, Cfg: cfg, triageJobs: make(chan triageJob, 1024), replyJobs: make(chan replyJob, 256)}
}

// call wraps a principal-scoped transaction and loads the per-call view of the org.
type call struct {
	ctx     context.Context
	tx      pgx.Tx
	p       *Principal
	s       *Service
	members map[string]*Member
	// visible is the set of project IDs the principal may see.
	visible  []string
	projects map[string]*Project
	// triage is set while creating tasks that the workspace's triage agent should evaluate.
	triage bool
	// mine is set while a person creates tasks in the web app: unassigned ones go to them.
	mine bool
	// after runs once the transaction has committed (e.g. waking a background worker).
	after []func()
}

func (s *Service) run(ctx context.Context, p *Principal, fn func(c *call) error) error {
	if p == nil || p.Org == nil || p.Member == nil {
		return errf(ErrForbidden, "not authenticated")
	}
	var after []func()
	err := s.DB.InOrg(ctx, p.Org.ID, func(tx pgx.Tx) error {
		c := &call{ctx: ctx, tx: tx, p: p, s: s}
		defer func() { after = c.after }()
		if _, err := tx.Exec(ctx, `SELECT set_config('app.member_id', $1, true)`, p.Member.ID); err != nil {
			return err
		}
		if err := c.load(); err != nil {
			return err
		}
		if p.Member.IsAgent() && !p.can(ActRead) {
			return errf(ErrForbidden, "this agent has no read access")
		}
		return fn(c)
	})
	// Only once the changes are committed.
	if err == nil {
		for _, f := range after {
			f()
		}
	}
	return err
}

const memberCols = `id, kind, name, coalesce(email, ''), role, status, coalesce(owner_id::text, ''), coalesce(oauth_client_id, ''), permissions, created_at`

func scanMember(row pgx.Row) (*Member, error) {
	m := &Member{}
	var perms []byte
	if err := row.Scan(&m.ID, &m.Kind, &m.Name, &m.Email, &m.Role, &m.Status, &m.OwnerID, &m.ClientID, &perms, &m.CreatedAt); err != nil {
		return nil, err
	}
	if len(perms) > 0 {
		_ = json.Unmarshal(perms, &m.Permissions)
	}
	m.Permissions = m.Permissions.normalized()
	return m, nil
}

func (c *call) load() error {
	rows, err := c.tx.Query(c.ctx, `SELECT `+memberCols+` FROM members`)
	if err != nil {
		return err
	}
	c.members = map[string]*Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			rows.Close()
			return err
		}
		c.members[m.ID] = m
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	return c.loadProjects()
}

func (c *call) loadProjects() error {
	ps, err := listProjects(c.ctx, c.tx)
	if err != nil {
		return err
	}
	c.projects = map[string]*Project{}
	c.visible = nil
	human := c.p.Member
	if c.p.Owner != nil {
		human = c.p.Owner
	}
	admin := human.Role == "admin"
	for _, pr := range ps {
		c.projects[pr.ID] = pr
		if pr.ArchivedAt != nil {
			continue
		}
		ok := pr.Visibility == "company" || admin || slices.Contains(pr.memberIDs, human.ID) || pr.createdBy == human.ID
		if ok && c.p.Member.IsAgent() {
			perm := c.p.Member.Permissions.Projects
			ok = slices.Contains(perm, "*") || slices.Contains(perm, pr.ID)
		}
		if ok && c.p.within != nil {
			ok = c.p.within[pr.ID]
		}
		if ok {
			c.visible = append(c.visible, pr.ID)
		}
	}
	if c.visible == nil {
		c.visible = []string{}
	}
	return nil
}

func (c *call) canSeeProject(id string) bool { return slices.Contains(c.visible, id) }

// event appends to the audit log.
func (c *call) event(taskID, kind string, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	b, _ := json.Marshal(data)
	var tid *string
	if taskID != "" {
		tid = &taskID
	}
	_, err := c.tx.Exec(c.ctx, `INSERT INTO events (org_id, task_id, actor_id, on_behalf_of, via, kind, data)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, c.p.Org.ID, tid, c.p.Member.ID, c.p.onBehalfOf(), c.p.Via, kind, b)
	return err
}

// MemberRef is how other members appear in outputs.
type MemberRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func (c *call) ref(id string) *MemberRef {
	if id == "" {
		return nil
	}
	m, ok := c.members[id]
	if !ok {
		return &MemberRef{ID: id, Name: "unknown", Kind: "human"}
	}
	return &MemberRef{ID: m.ID, Name: m.Name, Kind: m.Kind}
}

// resolveMember accepts "me", "owner", a member id, an email or a name.
func (c *call) resolveMember(s string) (*Member, error) {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "me":
		return c.p.Member, nil
	case "owner":
		if c.p.Owner != nil {
			return c.p.Owner, nil
		}
		return c.p.Member, nil
	}
	if m, ok := c.members[s]; ok && m.Status != "revoked" {
		return m, nil
	}
	var match *Member
	for _, m := range c.members {
		if m.Status == "revoked" {
			continue
		}
		if strings.EqualFold(m.Email, s) || strings.EqualFold(m.Name, s) {
			if match != nil {
				return nil, errf(ErrInvalid, "%q matches more than one member; use their id", s)
			}
			match = m
		}
	}
	if match == nil {
		return nil, errf(ErrNotFound, "no member called %q", s)
	}
	return match, nil
}

func ts(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
