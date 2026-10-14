package core

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
)

// Triage: when a person creates a task, the workspace's triage agent (an ordinary agent member,
// often the same one the team talks to in chat) reads it and tidies it up: a clean title, the
// fields the text implies, an owner when nobody was picked, and a short plan as its first comment.
// Every change goes through the normal tools as that agent, so permissions and history apply.
// The model is any OpenAI-compatible endpoint (a provider, a gateway like OpenRouter or the
// Vercel AI Gateway, or a local server), so each workspace picks its own LLM.

type TriageOut struct {
	Enabled       bool       `json:"enabled"`
	Agent         *MemberRef `json:"agent,omitempty"`
	BaseURL       string     `json:"base_url,omitempty"`
	Model         string     `json:"model,omitempty"`
	HasCredential bool       `json:"has_credential"`
	UpdatedAt     string     `json:"updated_at,omitempty"`
}

type SetTriageIn struct {
	Agent      string `json:"agent" jsonschema:"the agent that triages new tasks: id or name"`
	BaseURL    string `json:"base_url" jsonschema:"OpenAI-compatible API base, e.g. https://api.openai.com/v1, https://openrouter.ai/api/v1, https://ai-gateway.vercel.sh/v1, https://api.anthropic.com/v1"`
	Model      string `json:"model" jsonschema:"model id as the endpoint names it"`
	Credential string `json:"credential,omitempty" jsonschema:"API key or OAuth access token, sent as a bearer token; empty keeps the saved one"`
}

func (p *Principal) requireAdmin() error {
	if !p.IsAdmin() {
		return errf(ErrForbidden, "only workspace admins can change triage")
	}
	return nil
}

func (c *call) triageEnabled() (bool, error) {
	var on bool
	err := c.tx.QueryRow(c.ctx, `SELECT EXISTS (SELECT 1 FROM triage_settings)`).Scan(&on)
	return on, err
}

func (c *call) triageOut() (TriageOut, error) {
	var out TriageOut
	var agent string
	var cred []byte
	var updated time.Time
	err := c.tx.QueryRow(c.ctx, `SELECT agent_id::text, base_url, model, credential, updated_at FROM triage_settings`).
		Scan(&agent, &out.BaseURL, &out.Model, &cred, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return TriageOut{}, nil
	}
	if err != nil {
		return out, err
	}
	out.Enabled, out.Agent, out.HasCredential, out.UpdatedAt = true, c.ref(agent), len(cred) > 0, ts(&updated)
	return out, nil
}

func (s *Service) GetTriage(ctx context.Context, p *Principal, _ struct{}) (TriageOut, error) {
	var out TriageOut
	err := s.run(ctx, p, func(c *call) error {
		if err := p.requireAdmin(); err != nil {
			return err
		}
		var err error
		out, err = c.triageOut()
		return err
	})
	return out, err
}

func (s *Service) SetTriage(ctx context.Context, p *Principal, in SetTriageIn) (TriageOut, error) {
	var out TriageOut
	err := s.run(ctx, p, func(c *call) error {
		if err := p.requireAdmin(); err != nil {
			return err
		}
		m, err := c.resolveMember(in.Agent)
		if err != nil {
			return err
		}
		if !m.IsAgent() || m.Status != "active" {
			return errf(ErrInvalid, "%s is not an active agent", m.Name)
		}
		if !slices.Contains(m.Permissions.Actions, ActUpdate) {
			return errf(ErrInvalid, "%s needs the update permission to triage tasks", m.Name)
		}
		base, err := triageBaseURL(in.BaseURL)
		if err != nil {
			return err
		}
		model := strings.TrimSpace(in.Model)
		if model == "" || len(model) > 200 {
			return errf(ErrInvalid, "model is required")
		}
		var sealed []byte
		if cred := strings.TrimSpace(in.Credential); cred != "" {
			if sealed, err = s.seal([]byte(cred)); err != nil {
				return err
			}
		} else if err := c.tx.QueryRow(c.ctx, `SELECT credential FROM triage_settings`).Scan(&sealed); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errf(ErrInvalid, "an API key or OAuth token is required")
			}
			return err
		}
		_, err = c.tx.Exec(c.ctx, `INSERT INTO triage_settings (org_id, agent_id, base_url, model, credential, updated_by, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			ON CONFLICT (org_id) DO UPDATE SET agent_id = $2, base_url = $3, model = $4, credential = $5, updated_by = $6, updated_at = now()`,
			p.Org.ID, m.ID, base, model, sealed, p.Member.ID)
		if err != nil {
			return err
		}
		out, err = c.triageOut()
		return err
	})
	return out, err
}

func (s *Service) DisableTriage(ctx context.Context, p *Principal, _ struct{}) (TriageOut, error) {
	err := s.run(ctx, p, func(c *call) error {
		if err := p.requireAdmin(); err != nil {
			return err
		}
		if _, err := c.tx.Exec(c.ctx, `DELETE FROM triage_settings`); err != nil {
			return err
		}
		// Nothing will pick these up any more.
		_, err := c.tx.Exec(c.ctx, `UPDATE tasks SET triage = NULL WHERE triage = 'pending'`)
		return err
	})
	return TriageOut{}, err
}

func triageBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errf(ErrInvalid, "base_url must be an http(s) URL such as https://api.openai.com/v1")
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/"), nil
}

// ---- credential sealing ----

func (s *Service) sealKey() []byte {
	k := sha256.Sum256(append([]byte("handoff triage credential\x00"), s.Cfg.Secret...))
	return k[:]
}

func (s *Service) seal(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.sealKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func (s *Service) unseal(sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.sealKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("short credential")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
}

// ---- worker ----

type triageJob struct{ org, task string }

func (s *Service) enqueueTriage(org, task string) {
	if s.triageJobs == nil {
		return
	}
	select {
	case s.triageJobs <- triageJob{org, task}:
	default:
		// Full: the periodic sweep picks it up.
	}
}

// RunTriage evaluates newly created tasks until ctx ends. Tasks still pending after a restart
// or a full queue are found by a sweep every minute.
func (s *Service) RunTriage(ctx context.Context) {
	sem := make(chan struct{}, 4)
	var mu sync.Mutex
	inflight := map[triageJob]bool{}
	start := func(j triageJob) {
		mu.Lock()
		if inflight[j] {
			mu.Unlock()
			return
		}
		inflight[j] = true
		mu.Unlock()
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		go func() {
			defer func() {
				<-sem
				mu.Lock()
				delete(inflight, j)
				mu.Unlock()
			}()
			s.triageOne(ctx, j)
		}()
	}
	sweep := func(olderThan time.Duration) {
		for _, j := range s.pendingTriage(ctx, olderThan) {
			start(j)
		}
	}
	sweep(0)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-s.triageJobs:
			start(j)
		case <-tick.C:
			sweep(time.Minute)
		}
	}
}

func (s *Service) pendingTriage(ctx context.Context, olderThan time.Duration) []triageJob {
	rows, err := s.DB.Pool.Query(ctx, `SELECT id::text FROM orgs`)
	if err != nil {
		slog.Error("triage sweep", "err", err)
		return nil
	}
	orgs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		slog.Error("triage sweep", "err", err)
		return nil
	}
	var jobs []triageJob
	for _, org := range orgs {
		_ = s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT id FROM tasks WHERE triage = 'pending' AND created_at < now() - make_interval(secs => $1)
				ORDER BY created_at LIMIT 100`, olderThan.Seconds())
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
			for _, id := range ids {
				jobs = append(jobs, triageJob{org, id})
			}
			return err
		})
	}
	return jobs
}

type triageSettings struct {
	AgentID, BaseURL, Model string
	Credential              []byte
}

type triageTask struct {
	Title, Body string
	Explicit    []string
	State       string
}

func (s *Service) triageOne(ctx context.Context, j triageJob) {
	org, err := s.OrgByID(ctx, j.org)
	if err != nil {
		slog.Error("triage", "task", j.task, "err", err)
		return
	}
	var st *triageSettings
	var t triageTask
	err = s.DB.InOrg(ctx, j.org, func(tx pgx.Tx) error {
		var x triageSettings
		err := tx.QueryRow(ctx, `SELECT agent_id::text, base_url, model, credential FROM triage_settings`).
			Scan(&x.AgentID, &x.BaseURL, &x.Model, &x.Credential)
		if err == nil {
			st = &x
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return tx.QueryRow(ctx, `SELECT title, body, triage_explicit, coalesce(triage, '') FROM tasks WHERE id = $1 AND deleted_at IS NULL`, j.task).
			Scan(&t.Title, &t.Body, &t.Explicit, &t.State)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && t.State != "pending") {
		return
	}
	if err != nil {
		slog.Error("triage", "task", j.task, "err", err)
		return
	}
	if st == nil {
		s.endTriage(ctx, j, "", "", nil)
		return
	}
	agent, err := s.Principal(ctx, org, st.AgentID, "triage")
	if err != nil {
		s.endTriage(ctx, j, st.AgentID, "the triage agent can't act: "+err.Error(), nil)
		return
	}
	cred, err := s.unseal(st.Credential)
	if err != nil {
		s.endTriage(ctx, j, st.AgentID, "the saved model credential can't be read (was HANDOFF_SECRET changed?); an admin needs to enter it again", nil)
		return
	}
	projects, err := s.ListProjects(ctx, agent, struct{}{})
	if err != nil {
		s.endTriage(ctx, j, st.AgentID, err.Error(), nil)
		return
	}
	members, err := s.ListMembers(ctx, agent, struct{}{})
	if err != nil {
		s.endTriage(ctx, j, st.AgentID, err.Error(), nil)
		return
	}
	lctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	sugg, err := s.askModel(lctx, st, string(cred), org, t, projects.Projects, members.Members)
	if err != nil {
		s.endTriage(ctx, j, st.AgentID, err.Error(), nil)
		return
	}
	changed, err := s.applyTriage(ctx, agent, j.task, t, sugg, projects.Projects, members.Members)
	if err != nil {
		s.endTriage(ctx, j, st.AgentID, err.Error(), changed)
		return
	}
	s.endTriage(ctx, j, st.AgentID, "", changed)
}

// endTriage records the outcome. An empty agentID means triage was switched off: the mark is
// simply cleared.
func (s *Service) endTriage(ctx context.Context, j triageJob, agentID, failure string, changed []string) {
	err := s.DB.InOrg(ctx, j.org, func(tx pgx.Tx) error {
		if agentID == "" {
			_, err := tx.Exec(ctx, `UPDATE tasks SET triage = NULL WHERE id = $1 AND triage = 'pending'`, j.task)
			return err
		}
		state, kind, data := "done", "triaged", map[string]any{"changed": nonNil(changed)}
		if failure != "" {
			if len(failure) > 300 {
				failure = failure[:300] + "…"
			}
			state, kind, data = "failed", "triage_failed", map[string]any{"error": failure}
		}
		if _, err := tx.Exec(ctx, `UPDATE tasks SET triage = $2, triage_error = $3 WHERE id = $1`, j.task, state, failure); err != nil {
			return err
		}
		b, _ := json.Marshal(data)
		// The event also tells open screens to refresh.
		_, err := tx.Exec(ctx, `INSERT INTO events (org_id, task_id, actor_id, via, kind, data) VALUES ($1, $2, $3, 'triage', $4, $5)`,
			j.org, j.task, agentID, kind, b)
		return err
	})
	if err != nil {
		slog.Error("triage: record outcome", "task", j.task, "err", err)
	}
	if failure != "" {
		slog.Warn("triage failed", "task", j.task, "reason", failure)
	}
}

// ---- the model ----

type triageSuggestion struct {
	Title    string   `json:"title"`
	Due      string   `json:"due"`
	Priority *int     `json:"priority"`
	Project  string   `json:"project"`
	Labels   []string `json:"labels"`
	Assignee string   `json:"assignee"`
	Plan     string   `json:"plan"`
}

const triagePrompt = `You triage new tasks in a shared task list used by a team of people and AI agents.
Read the task and reply with one JSON object and nothing else:
{"title": string, "due": "YYYY-MM-DD" or null, "priority": 0-4 or null, "project": string or null, "labels": [string], "assignee": string or null, "plan": string}

- title: the task as a short imperative (at most 80 characters), in the same language, keeping every specific detail (names, amounts, places). Drop filler like "can you", "I need to", and drop words that only state the due date. If it is already clean, return it unchanged.
- due: only if the text implies a date or deadline. Resolve relative dates ("friday", "tomorrow", "end of month") against today's date.
- priority: only if the text signals urgency (0 urgent, 1 high) or that it can wait (3 low, 4 someday). Otherwise null.
- project: only if one of the listed projects clearly fits; use its exact name. Otherwise null.
- labels: up to 3 short lowercase labels, preferring ones already used. Empty if none fit.
- assignee: only if the text names or clearly implies one of the listed members; use the exact name. Otherwise null.
- plan: markdown, at most 600 characters: one line starting "Done when", then 2-5 short steps, then any open questions. Be concrete; no preamble.
Never invent facts that are not in the task.`

func (s *Service) askModel(ctx context.Context, st *triageSettings, cred string, org *Org, t triageTask, projects []ProjectOut, members []MemberOut) (triageSuggestion, error) {
	var sugg triageSuggestion
	var b strings.Builder
	now := time.Now().UTC()
	fmt.Fprintf(&b, "Workspace: %s\nToday: %s (%s)\n\nProjects:\n", org.Name, now.Format("2006-01-02"), now.Weekday())
	for _, p := range projects {
		fmt.Fprintf(&b, "- %s\n", p.Name)
	}
	b.WriteString("\nMembers:\n")
	for _, m := range members {
		if m.Status == "active" {
			fmt.Fprintf(&b, "- %s (%s)\n", m.Name, map[string]string{"agent": "agent", "human": "person"}[m.Kind])
		}
	}
	if len(t.Explicit) > 0 {
		fmt.Fprintf(&b, "\nThe creator already chose: %s. Return null for those.\n", strings.Join(t.Explicit, ", "))
	}
	fmt.Fprintf(&b, "\nTask title: %s\n", t.Title)
	if strings.TrimSpace(t.Body) != "" {
		body := t.Body
		if len(body) > 4000 {
			body = body[:4000]
		}
		fmt.Fprintf(&b, "Task details:\n%s\n", body)
	}
	req, _ := json.Marshal(map[string]any{
		"model": st.Model,
		"messages": []map[string]string{
			{"role": "system", "content": triagePrompt},
			{"role": "user", "content": b.String()},
		},
		"max_tokens":      900,
		"response_format": map[string]string{"type": "json_object"},
	})
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, st.BaseURL+"/chat/completions", bytes.NewReader(req))
	if err != nil {
		return sugg, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Authorization", "Bearer "+cred)
	resp, err := s.triageClient().Do(hr)
	if err != nil {
		return sugg, fmt.Errorf("model endpoint unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return sugg, fmt.Errorf("model endpoint returned %d: %s", resp.StatusCode, msg)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return sugg, errors.New("model endpoint returned an unexpected response")
	}
	text := out.Choices[0].Message.Content
	i, k := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if i < 0 || k < i {
		return sugg, errors.New("the model did not answer with JSON")
	}
	if err := json.Unmarshal([]byte(text[i:k+1]), &sugg); err != nil {
		return sugg, errors.New("the model's JSON could not be read")
	}
	return sugg, nil
}

// triageClient refuses private, loopback and link-local addresses unless this server is a single
// workspace or in dev mode, so one workspace's admin can't point the server at internal services.
func (s *Service) triageClient() *http.Client {
	if s.TriageHTTP != nil {
		return s.TriageHTTP
	}
	allowPrivate := s.Cfg.Dev || s.Cfg.SingleOrg != ""
	d := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		if allowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("refusing to call %s: private address", host)
		}
		return nil
	}}
	return &http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{DialContext: d.DialContext, TLSHandshakeTimeout: 10 * time.Second}}
}

// applyTriage makes the suggested changes as the triage agent. A field is only filled when the
// creator didn't set it and it still holds its default, so edits made meanwhile always win.
func (s *Service) applyTriage(ctx context.Context, agent *Principal, id string, t triageTask, sugg triageSuggestion, projects []ProjectOut, members []MemberOut) ([]string, error) {
	cur, err := s.GetTask(ctx, agent, TaskIDIn{ID: id})
	if err != nil {
		return nil, err
	}
	task := cur.Task
	explicit := func(f string) bool { return slices.Contains(t.Explicit, f) }
	upd := UpdateTaskIn{ID: id}
	var changed []string
	if title := strings.TrimSpace(sugg.Title); title != "" && len(title) <= 300 && title != task.Title && task.Title == t.Title {
		upd.Title, changed = &title, append(changed, "title")
	}
	if due := strings.TrimSpace(sugg.Due); due != "" && !explicit("due") && task.Due == "" {
		if _, err := time.Parse("2006-01-02", due); err == nil {
			upd.Due, changed = &due, append(changed, "due")
		}
	}
	if p := sugg.Priority; p != nil && *p >= 0 && *p <= 4 && *p != 2 && !explicit("priority") && task.Priority == 2 {
		upd.Priority, changed = p, append(changed, "priority")
	}
	if name := strings.TrimSpace(sugg.Project); name != "" && !explicit("project") && task.Project == DefaultProject {
		for _, p := range projects {
			if strings.EqualFold(p.Name, name) && p.Name != DefaultProject {
				upd.Project, changed = p.ID, append(changed, "project")
				break
			}
		}
	}
	if len(sugg.Labels) > 0 && !explicit("labels") && len(task.Labels) == 0 {
		labels := sugg.Labels
		if len(labels) > 3 {
			labels = labels[:3]
		}
		upd.Labels, changed = labels, append(changed, "labels")
	}
	if name := strings.TrimSpace(sugg.Assignee); name != "" && !explicit("assignee") && task.Assignee == nil {
		for _, m := range members {
			if strings.EqualFold(m.Name, name) && m.Status == "active" {
				who := m.ID
				upd.Assignee, changed = &who, append(changed, "assignee")
				break
			}
		}
	}
	if len(changed) > 0 {
		if _, err := s.UpdateTask(ctx, agent, upd); err != nil {
			return nil, err
		}
	}
	if plan := strings.TrimSpace(sugg.Plan); plan != "" {
		if len(plan) > 2000 {
			plan = plan[:2000]
		}
		if _, err := s.Comment(ctx, agent, CommentIn{ID: id, Body: plan}); err != nil {
			return changed, err
		}
		changed = append(changed, "plan")
	}
	return changed, nil
}
