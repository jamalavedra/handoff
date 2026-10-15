package core

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/db"
)

// ErrNoOrg means the host does not belong to any workspace.
var ErrNoOrg = errors.New("no workspace at this address")

// OrgForHost maps a request host to a workspace: acme.<base> or a custom domain.
// It returns (nil, nil) for the apex (the signup site).
func (s *Service) OrgForHost(ctx context.Context, host string) (*Org, error) {
	host = strings.ToLower(host)
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h
	}
	base := s.Cfg.BaseHost()
	if s.Cfg.SingleOrg != "" {
		return s.OrgBySlug(ctx, s.Cfg.SingleOrg)
	}
	if host == base {
		return nil, nil
	}
	if sub, ok := strings.CutSuffix(host, "."+base); ok && config.ValidSlug(sub) {
		o, err := s.OrgBySlug(ctx, sub)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoOrg
		}
		return o, err
	}
	o, err := scanOrg(s.DB.Pool.QueryRow(ctx, `SELECT `+orgCols+` FROM orgs WHERE custom_domain = $1`, host))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoOrg
	}
	return o, err
}

func (s *Service) OrgBySlug(ctx context.Context, slug string) (*Org, error) {
	return scanOrg(s.DB.Pool.QueryRow(ctx, `SELECT `+orgCols+` FROM orgs WHERE slug = $1`, slug))
}

func (s *Service) OrgByID(ctx context.Context, id string) (*Org, error) {
	return scanOrg(s.DB.Pool.QueryRow(ctx, `SELECT `+orgCols+` FROM orgs WHERE id = $1`, id))
}

// Reserved slugs can't be taken by a workspace.
var reserved = []string{"www", "app", "api", "admin", "mail", "auth", "login", "signup", "static", "docs", "status", "mcp"}

type NewOrg struct {
	Slug       string
	Name       string
	AdminEmail string
	AdminName  string
}

// CreateOrg makes a workspace with its first admin and an Inbox project.
func (s *Service) CreateOrg(ctx context.Context, in NewOrg) (*Org, *Member, error) {
	slug := strings.ToLower(strings.TrimSpace(in.Slug))
	if !config.ValidSlug(slug) {
		return nil, nil, errf(ErrInvalid, "address must be 1-32 lowercase letters, digits or dashes")
	}
	for _, r := range reserved {
		if slug == r {
			return nil, nil, errf(ErrConflict, "%q is reserved", slug)
		}
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = slug
	}
	email := strings.ToLower(strings.TrimSpace(in.AdminEmail))
	if !strings.Contains(email, "@") {
		return nil, nil, errf(ErrInvalid, "invalid email")
	}
	adminName := strings.TrimSpace(in.AdminName)
	if adminName == "" {
		adminName = strings.Split(email, "@")[0]
	}
	prefix := strings.ReplaceAll(slug, "-", "")
	if len(prefix) > 6 {
		prefix = prefix[:6]
	}
	org := &Org{}
	var err error
	org, err = scanOrg(s.DB.Pool.QueryRow(ctx, `INSERT INTO orgs (slug, name, id_prefix) VALUES ($1, $2, $3) RETURNING `+orgCols, slug, name, prefix))
	if db.IsUniqueViolation(err) {
		return nil, nil, errf(ErrConflict, "%s is taken", slug)
	}
	if err != nil {
		return nil, nil, err
	}
	var admin *Member
	err = s.DB.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
		admin, err = scanMember(tx.QueryRow(ctx, `INSERT INTO members (org_id, kind, name, email, role)
			VALUES ($1, 'human', $2, $3, 'admin') RETURNING `+memberCols, org.ID, adminName, email))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO projects (org_id, name, description, created_by) VALUES ($1, $2, 'Default project', $3)`,
			org.ID, DefaultProject, admin.ID)
		return err
	})
	if err != nil {
		_, _ = s.DB.Pool.Exec(ctx, `DELETE FROM orgs WHERE id = $1`, org.ID)
		return nil, nil, err
	}
	return org, admin, nil
}

// EnsureSingleOrg creates the self-hosted workspace on first start.
func (s *Service) EnsureSingleOrg(ctx context.Context, adminEmail string) error {
	if s.Cfg.SingleOrg == "" {
		return nil
	}
	if _, err := s.OrgBySlug(ctx, s.Cfg.SingleOrg); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if adminEmail == "" {
		return errors.New("first start in single-org mode: set HANDOFF_ADMIN_EMAIL")
	}
	_, _, err := s.CreateOrg(ctx, NewOrg{Slug: s.Cfg.SingleOrg, Name: s.Cfg.SingleOrgName, AdminEmail: adminEmail})
	return err
}

// CanSignIn reports, without changing anything, whether this email may get a sign-in link:
// an invited or active member, or an address on the workspace's allowed domain.
func (s *Service) CanSignIn(ctx context.Context, org *Org, email string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	return s.DB.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM members WHERE kind = 'human' AND lower(email) = $1`, email).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			_, domain, _ := strings.Cut(email, "@")
			if org.AllowedEmailDomain == "" || domain != org.AllowedEmailDomain {
				return errf(ErrForbidden, "%s isn't a member of this workspace; ask an admin to invite you", email)
			}
			return nil
		}
		if err != nil {
			return err
		}
		if status == "revoked" || status == "paused" {
			return errf(ErrForbidden, "%s no longer has access to this workspace", email)
		}
		return nil
	})
}

// MemberForLogin finds (or, for allowed email domains, creates) the human with this email.
func (s *Service) MemberForLogin(ctx context.Context, org *Org, email, name string) (*Member, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var m *Member
	err := s.DB.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
		var err error
		m, err = scanMember(tx.QueryRow(ctx, `SELECT `+memberCols+` FROM members WHERE kind = 'human' AND lower(email) = $1`, email))
		if errors.Is(err, pgx.ErrNoRows) {
			_, domain, _ := strings.Cut(email, "@")
			if org.AllowedEmailDomain == "" || domain != org.AllowedEmailDomain {
				return errf(ErrForbidden, "%s isn't a member of this workspace; ask an admin to invite you", email)
			}
			if name == "" {
				name = strings.Split(email, "@")[0]
			}
			// Two links for a new address can be opened at once; the second waits for the first insert and uses that row.
			m, err = scanMember(tx.QueryRow(ctx, `INSERT INTO members (org_id, kind, name, email) VALUES ($1, 'human', $2, $3)
				ON CONFLICT DO NOTHING RETURNING `+memberCols, org.ID, name, email))
			if errors.Is(err, pgx.ErrNoRows) {
				m, err = scanMember(tx.QueryRow(ctx, `SELECT `+memberCols+` FROM members WHERE kind = 'human' AND lower(email) = $1`, email))
			}
		}
		if err != nil {
			return err
		}
		if m.Status == "revoked" || m.Status == "paused" {
			return errf(ErrForbidden, "%s no longer has access to this workspace", email)
		}
		if m.Status == "invited" {
			_, err = tx.Exec(ctx, `UPDATE members SET status = 'active', name = CASE WHEN $2 <> '' THEN $2 ELSE name END WHERE id = $1`, m.ID, name)
			m.Status = "active"
		}
		return err
	})
	return m, err
}

// Principal loads the caller for an authenticated member id.
func (s *Service) Principal(ctx context.Context, org *Org, memberID, via string) (*Principal, error) {
	p := &Principal{Org: org, Via: via}
	err := s.DB.InOrg(ctx, org.ID, func(tx pgx.Tx) error {
		m, err := scanMember(tx.QueryRow(ctx, `SELECT `+memberCols+` FROM members WHERE id = $1`, memberID))
		if err != nil {
			return err
		}
		if m.Status != "active" {
			return errf(ErrForbidden, "%s is %s", m.Name, m.Status)
		}
		p.Member = m
		if m.IsAgent() {
			owner, err := scanMember(tx.QueryRow(ctx, `SELECT `+memberCols+` FROM members WHERE id = $1`, m.OwnerID))
			if err != nil {
				return err
			}
			if owner.Status != "active" {
				return errf(ErrForbidden, "%s's owner no longer has access", m.Name)
			}
			p.Owner = owner
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errf(ErrForbidden, "unknown member")
	}
	return p, err
}

// AgentForClient returns the caller's agent bound to an OAuth client, creating it on first consent.
func (s *Service) AgentForClient(ctx context.Context, p *Principal, clientID, name string) (*Member, error) {
	var m *Member
	err := s.run(ctx, p, func(c *call) error {
		for _, x := range c.members {
			if x.IsAgent() && x.OwnerID == p.Member.ID && x.ClientID == clientID && x.Status == "active" {
				m = x
				return nil
			}
		}
		var err error
		m, err = c.createAgent(name, nil, clientID)
		return err
	})
	return m, err
}
