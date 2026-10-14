package core

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
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
	"time"

	"github.com/jackc/pgx/v5"
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
			return errf(ErrInvalid, "url must be an http(s) URL")
		}
		existing, err := c.webhookOut(m.ID)
		if err != nil {
			return err
		}
		var secret string
		if existing == nil || in.Rotate {
			b := make([]byte, 24)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			secret = "whsec_" + hex.EncodeToString(b)
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
		s.DeliverWebhooks(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// DeliverWebhooks runs one delivery round over every workspace.
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
	for _, o := range orgs {
		s.webhookTick(ctx, o.id, o.slug)
	}
}

type pendingDelivery struct {
	notification int64
	attempts     int
	hook         webhookTarget
	payload      webhookPayload
}

// webhookTick does one round for a workspace: note tasks that came due for agents, queue new
// notifications for agents with a webhook, then claim and send what's due.
func (s *Service) webhookTick(ctx context.Context, org, slug string) {
	var batch []pendingDelivery
	err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		// Due today (UTC) and assigned to an agent with a webhook: one "due" notification per due date.
		if _, err := tx.Exec(ctx, `INSERT INTO notifications (org_id, member_id, kind, task_id, actor_id, excerpt)
			SELECT t.org_id, t.assignee_id, 'due', t.id, t.created_by, to_char(t.due_at, 'YYYY-MM-DD')
			FROM tasks t JOIN agent_webhooks w ON w.org_id = t.org_id AND w.agent_id = t.assignee_id
			WHERE t.deleted_at IS NULL AND t.status NOT IN ('done', 'cancelled') AND t.due_at IS NOT NULL
				AND t.due_at < (now() AT TIME ZONE 'UTC')::date + 1
				AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.org_id = t.org_id AND n.task_id = t.id
					AND n.kind = 'due' AND n.excerpt = to_char(t.due_at, 'YYYY-MM-DD'))`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO webhook_deliveries (org_id, notification_id, agent_id)
			SELECT n.org_id, n.id, n.member_id FROM notifications n
			JOIN agent_webhooks w ON w.org_id = n.org_id AND w.agent_id = n.member_id
			WHERE n.created_at >= w.since
				AND NOT EXISTS (SELECT 1 FROM webhook_deliveries d WHERE d.org_id = n.org_id AND d.notification_id = n.id)
			ON CONFLICT DO NOTHING`); err != nil {
			return err
		}
		// Claim a batch: the lease keeps other servers off it while it's being sent.
		rows, err := tx.Query(ctx, `UPDATE webhook_deliveries d SET next_attempt_at = now() + interval '2 minutes', attempts = d.attempts + 1
			WHERE (d.org_id, d.notification_id) IN (
				SELECT org_id, notification_id FROM webhook_deliveries
				WHERE delivered_at IS NULL AND failed_at IS NULL AND next_attempt_at <= now()
				ORDER BY next_attempt_at LIMIT 20 FOR UPDATE SKIP LOCKED)
			RETURNING d.notification_id, d.agent_id::text, d.attempts`)
		if err != nil {
			return err
		}
		type claim struct {
			id       int64
			agent    string
			attempts int
		}
		claims, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (claim, error) {
			var c claim
			return c, r.Scan(&c.id, &c.agent, &c.attempts)
		})
		if err != nil {
			return err
		}
		for _, cl := range claims {
			d := pendingDelivery{notification: cl.id, attempts: cl.attempts}
			var sealed []byte
			var kind, actor, actorName, actorKind, agentName, excerpt string
			var created time.Time
			var task webhookTask
			var due *time.Time
			err := tx.QueryRow(ctx, `SELECT w.url, w.secret, n.kind, n.actor_id::text, a.name, a.kind, g.name, n.excerpt, n.created_at,
				t.id, t.title, t.status, t.due_at, coalesce(p.name, '')
				FROM notifications n
				JOIN agent_webhooks w ON w.org_id = n.org_id AND w.agent_id = n.member_id
				JOIN members a ON a.org_id = n.org_id AND a.id = n.actor_id
				JOIN members g ON g.org_id = n.org_id AND g.id = n.member_id
				JOIN tasks t ON t.org_id = n.org_id AND t.id = n.task_id
				LEFT JOIN projects p ON p.org_id = t.org_id AND p.id = t.project_id
				WHERE n.id = $1`, cl.id).Scan(&d.hook.url, &sealed, &kind, &actor, &actorName, &actorKind, &agentName, &excerpt, &created,
				&task.ID, &task.Title, &task.Status, &due, &task.Project)
			if errors.Is(err, pgx.ErrNoRows) {
				// The webhook was removed or the notification is gone: nothing to send.
				if _, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET failed_at = now(), last_error = 'webhook removed' WHERE notification_id = $1`, cl.id); err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			if d.hook.secret, err = s.unseal(sealed); err != nil {
				continue
			}
			d.hook.agentID = cl.agent
			if due != nil {
				task.Due = due.UTC().Format("2006-01-02")
			}
			task.URL = s.Cfg.OrgURL(slug) + "/tasks/" + task.ID
			d.payload = webhookPayload{
				ID: strconv.FormatInt(cl.id, 10), Type: webhookTypes[kind], Workspace: slug,
				Agent: &MemberRef{ID: cl.agent, Name: agentName, Kind: "agent"}, Task: &task,
				Actor: &MemberRef{ID: actor, Name: actorName, Kind: actorKind}, CreatedAt: created.UTC().Format(time.RFC3339),
			}
			if kind != "due" {
				d.payload.Excerpt = excerpt
			}
			batch = append(batch, d)
		}
		return nil
	})
	if err != nil {
		slog.Error("webhooks", "org", slug, "err", err)
		return
	}
	for _, d := range batch {
		body, _ := json.Marshal(d.payload)
		status, derr := s.post(ctx, d.hook, body)
		s.recordWebhook(ctx, org, d.hook.agentID, status, derr)
		err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
			if derr == nil {
				_, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET delivered_at = now(), last_error = '' WHERE notification_id = $1`, d.notification)
				return err
			}
			if d.attempts >= webhookAttempts {
				_, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET failed_at = now(), last_error = $2 WHERE notification_id = $1`, d.notification, derr.Error())
				return err
			}
			// 30s, 1m, 2m, 4m... up to an hour.
			wait := min(30*time.Second<<(d.attempts-1), time.Hour)
			_, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET next_attempt_at = now() + make_interval(secs => $2), last_error = $3 WHERE notification_id = $1`,
				d.notification, wait.Seconds(), derr.Error())
			return err
		})
		if err != nil {
			slog.Error("webhooks: record", "org", slug, "err", err)
		}
	}
}
