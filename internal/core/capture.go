package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Quiet-chat capture: when a person talks on a task the agent owns, or once the agent's reply lands,
// a memory_capture job is queued for the task. It waits a couple of minutes for the conversation to
// settle (six new comments run it at once), then the background model files what the chat taught it:
// facts about the person and the work — noticed, so anything a person removed stays removed.

func init() {
	jobKinds["memory_capture"] = func(ctx context.Context, s *Service, j *agentJob) (map[string]any, error) {
		return s.captureMemory(ctx, j)
	}
}

const captureDelay = 2 * time.Minute
const captureBurst = 6 // comments since the last capture that run it at once

// enqueueCapture queues the task's capture job: at once once the chat is moving fast, else after a
// short wait for more of the conversation to land. The pending job's due time is whichever settled
// first — a burst keeps it due; otherwise it quiet-extends with the latest comment.
func enqueueCapture(ctx context.Context, tx pgx.Tx, org, task, member string) (bool, error) {
	var burst bool
	if err := tx.QueryRow(ctx, `SELECT count(*) >= $2 FROM comments c JOIN tasks t ON t.org_id = c.org_id AND t.id = c.task_id
		WHERE c.task_id = $1 AND c.created_at > coalesce(t.captured_at, '-infinity'::timestamptz)`, task, captureBurst).Scan(&burst); err != nil {
		return false, err
	}
	delay := captureDelay
	if burst {
		delay = 0
	}
	var dueNow bool
	err := tx.QueryRow(ctx, `INSERT INTO agent_jobs (org_id, kind, task_id, member_id, due_at)
		VALUES ($1, 'memory_capture', $2, nullif($3, '')::uuid, now() + make_interval(secs => $4))
		ON CONFLICT (org_id, kind, coalesce(task_id, '')) WHERE state = 'pending' DO UPDATE SET
			triggers = agent_jobs.triggers + 1,
			member_id = coalesce(excluded.member_id, agent_jobs.member_id),
			due_at = CASE WHEN least(agent_jobs.due_at, excluded.due_at) <= now()
				THEN least(agent_jobs.due_at, excluded.due_at)
				ELSE greatest(agent_jobs.due_at, excluded.due_at) END,
			updated_at = now()
		RETURNING due_at <= now()`, org, task, member, max(delay, 0).Seconds()).Scan(&dueNow)
	return dueNow, err
}

func (s *Service) enqueueCapture(ctx context.Context, org, task, member string) {
	var dueNow bool
	err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		var err error
		dueNow, err = enqueueCapture(ctx, tx, org, task, member)
		return err
	})
	if err != nil {
		slog.Error("capture: enqueue", "task", task, "err", err)
		return
	}
	if dueNow {
		s.wakeJobs()
	}
}

const captureNote = `You consolidate what the workspace's agent should remember from a task's chat. Answer with JSON only:
{"add": [{"scope": "person" or "project", "text": "one short fact in a sentence", "until": "YYYY-MM-DD"?}], "supersede": [{"id", "reason"}]}
- At most 5 adds. Facts worth keeping: who people are and what they prefer, decisions the chat settled, outcomes now true, constraints that will still matter later.
- A person's words can give facts about them and the work. Lines starting "you:" are the agent's own messages: they only give facts it verified or the person agreed to.
- Scope: "person" is only for facts about %s; "project" is only about the %s project. Nothing wider, nothing about anyone else.
- Facts already remembered and ones people removed are listed: never offer them again. "supersede" is only for a listed fact the chat now shows wrong, by its id.
- Do not capture: failures that depend on setup (capture the fix instead), claims that a tool does not work, errors that went away, one-off errands, secrets or credentials.
- Keep each fact a short sentence from the chat, not a summary of the task.`

type captureLine struct {
	author string
	agent  bool
	name   string
	body   string
	at     time.Time
}

type captureAdd struct {
	Scope string `json:"scope"`
	Text  string `json:"text"`
	Until string `json:"until"`
}

type captureOut struct {
	Add       []captureAdd `json:"add"`
	Supersede []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"supersede"`
}

func (s *Service) captureMemory(ctx context.Context, j *agentJob) (map[string]any, error) {
	st, cred, err := s.backgroundModel(ctx, j.Org)
	if err != nil {
		return nil, err
	}
	org, err := s.OrgByID(ctx, j.Org)
	if err != nil {
		return nil, err
	}
	agent, err := s.Principal(ctx, org, st.AgentID, "triage")
	if err != nil {
		return nil, err
	}
	var title, project, projectName string
	var capturedAt *time.Time
	var chat []captureLine
	var member *Member
	var facts, removed []*memoryRow
	var noteTitles []string
	err = s.DB.InOrg(ctx, j.Org, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT t.title, t.project_id::text, p.name, t.captured_at FROM tasks t
			JOIN projects p ON p.org_id = t.org_id AND p.id = t.project_id
			WHERE t.id = $1 AND t.deleted_at IS NULL`, j.Task).Scan(&title, &project, &projectName, &capturedAt)
		if err != nil {
			return err
		}
		if j.Member != "" {
			member, err = scanMember(tx.QueryRow(ctx, `SELECT `+memberCols+` FROM members WHERE id = $1`, j.Member))
		}
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]any{"skipped": "task gone"}, nil
	}
	if err != nil {
		return nil, err
	}
	if member != nil {
		agent.Owner = member
	}
	// The gather runs as the member again: someone who lost the project since commenting sees
	// nothing of its chat — there is nothing to capture either way.
	var noAccess bool
	err = s.run(ctx, agent, func(c *call) error {
		if member != nil && !c.canSeeProject(project) {
			noAccess = true
			return nil
		}
		// The comments it hasn't read yet, oldest forty: the rest still count as unread, so the
		// next trigger picks up where this run stopped.
		rows, err := c.tx.Query(ctx, `SELECT c.author_id::text, c.author_id::text = $2, m.name, c.body, c.created_at FROM comments c
			JOIN members m ON m.org_id = c.org_id AND m.id = c.author_id
			WHERE c.task_id = $1 AND c.created_at > coalesce($3, '-infinity'::timestamptz)
			ORDER BY c.created_at ASC LIMIT 40`, j.Task, st.AgentID, capturedAt)
		if err != nil {
			return err
		}
		chat, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (captureLine, error) {
			var l captureLine
			return l, r.Scan(&l.author, &l.agent, &l.name, &l.body, &l.at)
		})
		if err != nil || len(chat) == 0 {
			return err
		}
		// What it already remembers in this scope (to dedupe and to supersede), then what people
		// removed (never to offer again). Rows are capped: the lists are context, not the corpus.
		facts, err = queryMemoryTx(ctx, c.tx, `m.removed_at IS NULL AND m.superseded_at IS NULL
			AND (m.until_date IS NULL OR m.until_date >= CURRENT_DATE)
			AND (m.scope = 'workspace' OR (m.scope = 'project' AND m.project_id = $1::uuid)
				OR (m.scope = 'person' AND m.member_id = nullif($2, '')::uuid))`, project, j.Member)
		if err != nil {
			return err
		}
		removed, err = queryMemoryTx(ctx, c.tx, `m.removed_at IS NOT NULL
			AND (m.scope = 'workspace' OR (m.scope = 'project' AND m.project_id = $1::uuid)
				OR (m.scope = 'person' AND m.member_id = nullif($2, '')::uuid))
			ORDER BY m.removed_at DESC LIMIT 20`, project, j.Member)
		if err != nil {
			return err
		}
		rows, err = c.tx.Query(ctx, `SELECT title FROM notes WHERE project_id = $1::uuid AND deleted_at IS NULL
			ORDER BY updated_at DESC LIMIT 20`, project)
		if err != nil {
			return err
		}
		noteTitles, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return nil, err
	}
	if noAccess {
		return map[string]any{"skipped": "no access"}, nil
	}
	if len(chat) == 0 {
		return map[string]any{"skipped": "nothing new"}, nil
	}
	memberName := "the person"
	if member != nil {
		memberName = member.Name
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Task %q in the %s project, chat since last looked (oldest first):\n<<<\n", title, projectName)
	for _, l := range chat {
		if l.agent {
			fmt.Fprintf(&b, "you: %s\n", clipBytes(l.body, 2000))
		} else {
			fmt.Fprintf(&b, "%s: %s\n", l.name, clipBytes(l.body, 2000))
		}
	}
	b.WriteString(">>>\n")
	if len(facts) > 0 {
		b.WriteString("\nAlready remembered (id — scope — fact):\n")
		for _, f := range facts {
			label := f.Scope
			if f.Scope == "person" {
				label = "person (about " + memberName + ")"
			} else if f.Scope == "project" {
				label = "project " + projectName
			}
			fmt.Fprintf(&b, "- %s — %s — %s\n", f.ID, label, f.Text)
		}
	}
	if len(removed) > 0 {
		b.WriteString("\nRemoved, never offer again:\n")
		for _, f := range removed {
			fmt.Fprintf(&b, "- %s\n", f.Text)
		}
	}
	if len(noteTitles) > 0 {
		b.WriteString("\nThe project's notes (context): " + strings.Join(noteTitles, ", ") + "\n")
	}
	out, err := completeJSON[captureOut](ctx, s, st, cred, []map[string]any{
		{"role": "system", "content": fmt.Sprintf(captureNote, memberName, projectName)},
		{"role": "user", "content": clipBytes(b.String(), 30000)},
	})
	if err != nil {
		return nil, err
	}

	// Person facts only bind when the window has a single person talking — with several authors,
	// whose fact is whose can't be told apart, so person adds are dropped (project adds still land).
	persons := map[string]bool{}
	for _, l := range chat {
		if !l.agent {
			persons[l.author] = true
		}
	}
	oneAuthor := member != nil && len(persons) == 1 && persons[member.ID]
	// Server-side: a fact lands only in the person/project this chat is about, cleaned like any
	// memory write; a supersede only touches a fact in that same scope. Anything else is dropped.
	var added, superseded int
	err = s.run(ctx, agent, func(c *call) error {
		live := map[string]*memoryRow{}
		liveText := map[string]bool{}
		for _, f := range facts {
			live[f.ID] = f
			liveText[f.Scope+"\x00"+f.ProjectID+"\x00"+f.MemberID+"\x00"+normFact(f.Text)] = true
		}
		for i, a := range out.Add {
			if i >= 5 {
				break
			}
			in := RememberIn{Text: a.Text, Until: a.Until, Task: j.Task, Noticed: true}
			switch strings.ToLower(strings.TrimSpace(a.Scope)) {
			case "person":
				if !oneAuthor {
					continue
				}
				in.Scope, in.Member = "person", member.ID
			case "project":
				in.Scope, in.Project = "project", project
			default:
				continue
			}
			// Restating a live fact never renews it with a shorter leash — the saved expiry stands.
			if liveText[in.Scope+"\x00"+in.Project+"\x00"+in.Member+"\x00"+normFact(in.Text)] {
				in.Until = ""
			}
			if _, ok, err := c.remember(in); err != nil {
				continue // refused facts (secret-shaped, malformed) are dropped, not retried
			} else if ok {
				added++
			}
		}
		for i, sup := range out.Supersede {
			if i >= 10 { // a model gone sideways retires at most a page of facts per run
				break
			}
			f, ok := live[sup.ID]
			if !ok || (f.Scope == "person" && f.MemberID != j.Member) || (f.Scope == "project" && f.ProjectID != project) || f.Scope == "workspace" {
				continue
			}
			tag, err := c.tx.Exec(c.ctx, `UPDATE agent_memory SET superseded_at = now(), updated_at = now()
				WHERE id = $1 AND superseded_at IS NULL AND removed_at IS NULL`, sup.ID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() > 0 {
				superseded++
			}
		}
		// Read up to the newest comment seen: a retry never re-reads it, and anything newer waits
		// for the next trigger (the job re-queues itself while it ran).
		var latest time.Time
		for _, l := range chat {
			if l.at.After(latest) {
				latest = l.at
			}
		}
		_, err := c.tx.Exec(c.ctx, `UPDATE tasks SET captured_at = $2 WHERE id = $1`, j.Task, latest)
		return err
	})
	if err != nil {
		return nil, err
	}
	// A full window means more comments wait past the cursor: queue the next read now instead of
	// hoping another comment comes along to trigger it.
	if len(chat) == 40 {
		s.enqueueCapture(ctx, j.Org, j.Task, j.Member)
	}
	return map[string]any{"added": added, "superseded": superseded}, nil
}
