package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type MemberOut struct {
	ID          string       `json:"id"`
	Kind        string       `json:"kind"`
	Name        string       `json:"name"`
	Email       string       `json:"email,omitempty"`
	Role        string       `json:"role"`
	Status      string       `json:"status"`
	Owner       *MemberRef   `json:"owner,omitempty"`
	Permissions *Permissions `json:"permissions,omitempty"`
	CreatedAt   string       `json:"created_at"`
}

func (c *call) memberOut(m *Member) MemberOut {
	o := MemberOut{ID: m.ID, Kind: m.Kind, Name: m.Name, Email: m.Email, Role: m.Role, Status: m.Status, CreatedAt: ts(&m.CreatedAt)}
	if m.IsAgent() {
		o.Owner = c.ref(m.OwnerID)
		perms := m.Permissions
		o.Permissions = &perms
	}
	return o
}

type MembersOut struct {
	Members []MemberOut `json:"members"`
}

func (s *Service) ListMembers(ctx context.Context, p *Principal, _ struct{}) (MembersOut, error) {
	out := MembersOut{Members: []MemberOut{}}
	err := s.run(ctx, p, func(c *call) error {
		ms := make([]*Member, 0, len(c.members))
		for _, m := range c.members {
			if m.Status != "revoked" {
				ms = append(ms, m)
			}
		}
		slices.SortFunc(ms, func(a, b *Member) int {
			if a.Kind != b.Kind {
				return strings.Compare(b.Kind, a.Kind) // humans first
			}
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
		for _, m := range ms {
			out.Members = append(out.Members, c.memberOut(m))
		}
		return nil
	})
	return out, err
}

type EventOut struct {
	ID         int64          `json:"id"`
	TaskID     string         `json:"task_id,omitempty"`
	TaskTitle  string         `json:"task_title,omitempty"`
	Actor      *MemberRef     `json:"actor"`
	OnBehalfOf *MemberRef     `json:"on_behalf_of,omitempty"`
	Via        string         `json:"via"`
	Kind       string         `json:"kind"`
	Data       map[string]any `json:"data"`
	At         string         `json:"at"`
}

func (c *call) events(where string, args []any, limit int, asc bool) ([]EventOut, error) {
	order := "DESC"
	if asc {
		order = "ASC"
	}
	args = append(args, limit)
	rows, err := c.tx.Query(c.ctx, fmt.Sprintf(`SELECT e.id, coalesce(e.task_id, ''), coalesce(t.title, ''), e.actor_id::text,
		coalesce(e.on_behalf_of::text, ''), e.via, e.kind, e.data, e.created_at
		FROM events e LEFT JOIN tasks t ON t.org_id = e.org_id AND t.id = e.task_id
		WHERE %s ORDER BY e.id %s LIMIT $%d`, where, order, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EventOut{}
	for rows.Next() {
		var e EventOut
		var actor, obo string
		var data []byte
		var at time.Time
		if err := rows.Scan(&e.ID, &e.TaskID, &e.TaskTitle, &actor, &obo, &e.Via, &e.Kind, &data, &at); err != nil {
			return nil, err
		}
		e.Actor, e.OnBehalfOf, e.At = c.ref(actor), c.ref(obo), ts(&at)
		_ = json.Unmarshal(data, &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}

type ActivityIn struct {
	Member string `json:"member,omitempty" jsonschema:"only this member's actions"`
	Limit  int    `json:"limit,omitempty" jsonschema:"default 50, max 200"`
}

type ActivityOut struct {
	Events []EventOut `json:"events"`
}

// Activity is the company-wide log, limited to tasks the caller can see.
func (s *Service) Activity(ctx context.Context, p *Principal, in ActivityIn) (ActivityOut, error) {
	var out ActivityOut
	err := s.run(ctx, p, func(c *call) error {
		where := "(e.task_id IS NULL OR t.project_id::text = ANY($1))"
		args := []any{c.visible}
		if in.Member != "" {
			m, err := c.resolveMember(in.Member)
			if err != nil {
				return err
			}
			args = append(args, m.ID)
			where += fmt.Sprintf(" AND e.actor_id = $%d", len(args))
		}
		if p.Member.IsAgent() {
			where += " AND e.task_id IS NOT NULL"
		}
		var err error
		out.Events, err = c.events(where, args, clampLimit(in.Limit, 50, 200), false)
		return err
	})
	return out, err
}

// ---- agents (people manage these from the web app) ----

type CreateAgentIn struct {
	Name        string       `json:"name"`
	Permissions *Permissions `json:"permissions,omitempty"`
}

func (s *Service) validPerms(c *call, perms Permissions) (Permissions, error) {
	perms = perms.normalized()
	for _, a := range append(slices.Clone(perms.Actions), perms.Approval...) {
		if !slices.Contains(AllActions, a) {
			return perms, errf(ErrInvalid, "unknown action %q", a)
		}
	}
	for _, id := range perms.Projects {
		if id != "*" && c.projects[id] == nil {
			return perms, errf(ErrInvalid, "unknown project %q", id)
		}
	}
	return perms, nil
}

// CreateAgent adds an agent member owned by the caller. Used by the web app
// and by OAuth consent.
func (s *Service) CreateAgent(ctx context.Context, p *Principal, in CreateAgentIn) (MemberOut, error) {
	var out MemberOut
	err := s.run(ctx, p, func(c *call) error {
		m, err := c.createAgent(in.Name, in.Permissions, "")
		if err != nil {
			return err
		}
		out = c.memberOut(m)
		return nil
	})
	return out, err
}

func (c *call) createAgent(name string, perms *Permissions, clientID string) (*Member, error) {
	if c.p.Member.IsAgent() {
		return nil, errf(ErrForbidden, "only people can add agents")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return nil, errf(ErrInvalid, "agent name must be 1-80 characters")
	}
	pp := DefaultAgentPermissions()
	if perms != nil {
		var err error
		if pp, err = c.s.validPerms(c, *perms); err != nil {
			return nil, err
		}
	}
	b, _ := json.Marshal(pp)
	var cid *string
	if clientID != "" {
		cid = &clientID
	}
	m, err := scanMember(c.tx.QueryRow(c.ctx, `INSERT INTO members (org_id, kind, name, role, owner_id, oauth_client_id, permissions)
		VALUES ($1, 'agent', $2, 'member', $3, $4, $5) RETURNING `+memberCols, c.p.Org.ID, name, c.p.Member.ID, cid, b))
	if err != nil {
		return nil, err
	}
	c.members[m.ID] = m
	return m, c.event("", "agent_added", map[string]any{"agent_id": m.ID, "name": name})
}

type UpdateAgentIn struct {
	ID          string       `json:"id"`
	Name        string       `json:"name,omitempty"`
	Permissions *Permissions `json:"permissions,omitempty"`
	Revoke      bool         `json:"revoke,omitempty"`
}

func (c *call) manageable(id string) (*Member, error) {
	m, ok := c.members[id]
	if !ok || !m.IsAgent() {
		return nil, errf(ErrNotFound, "no agent %q", id)
	}
	if c.p.Member.IsAgent() || (m.OwnerID != c.p.Member.ID && !c.p.IsAdmin()) {
		return nil, errf(ErrForbidden, "only the agent's owner or an admin can change it")
	}
	return m, nil
}

func (s *Service) UpdateAgent(ctx context.Context, p *Principal, in UpdateAgentIn) (MemberOut, error) {
	var out MemberOut
	err := s.run(ctx, p, func(c *call) error {
		m, err := c.manageable(in.ID)
		if err != nil {
			return err
		}
		if in.Name != "" {
			if _, err := c.tx.Exec(c.ctx, `UPDATE members SET name = $2 WHERE id = $1`, m.ID, strings.TrimSpace(in.Name)); err != nil {
				return err
			}
		}
		if in.Permissions != nil {
			pp, err := s.validPerms(c, *in.Permissions)
			if err != nil {
				return err
			}
			b, _ := json.Marshal(pp)
			if _, err := c.tx.Exec(c.ctx, `UPDATE members SET permissions = $2 WHERE id = $1`, m.ID, b); err != nil {
				return err
			}
			if err := c.event("", "agent_permissions_changed", map[string]any{"agent_id": m.ID, "permissions": pp}); err != nil {
				return err
			}
		}
		if in.Revoke {
			if err := c.revokeAgent(m.ID); err != nil {
				return err
			}
		}
		m, err = scanMember(c.tx.QueryRow(c.ctx, `SELECT `+memberCols+` FROM members WHERE id = $1`, m.ID))
		if err != nil {
			return err
		}
		out = c.memberOut(m)
		return nil
	})
	return out, err
}

func (c *call) revokeAgent(id string) error {
	for _, q := range []string{
		`UPDATE members SET status = 'revoked' WHERE id = $1`,
		`UPDATE api_keys SET revoked_at = now() WHERE member_id = $1 AND revoked_at IS NULL`,
		`UPDATE oauth_tokens SET revoked_at = now() WHERE member_id = $1 AND revoked_at IS NULL`,
		`UPDATE tasks SET claimed_by = NULL, claim_expires_at = NULL, status = CASE WHEN status = 'in_progress' THEN 'open' ELSE status END WHERE claimed_by = $1`,
	} {
		if _, err := c.tx.Exec(c.ctx, q, id); err != nil {
			return err
		}
	}
	return c.event("", "agent_revoked", map[string]any{"agent_id": id})
}

// ---- approvals ----

type ApprovalOut struct {
	ID        string     `json:"id"`
	Agent     *MemberRef `json:"agent"`
	Action    string     `json:"action"`
	Task      *TaskOut   `json:"task,omitempty"`
	Reason    string     `json:"reason,omitempty"`
	Status    string     `json:"status"`
	CreatedAt string     `json:"created_at"`
}

type ApprovalsOut struct {
	Approvals []ApprovalOut `json:"approvals"`
}

// ListApprovals returns pending approvals for agents the caller owns (all for admins).
func (s *Service) ListApprovals(ctx context.Context, p *Principal, _ struct{}) (ApprovalsOut, error) {
	out := ApprovalsOut{Approvals: []ApprovalOut{}}
	err := s.run(ctx, p, func(c *call) error {
		rows, err := c.tx.Query(c.ctx, `SELECT id, agent_id::text, action, coalesce(task_id, ''), payload, status, created_at
			FROM approvals WHERE status = 'pending' ORDER BY created_at`)
		if err != nil {
			return err
		}
		type row struct {
			ApprovalOut
			agentID, taskID string
		}
		var rs []row
		for rows.Next() {
			var r row
			var payload []byte
			var at time.Time
			if err := rows.Scan(&r.ID, &r.agentID, &r.Action, &r.taskID, &payload, &r.Status, &at); err != nil {
				rows.Close()
				return err
			}
			var pl struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(payload, &pl)
			r.Reason, r.CreatedAt, r.Agent = pl.Reason, ts(&at), c.ref(r.agentID)
			rs = append(rs, r)
		}
		rows.Close()
		for _, r := range rs {
			if _, err := c.manageable(r.agentID); err != nil {
				continue
			}
			if r.taskID != "" {
				if t, err := c.getTask(r.taskID, true, false); err == nil {
					o := c.out(t, false)
					r.Task = &o
				}
			}
			out.Approvals = append(out.Approvals, r.ApprovalOut)
		}
		return nil
	})
	return out, err
}

type DecideIn struct {
	ID      string `json:"id"`
	Approve bool   `json:"approve"`
}

func (s *Service) DecideApproval(ctx context.Context, p *Principal, in DecideIn) (ApprovalOut, error) {
	var out ApprovalOut
	err := s.run(ctx, p, func(c *call) error {
		var agentID, action, taskID, status string
		err := c.tx.QueryRow(c.ctx, `SELECT agent_id::text, action, coalesce(task_id, ''), status FROM approvals WHERE id = $1 FOR UPDATE`, in.ID).
			Scan(&agentID, &action, &taskID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return errf(ErrNotFound, "no approval %q", in.ID)
		}
		if err != nil {
			return err
		}
		if _, err := c.manageable(agentID); err != nil {
			return err
		}
		if status != "pending" {
			return errf(ErrConflict, "approval already %s", status)
		}
		decision := "rejected"
		if in.Approve {
			decision = "approved"
		}
		if _, err := c.tx.Exec(c.ctx, `UPDATE approvals SET status = $2, decided_by = $3, decided_at = now() WHERE id = $1`,
			in.ID, decision, p.Member.ID); err != nil {
			return err
		}
		if in.Approve && action == "trash" && taskID != "" {
			if t, err := c.getTask(taskID, true, true); err == nil && t.DeletedAt == nil {
				if err := c.trash(taskID, "approved request from "+c.ref(agentID).Name); err != nil {
					return err
				}
			}
		}
		if err := c.event(taskID, "approval_"+decision, map[string]any{"approval_id": in.ID, "action": action, "agent": c.ref(agentID).Name}); err != nil {
			return err
		}
		out = ApprovalOut{ID: in.ID, Agent: c.ref(agentID), Action: action, Status: decision}
		return nil
	})
	return out, err
}

// ---- admin ----

type InviteIn struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
	Role  string `json:"role,omitempty" jsonschema:"member (default) or admin"`
}

// Invite adds a human member who can then sign in by email.
func (s *Service) Invite(ctx context.Context, p *Principal, in InviteIn) (MemberOut, error) {
	var out MemberOut
	err := s.run(ctx, p, func(c *call) error {
		if !p.IsAdmin() {
			return errf(ErrForbidden, "only admins can invite people")
		}
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if !strings.Contains(email, "@") {
			return errf(ErrInvalid, "invalid email")
		}
		role := in.Role
		if role == "" {
			role = "member"
		}
		if role != "member" && role != "admin" {
			return errf(ErrInvalid, "role must be member or admin")
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = strings.Split(email, "@")[0]
		}
		m, err := scanMember(c.tx.QueryRow(c.ctx, `INSERT INTO members (org_id, kind, name, email, role, status)
			VALUES ($1, 'human', $2, $3, $4, 'invited')
			ON CONFLICT (org_id, lower(email)) WHERE kind = 'human' DO UPDATE SET role = EXCLUDED.role,
				status = CASE WHEN members.status = 'revoked' THEN 'invited' ELSE members.status END
			RETURNING `+memberCols, p.Org.ID, name, email, role))
		if err != nil {
			return err
		}
		out = c.memberOut(m)
		return c.event("", "member_invited", map[string]any{"email": email, "role": role})
	})
	return out, err
}

type UpdateMemberIn struct {
	ID     string `json:"id"`
	Role   string `json:"role,omitempty"`
	Remove bool   `json:"remove,omitempty"`
}

func (s *Service) UpdateMember(ctx context.Context, p *Principal, in UpdateMemberIn) (MemberOut, error) {
	var out MemberOut
	err := s.run(ctx, p, func(c *call) error {
		if !p.IsAdmin() {
			return errf(ErrForbidden, "only admins can change members")
		}
		m, ok := c.members[in.ID]
		if !ok || m.IsAgent() {
			return errf(ErrNotFound, "no person %q", in.ID)
		}
		// Lock the active admins so concurrent demotions can't remove the last one.
		var adminIDs []string
		rows, qerr := c.tx.Query(c.ctx, `SELECT id::text FROM members
			WHERE kind = 'human' AND role = 'admin' AND status = 'active' FOR UPDATE`)
		if qerr != nil {
			return qerr
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			adminIDs = append(adminIDs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		admins := len(adminIDs)
		m.Role = "member"
		for _, id := range adminIDs {
			if id == m.ID {
				m.Role = "admin"
			}
		}
		lastAdmin := m.Role == "admin" && admins <= 1
		if in.Role != "" {
			if in.Role != "member" && in.Role != "admin" {
				return errf(ErrInvalid, "role must be member or admin")
			}
			if lastAdmin && in.Role != "admin" {
				return errf(ErrInvalid, "the workspace needs at least one admin")
			}
			if _, err := c.tx.Exec(c.ctx, `UPDATE members SET role = $2 WHERE id = $1`, m.ID, in.Role); err != nil {
				return err
			}
		}
		if in.Remove {
			if lastAdmin {
				return errf(ErrInvalid, "the workspace needs at least one admin")
			}
			if _, err := c.tx.Exec(c.ctx, `UPDATE members SET status = 'revoked' WHERE id = $1`, m.ID); err != nil {
				return err
			}
			if _, err := c.tx.Exec(c.ctx, `DELETE FROM sessions WHERE member_id = $1`, m.ID); err != nil {
				return err
			}
			for _, a := range c.members {
				if a.OwnerID == m.ID && a.Status != "revoked" {
					if err := c.revokeAgent(a.ID); err != nil {
						return err
					}
				}
			}
		}
		if err := c.event("", "member_changed", map[string]any{"member_id": m.ID, "role": in.Role, "removed": in.Remove}); err != nil {
			return err
		}
		m, err := scanMember(c.tx.QueryRow(c.ctx, `SELECT `+memberCols+` FROM members WHERE id = $1`, m.ID))
		if err != nil {
			return err
		}
		out = c.memberOut(m)
		return nil
	})
	return out, err
}

type UpdateOrgIn struct {
	Name               string  `json:"name,omitempty"`
	CustomDomain       *string `json:"custom_domain,omitempty"`
	AllowedEmailDomain *string `json:"allowed_email_domain,omitempty" jsonschema:"anyone with this email domain may join"`
}

func (s *Service) UpdateOrg(ctx context.Context, p *Principal, in UpdateOrgIn) (Org, error) {
	var out Org
	err := s.run(ctx, p, func(c *call) error {
		if !p.IsAdmin() {
			return errf(ErrForbidden, "only admins can change workspace settings")
		}
		if n := strings.TrimSpace(in.Name); n != "" {
			if _, err := c.tx.Exec(c.ctx, `UPDATE orgs SET name = $2 WHERE id = $1`, p.Org.ID, n); err != nil {
				return err
			}
		}
		if in.CustomDomain != nil {
			d := strings.ToLower(strings.TrimSpace(*in.CustomDomain))
			var v *string
			if d != "" {
				if strings.ContainsAny(d, "/: ") || !strings.Contains(d, ".") {
					return errf(ErrInvalid, "custom domain should look like tasks.acme.com")
				}
				v = &d
			}
			if _, err := c.tx.Exec(c.ctx, `UPDATE orgs SET custom_domain = $2 WHERE id = $1`, p.Org.ID, v); err != nil {
				if strings.Contains(err.Error(), "23505") {
					return errf(ErrConflict, "that domain is used by another workspace")
				}
				return err
			}
		}
		if in.AllowedEmailDomain != nil {
			d := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(*in.AllowedEmailDomain)), "@")
			var v *string
			if d != "" {
				v = &d
			}
			if _, err := c.tx.Exec(c.ctx, `UPDATE orgs SET allowed_email_domain = $2 WHERE id = $1`, p.Org.ID, v); err != nil {
				return err
			}
		}
		if err := c.event("", "org_changed", nil); err != nil {
			return err
		}
		o, err := scanOrg(c.tx.QueryRow(c.ctx, `SELECT `+orgCols+` FROM orgs WHERE id = $1`, p.Org.ID))
		if err != nil {
			return err
		}
		out = *o
		return nil
	})
	return out, err
}

const orgCols = `id, slug, name, id_prefix, coalesce(custom_domain, ''), coalesce(allowed_email_domain, '')`

func scanOrg(row pgx.Row) (*Org, error) {
	o := &Org{}
	return o, row.Scan(&o.ID, &o.Slug, &o.Name, &o.IDPrefix, &o.CustomDomain, &o.AllowedEmailDomain)
}

// ---- API keys ----

type APIKeyOut struct {
	ID         string `json:"id"`
	Hint       string `json:"hint"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
	Key        string `json:"key,omitempty" jsonschema:"only returned once, at creation"`
}

// CreateAPIKey mints a key for an agent; only its hash is stored.
func (s *Service) CreateAPIKey(ctx context.Context, p *Principal, agentID, key string) (APIKeyOut, error) {
	var out APIKeyOut
	err := s.run(ctx, p, func(c *call) error {
		m, err := c.manageable(agentID)
		if err != nil {
			return err
		}
		if m.Status != "active" {
			return errf(ErrInvalid, "%s is %s", m.Name, m.Status)
		}
		hint := key[:8] + "…" + key[len(key)-4:]
		var at time.Time
		if err := c.tx.QueryRow(c.ctx, `INSERT INTO api_keys (org_id, member_id, token_hash, hint, created_by) VALUES ($1, $2, $3, $4, $5)
			RETURNING id, created_at`, p.Org.ID, m.ID, hashKey(key), hint, p.Member.ID).Scan(&out.ID, &at); err != nil {
			return err
		}
		out.Hint, out.CreatedAt, out.Key = hint, ts(&at), key
		return c.event("", "api_key_created", map[string]any{"agent_id": m.ID, "hint": hint})
	})
	return out, err
}

func (s *Service) ListAPIKeys(ctx context.Context, p *Principal, agentID string) ([]APIKeyOut, error) {
	out := []APIKeyOut{}
	err := s.run(ctx, p, func(c *call) error {
		if _, err := c.manageable(agentID); err != nil {
			return err
		}
		rows, err := c.tx.Query(c.ctx, `SELECT id, hint, created_at, last_used_at FROM api_keys WHERE member_id = $1 AND revoked_at IS NULL ORDER BY created_at`, agentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k APIKeyOut
			var at time.Time
			var used *time.Time
			if err := rows.Scan(&k.ID, &k.Hint, &at, &used); err != nil {
				return err
			}
			k.CreatedAt, k.LastUsedAt = ts(&at), ts(used)
			out = append(out, k)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Service) RevokeAPIKey(ctx context.Context, p *Principal, agentID, keyID string) error {
	return s.run(ctx, p, func(c *call) error {
		if _, err := c.manageable(agentID); err != nil {
			return err
		}
		tag, err := c.tx.Exec(c.ctx, `UPDATE api_keys SET revoked_at = now() WHERE id::text = $1 AND member_id = $2 AND revoked_at IS NULL`, keyID, agentID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errf(ErrNotFound, "no such key")
		}
		return c.event("", "api_key_revoked", map[string]any{"agent_id": agentID, "key_id": keyID})
	})
}
