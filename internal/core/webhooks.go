package core

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/auth"
)

// Agent webhooks: an agent that isn't always connected (a Slack bot, a CI job, a cloud function) gives
// Handoff a URL, and Handoff POSTs to it whenever the agent gets a notification: a task assigned to it,
// an @mention, an approval decision it waits on, or one of its tasks coming due. Recurring tasks
// assigned to an agent therefore act as a schedule ("every Friday, write the weekly report").
//
// Each POST is JSON, signed as X-Handoff-Signature: t=<unix seconds>,v1=<hex HMAC-SHA256 of "t.body">
// with the agent's webhook secret. Failed deliveries are retried with backoff, 8 attempts in all.

type WebhookOut struct {
	URL        string `json:"url"`
	Secret     string `json:"secret,omitempty" jsonschema:"only returned when the webhook is created or the secret rotated; store it to verify signatures"`
	LastAt     string `json:"last_at,omitempty"`
	LastStatus int    `json:"last_status,omitempty"`
	LastError  string `json:"last_error,omitempty"`
}

type SetWebhookIn struct {
	Agent  string `json:"agent" jsonschema:"agent id or name"`
	URL    string `json:"url" jsonschema:"https URL to POST notifications to; empty removes the webhook"`
	Rotate bool   `json:"rotate,omitempty" jsonschema:"issue a new signing secret"`
}

type AgentIn struct {
	Agent string `json:"agent" jsonschema:"agent id or name"`
}

type WebhookTestOut struct {
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
}

const webhookAttempts = 8

func (c *call) webhookOut(agentID string) (*WebhookOut, error) {
	var o WebhookOut
	var last *time.Time
	var status *int
	err := c.tx.QueryRow(c.ctx, `SELECT url, last_at, last_status, last_error FROM agent_webhooks WHERE agent_id = $1`, agentID).
		Scan(&o.URL, &last, &status, &o.LastError)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	o.LastAt = ts(last)
	if status != nil {
		o.LastStatus = *status
	}
	return &o, nil
}

// SetWebhook sets, changes or removes an agent's webhook. Owners and admins only.
func (s *Service) SetWebhook(ctx context.Context, p *Principal, in SetWebhookIn) (*WebhookOut, error) {
	var out *WebhookOut
	err := s.run(ctx, p, func(c *call) error {
		who, err := c.resolveMember(in.Agent)
		if err != nil {
			return err
		}
		m, err := c.manageable(who.ID)
		if err != nil {
			return err
		}
		raw := strings.TrimSpace(in.URL)
		if raw == "" {
			_, err := c.tx.Exec(c.ctx, `DELETE FROM agent_webhooks WHERE agent_id = $1`, m.ID)
			if err == nil {
				err = c.event("", "webhook_removed", map[string]any{"agent": m.Name})
			}
			return err
		}
		u, err := triageBaseURL(raw)
		if err != nil {
			return errf(ErrInvalid, "url must be an http(s) URL without credentials in it")
		}
		existing, err := c.webhookOut(m.ID)
		if err != nil {
			return err
		}
		var secret string
		if existing == nil || in.Rotate {
			secret = auth.NewToken("whsec_")
			sealed, err := s.seal([]byte(secret))
			if err != nil {
				return err
			}
			_, err = c.tx.Exec(c.ctx, `INSERT INTO agent_webhooks (org_id, agent_id, url, secret) VALUES ($1, $2, $3, $4)
				ON CONFLICT (org_id, agent_id) DO UPDATE SET url = $3, secret = $4`, p.Org.ID, m.ID, u, sealed)
			if err != nil {
				return err
			}
		} else if _, err := c.tx.Exec(c.ctx, `UPDATE agent_webhooks SET url = $2 WHERE agent_id = $1`, m.ID, u); err != nil {
			return err
		}
		if out, err = c.webhookOut(m.ID); err != nil {
			return err
		}
		out.Secret = secret
		return c.event("", "webhook_set", map[string]any{"agent": m.Name})
	})
	return out, err
}

// TestWebhook sends a "ping" right away and reports what the endpoint answered.
func (s *Service) TestWebhook(ctx context.Context, p *Principal, in AgentIn) (WebhookTestOut, error) {
	var hook webhookTarget
	err := s.run(ctx, p, func(c *call) error {
		who, err := c.resolveMember(in.Agent)
		if err != nil {
			return err
		}
		m, err := c.manageable(who.ID)
		if err != nil {
			return err
		}
		hook, err = c.webhookTarget(m.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errf(ErrInvalid, "%s has no webhook", m.Name)
		}
		hook.agent = c.ref(m.ID)
		return err
	})
	if err != nil {
		return WebhookTestOut{}, err
	}
	body, _ := json.Marshal(webhookPayload{ID: "ping", Type: "ping", Workspace: p.Org.Slug, Agent: hook.agent, CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	status, derr := s.post(ctx, hook, body)
	s.recordWebhook(ctx, p.Org.ID, hook.agentID, status, derr)
	out := WebhookTestOut{Status: status}
	if derr != nil {
		out.Error = derr.Error()
	}
	return out, nil
}

type webhookTarget struct {
	agentID, url string
	secret       []byte
	agent        *MemberRef
}

func (c *call) webhookTarget(agentID string) (webhookTarget, error) {
	t := webhookTarget{agentID: agentID}
	var sealed []byte
	if err := c.tx.QueryRow(c.ctx, `SELECT url, secret FROM agent_webhooks WHERE agent_id = $1`, agentID).Scan(&t.url, &sealed); err != nil {
		return t, err
	}
	secret, err := c.s.unseal(sealed)
	if err != nil {
		return t, errf(ErrInvalid, "the webhook secret can't be read (was HANDOFF_SECRET changed?); rotate it")
	}
	t.secret = secret
	return t, nil
}

type webhookTask struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Due     string `json:"due,omitempty"`
	Project string `json:"project"`
	URL     string `json:"url"`
}

type webhookPayload struct {
	ID        string       `json:"id"`
	Type      string       `json:"type"`
	Workspace string       `json:"workspace"`
	Agent     *MemberRef   `json:"agent"`
	Task      *webhookTask `json:"task,omitempty"`
	Actor     *MemberRef   `json:"actor,omitempty"`
	Excerpt   string       `json:"excerpt,omitempty"`
	CreatedAt string       `json:"created_at"`
}

var webhookTypes = map[string]string{"assigned": "task.assigned", "mention": "comment.mention", "approval": "approval", "due": "task.due"}

// post sends one signed delivery and returns the HTTP status.
func (s *Service) post(ctx context.Context, hook webhookTarget, body []byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	t := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, hook.secret)
	mac.Write([]byte(t + "." + string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Handoff-Webhook/1")
	req.Header.Set("X-Handoff-Signature", "t="+t+",v1="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := s.outbound().Do(req)
	if err != nil {
		return 0, fmt.Errorf("unreachable: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, fmt.Errorf("endpoint answered %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func (s *Service) recordWebhook(ctx context.Context, org, agentID string, status int, derr error) {
	msg := ""
	if derr != nil {
		msg = derr.Error()
		if len(msg) > 300 {
			msg = msg[:300]
		}
	}
	_ = s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agent_webhooks SET last_at = now(), last_status = $2, last_error = $3 WHERE agent_id = $1`, agentID, status, msg)
		return err
	})
}

// RunWebhooks delivers agents' notifications until ctx ends.
func (s *Service) RunWebhooks(ctx context.Context) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		go s.DeliverWebhooks(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// DeliverWebhooks runs one delivery round over every workspace, a few at a time. A workspace whose
// last round is still going (a slow endpoint) is skipped, so it only holds up itself.
func (s *Service) DeliverWebhooks(ctx context.Context) {
	rows, err := s.DB.Pool.Query(ctx, `SELECT id::text, slug FROM orgs`)
	if err != nil {
		slog.Error("webhooks", "err", err)
		return
	}
	type org struct{ id, slug string }
	orgs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (org, error) {
		var o org
		return o, r.Scan(&o.id, &o.slug)
	})
	if err != nil {
		slog.Error("webhooks", "err", err)
		return
	}
	var wg sync.WaitGroup
	for _, o := range orgs {
		if _, busy := s.hooksBusy.LoadOrStore(o.id, true); busy {
			continue
		}
		wg.Go(func() {
			defer s.hooksBusy.Delete(o.id)
			s.hookSlots <- struct{}{}
			defer func() { <-s.hookSlots }()
			s.webhookTick(ctx, o.id, o.slug)
		})
	}
	wg.Wait()
}

// webhookTick does one round for a workspace: note tasks that came due for agents, queue new
// notifications for agents with a webhook, then send what's due one delivery at a time.
func (s *Service) webhookTick(ctx context.Context, org, slug string) {
	var takeOns []replyJob
	err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		// An automation assigned to the workspace's agent that came due (UTC) is handed to it once, as Run now
		// does; an occurrence that already had a take-on (run now, or handed over) isn't run again.
		// shortcut: UTC dates, no workspace timezone or time of day yet (#89).
		rows, err := tx.Query(ctx, `INSERT INTO chat_replies (org_id, task_id, author_id, kind)
			SELECT t.org_id, t.id, t.created_by, 'take_on'
			FROM tasks t JOIN triage_settings ts ON ts.org_id = t.org_id AND ts.agent_id = t.assignee_id
			JOIN members m ON m.org_id = t.org_id AND m.id = t.assignee_id AND m.status = 'active'
			WHERE t.deleted_at IS NULL AND t.repeat <> '' AND t.status NOT IN ('done', 'cancelled') AND t.due_at IS NOT NULL
				AND t.due_at < (now() AT TIME ZONE 'UTC')::date + 1
				AND NOT EXISTS (SELECT 1 FROM chat_replies r WHERE r.org_id = t.org_id AND r.task_id = t.id AND r.kind = 'take_on')
			ON CONFLICT DO NOTHING RETURNING id::text`)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, id := range ids {
			takeOns = append(takeOns, replyJob{org: org, id: id})
		}
		// Due today (UTC) and assigned to an agent with a webhook: one "due" notification per agent and due date.
		if _, err := tx.Exec(ctx, `INSERT INTO notifications (org_id, member_id, kind, task_id, actor_id, excerpt)
			SELECT t.org_id, t.assignee_id, 'due', t.id, t.created_by, to_char(t.due_at, 'YYYY-MM-DD')
			FROM tasks t JOIN agent_webhooks w ON w.org_id = t.org_id AND w.agent_id = t.assignee_id
			WHERE t.deleted_at IS NULL AND t.status NOT IN ('done', 'cancelled') AND t.due_at IS NOT NULL
				AND t.due_at < (now() AT TIME ZONE 'UTC')::date + 1
				AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.org_id = t.org_id AND n.task_id = t.id AND n.kind = 'due'
					AND n.member_id = t.assignee_id AND n.excerpt = to_char(t.due_at, 'YYYY-MM-DD'))
			ON CONFLICT (org_id, member_id, task_id, excerpt) WHERE kind = 'due' DO NOTHING`); err != nil {
			return err
		}
		// agent_done is for people only and has no webhook type; it's never sent.
		_, err = tx.Exec(ctx, `INSERT INTO webhook_deliveries (org_id, notification_id, agent_id)
			SELECT n.org_id, n.id, n.member_id FROM notifications n
			JOIN agent_webhooks w ON w.org_id = n.org_id AND w.agent_id = n.member_id
			-- shortcut: rescans 5 minutes back, so a notification committed later than that after it was stamped is missed; queue on write if transactions ever run that long.
			WHERE n.created_at >= greatest(w.since, w.scanned_at - interval '5 minutes') AND n.kind <> 'agent_done'
				AND NOT EXISTS (SELECT 1 FROM webhook_deliveries d WHERE d.org_id = n.org_id AND d.notification_id = n.id)
			ON CONFLICT DO NOTHING`)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE agent_webhooks SET scanned_at = now() WHERE org_id = $1`, org)
		return err
	})
	if err != nil {
		slog.Error("webhooks", "org", slug, "err", err)
		return
	}
	for _, j := range takeOns {
		s.enqueueReply(j)
	}
	for range 20 {
		d, ok := s.claimDelivery(ctx, org, slug)
		if !ok {
			return
		}
		body, _ := json.Marshal(d.payload)
		status, derr := s.post(ctx, d.hook, body)
		s.recordWebhook(ctx, org, d.hook.agentID, status, derr)
		s.finishDelivery(ctx, org, d, derr)
	}
}

type pendingDelivery struct {
	notification int64
	attempts     int // also the claim token: the result is only recorded if nobody claimed it since
	hook         webhookTarget
	payload      webhookPayload
}

// claimDelivery takes the next due delivery for 2 minutes, which comfortably covers one 15-second post.
func (s *Service) claimDelivery(ctx context.Context, org, slug string) (pendingDelivery, bool) {
	var d pendingDelivery
	found := false
	err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		var agent string
		err := tx.QueryRow(ctx, `UPDATE webhook_deliveries d SET next_attempt_at = now() + interval '2 minutes', attempts = d.attempts + 1
			WHERE (d.org_id, d.notification_id) IN (
				SELECT org_id, notification_id FROM webhook_deliveries
				WHERE delivered_at IS NULL AND failed_at IS NULL AND next_attempt_at <= now()
				ORDER BY next_attempt_at LIMIT 1 FOR UPDATE SKIP LOCKED)
			RETURNING d.notification_id, d.agent_id::text, d.attempts`).Scan(&d.notification, &agent, &d.attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		var sealed []byte
		var kind, actor, actorName, actorKind, agentName, excerpt string
		var created time.Time
		var task webhookTask
		var due *time.Time
		err = tx.QueryRow(ctx, `SELECT w.url, w.secret, n.kind, n.actor_id::text, a.name, a.kind, g.name, n.excerpt, n.created_at,
			t.id, t.title, t.status, t.due_at, coalesce(p.name, '')
			FROM notifications n
			JOIN agent_webhooks w ON w.org_id = n.org_id AND w.agent_id = n.member_id
			JOIN members a ON a.org_id = n.org_id AND a.id = n.actor_id
			JOIN members g ON g.org_id = n.org_id AND g.id = n.member_id
			JOIN tasks t ON t.org_id = n.org_id AND t.id = n.task_id
			LEFT JOIN projects p ON p.org_id = t.org_id AND p.id = t.project_id
			WHERE n.id = $1`, d.notification).Scan(&d.hook.url, &sealed, &kind, &actor, &actorName, &actorKind, &agentName, &excerpt, &created,
			&task.ID, &task.Title, &task.Status, &due, &task.Project)
		if errors.Is(err, pgx.ErrNoRows) {
			// The webhook was removed or the notification is gone: nothing to send.
			found = false
			_, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET failed_at = now(), last_error = 'webhook removed' WHERE notification_id = $1`, d.notification)
			return err
		}
		if err != nil {
			return err
		}
		if d.hook.secret, err = s.unseal(sealed); err != nil {
			found = false
			_, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET failed_at = now(), last_error = 'secret unreadable; rotate it' WHERE notification_id = $1`, d.notification)
			return err
		}
		d.hook.agentID = agent
		if due != nil {
			task.Due = due.UTC().Format("2006-01-02")
		}
		task.URL = s.Cfg.OrgURL(slug) + "/tasks/" + task.ID
		d.payload = webhookPayload{
			ID: strconv.FormatInt(d.notification, 10), Type: webhookTypes[kind], Workspace: slug,
			Agent: &MemberRef{ID: agent, Name: agentName, Kind: "agent"}, Task: &task,
			Actor: &MemberRef{ID: actor, Name: actorName, Kind: actorKind}, CreatedAt: created.UTC().Format(time.RFC3339),
		}
		if kind != "due" {
			d.payload.Excerpt = excerpt
		}
		return nil
	})
	if err != nil {
		slog.Error("webhooks: claim", "org", slug, "err", err)
		return d, false
	}
	return d, found
}

// finishDelivery records the outcome, unless another worker claimed the delivery since (attempts moved on).
func (s *Service) finishDelivery(ctx context.Context, org string, d pendingDelivery, derr error) {
	err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		if derr == nil {
			_, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET delivered_at = now(), last_error = ''
				WHERE notification_id = $1 AND attempts = $2`, d.notification, d.attempts)
			return err
		}
		if d.attempts >= webhookAttempts {
			_, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET failed_at = now(), last_error = $3
				WHERE notification_id = $1 AND attempts = $2`, d.notification, d.attempts, derr.Error())
			return err
		}
		// 30s, 1m, 2m, 4m... up to an hour.
		wait := min(30*time.Second<<(d.attempts-1), time.Hour)
		_, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET next_attempt_at = now() + make_interval(secs => $3), last_error = $4
			WHERE notification_id = $1 AND attempts = $2`, d.notification, d.attempts, wait.Seconds(), derr.Error())
		return err
	})
	if err != nil {
		slog.Error("webhooks: record", "err", err)
	}
}
