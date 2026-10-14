package core

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// Run now: an automation (a repeating task) assigned to an agent runs at once instead of waiting for
// its due date. The workspace's own agent takes the task on, as when it's handed one; another agent
// with a webhook gets the "due" notification early (task.due), so its webhook fires within seconds.

type RunAutomationOut struct {
	Agent   *MemberRef `json:"agent"`
	Via     string     `json:"via" jsonschema:"agent (the workspace's agent does it) or webhook (the agent's webhook is called)"`
	Message string     `json:"message"`
}

// RunAutomation runs a repeating task assigned to an agent now. People only.
func (s *Service) RunAutomation(ctx context.Context, p *Principal, in TaskIDIn) (RunAutomationOut, error) {
	var out RunAutomationOut
	err := s.run(ctx, p, func(c *call) error {
		if p.Member.IsAgent() {
			return errf(ErrForbidden, "only people can run automations")
		}
		if err := p.require(ActUpdate); err != nil {
			return err
		}
		r, err := c.getTask(in.ID, false, true)
		if err != nil {
			return err
		}
		if r.Repeat == "" {
			return errf(ErrInvalid, "task %s doesn't repeat; only automations can be run now", r.ID)
		}
		if r.Status == "done" || r.Status == "cancelled" {
			return errf(ErrInvalid, "task %s is already %s", r.ID, r.Status)
		}
		agent := c.members[r.AssigneeID]
		if agent == nil || !agent.IsAgent() {
			return errf(ErrInvalid, "only automations assigned to an agent can be run now; assign it to an agent first")
		}
		if agent.Status == "revoked" || agent.Status == "removed" {
			return errf(ErrInvalid, "%s has been revoked; assign the automation to another agent", agent.Name)
		}
		out.Agent = c.ref(agent.ID)

		var workspaceAgent string
		err = c.tx.QueryRow(c.ctx, `SELECT agent_id::text FROM triage_settings`).Scan(&workspaceAgent)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if agent.ID == workspaceAgent {
			var pending bool
			if err := c.tx.QueryRow(c.ctx, `SELECT EXISTS (SELECT 1 FROM chat_replies WHERE task_id = $1 AND kind = 'take_on' AND state = 'pending')`, r.ID).Scan(&pending); err != nil {
				return err
			}
			if pending {
				return errf(ErrConflict, "%s is already working on this", agent.Name)
			}
			if err := c.requestAgent("take_on", r.ID, nil); err != nil {
				return err
			}
			out.Via, out.Message = "agent", agent.Name+" is on it"
		} else {
			var since *time.Time
			err := c.tx.QueryRow(c.ctx, `SELECT since FROM agent_webhooks WHERE agent_id = $1`, agent.ID).Scan(&since)
			if errors.Is(err, pgx.ErrNoRows) {
				return errf(ErrInvalid, "%s has no webhook, so Handoff can't call it; add one in Settings → Agents, or assign the automation to the workspace's agent", agent.Name)
			}
			if err != nil {
				return err
			}
			// One run at a time: a "due" not yet delivered (or still being retried) is already on its way.
			var pending bool
			if err := c.tx.QueryRow(c.ctx, `SELECT EXISTS (SELECT 1 FROM notifications n
				LEFT JOIN webhook_deliveries d ON d.org_id = n.org_id AND d.notification_id = n.id
				WHERE n.task_id = $1 AND n.member_id = $2 AND n.kind = 'due' AND n.created_at >= $3
					AND d.delivered_at IS NULL AND d.failed_at IS NULL)`, r.ID, agent.ID, since).Scan(&pending); err != nil {
				return err
			}
			if pending {
				return errf(ErrConflict, "%s's webhook is already being called for this", agent.Name)
			}
			// The same notification as when it comes due, early; the excerpt keeps it apart from the
			// scheduled one (one per due date) and isn't part of the payload.
			if _, err := c.tx.Exec(c.ctx, `INSERT INTO notifications (org_id, member_id, kind, task_id, actor_id, excerpt)
				VALUES ($1, $2, 'due', $3, $4, 'run now ' || to_char(clock_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))`,
				p.Org.ID, agent.ID, r.ID, p.Member.ID); err != nil {
				return err
			}
			out.Via, out.Message = "webhook", "Calling "+agent.Name+"'s webhook"
		}
		return c.event(r.ID, "run_now", map[string]any{"agent": agent.Name, "via": out.Via})
	})
	return out, err
}
