package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type NotificationOut struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind" jsonschema:"mention, assigned, approval, due (agents) or agent_done (the workspace's agent finished what you asked)"`
	TaskID    string     `json:"task_id"`
	TaskTitle string     `json:"task_title"`
	Actor     *MemberRef `json:"actor"`
	Excerpt   string     `json:"excerpt,omitempty"`
	Read      bool       `json:"read"`
	At        string     `json:"at"`
}

// notify addresses a notification to one member; nobody is notified about their own actions.
func (c *call) notify(to *Member, kind, taskID, text string) error {
	if to == nil || to.ID == c.p.Member.ID || to.Status == "revoked" || to.Status == "removed" {
		return nil
	}
	_, err := c.tx.Exec(c.ctx, `INSERT INTO notifications (org_id, member_id, kind, task_id, actor_id, excerpt)
		VALUES ($1, $2, $3, $4, $5, $6)`, c.p.Org.ID, to.ID, kind, taskID, c.p.Member.ID, excerpt(text))
	return err
}

// notifyDone tells a person that the workspace's agent finished what they asked for. Agents never
// get it: they don't wait on the agent, and it would reach their webhooks.
func (c *call) notifyDone(to *Member, taskID, text string) error {
	if to == nil || to.IsAgent() {
		return nil
	}
	return c.notify(to, "agent_done", taskID, firstLine(text))
}

// mentioned finds "@Name" for members, longest name first so "@Legal bot" wins over "@Legal".
// "ops@example.com" is not a mention.
func (c *call) mentioned(body string) []*Member {
	var ms []*Member
	for _, m := range c.members {
		if m.Name != "" && m.Status != "revoked" && m.Status != "removed" {
			ms = append(ms, m)
		}
	}
	sort.Slice(ms, func(i, j int) bool { return len(ms[i].Name) > len(ms[j].Name) })
	lower := strings.ToLower(body)
	seen := map[string]bool{}
	var out []*Member
	for i := 0; i < len(lower); i++ {
		if lower[i] != '@' {
			continue
		}
		if i > 0 {
			if r, _ := utf8.DecodeLastRuneInString(lower[:i]); unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' {
				continue
			}
		}
		rest := lower[i+1:]
		for _, m := range ms {
			n := strings.ToLower(m.Name)
			if !strings.HasPrefix(rest, n) {
				continue
			}
			if r, _ := utf8.DecodeRuneInString(rest[len(n):]); unicode.IsLetter(r) || unicode.IsDigit(r) {
				continue
			}
			// Two members can share a name; notify all of them rather than guess.
			for _, same := range ms {
				if strings.EqualFold(same.Name, m.Name) && !seen[same.ID] {
					seen[same.ID] = true
					out = append(out, same)
				}
			}
			break
		}
	}
	return out
}

// visibleNotifications limits a member's notifications to live tasks they can still see.
const visibleNotifications = `FROM notifications n JOIN tasks t ON t.org_id = n.org_id AND t.id = n.task_id
	WHERE n.member_id = $1 AND t.deleted_at IS NULL AND t.project_id::text = ANY($2)`

func (c *call) unreadCount() (int, error) {
	var n int
	err := c.tx.QueryRow(c.ctx, `SELECT count(*) `+visibleNotifications+` AND n.read_at IS NULL`, c.p.Member.ID, c.visible).Scan(&n)
	return n, err
}

type InboxIn struct {
	Unread bool `json:"unread,omitempty" jsonschema:"only unread notifications"`
	Limit  int  `json:"limit,omitempty" jsonschema:"default 50, max 200"`
}

type InboxOut struct {
	Notifications []NotificationOut `json:"notifications" jsonschema:"newest first"`
	Unread        int               `json:"unread"`
}

// Inbox lists @mentions, tasks assigned to the caller and approval requests from the caller's agents.
func (s *Service) Inbox(ctx context.Context, p *Principal, in InboxIn) (InboxOut, error) {
	out := InboxOut{Notifications: []NotificationOut{}}
	// Newest first. The limit trims read history only, so an old unread alert is never cut off.
	err := s.run(ctx, p, func(c *call) error {
		cond := ""
		if in.Unread {
			cond = " AND n.read_at IS NULL"
		}
		rows, err := c.tx.Query(c.ctx, `SELECT n.id, n.kind, n.task_id, t.title, n.actor_id::text, n.excerpt, n.read_at IS NOT NULL, n.created_at
			`+visibleNotifications+cond+`
			AND (n.read_at IS NULL OR n.id IN (SELECT n.id `+visibleNotifications+cond+` AND n.read_at IS NOT NULL ORDER BY n.id DESC LIMIT $3))
			ORDER BY n.id DESC LIMIT 200`, p.Member.ID, c.visible, clampLimit(in.Limit, 50, 200))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n NotificationOut
			var actor string
			var at time.Time
			if err := rows.Scan(&n.ID, &n.Kind, &n.TaskID, &n.TaskTitle, &actor, &n.Excerpt, &n.Read, &at); err != nil {
				return err
			}
			n.Actor, n.At = c.ref(actor), ts(&at)
			out.Notifications = append(out.Notifications, n)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		out.Unread, err = c.unreadCount()
		return err
	})
	return out, err
}

type MarkReadIn struct {
	IDs  []int64 `json:"ids,omitempty" jsonschema:"notification ids from inbox"`
	Task string  `json:"task,omitempty" jsonschema:"mark this task's mentions and assignments read (approval requests stay until decided)"`
	All  bool    `json:"all,omitempty" jsonschema:"mark every notification read"`
}

type MarkReadOut struct {
	Unread int `json:"unread"`
}

func (s *Service) MarkRead(ctx context.Context, p *Principal, in MarkReadIn) (MarkReadOut, error) {
	var out MarkReadOut
	err := s.run(ctx, p, func(c *call) error {
		if !in.All && len(in.IDs) == 0 && in.Task == "" {
			return errf(ErrInvalid, "pass ids, task or all=true")
		}
		q := `UPDATE notifications SET read_at = now() WHERE member_id = $1 AND read_at IS NULL`
		args := []any{p.Member.ID}
		if !in.All && len(in.IDs) > 0 {
			args = append(args, in.IDs)
			q += fmt.Sprintf(` AND id = ANY($%d)`, len(args))
		}
		if !in.All && in.Task != "" {
			args = append(args, in.Task)
			q += fmt.Sprintf(` AND task_id = $%d AND kind <> 'approval'`, len(args))
		}
		if _, err := c.tx.Exec(c.ctx, q, args...); err != nil {
			return err
		}
		var err error
		out.Unread, err = c.unreadCount()
		return err
	})
	return out, err
}
