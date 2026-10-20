package core_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joalavedra/handoff/internal/core"
)

// Capture jobs sit behind the chat's own reply turns on the one job runner; on a loaded CI
// box that queue is slow, so the waits in this file are generous (45s).

// waitFact polls until a fact with this text is listed, or fails.
func waitFact(t *testing.T, e *env, p *core.Principal, text string) core.MemoryFactOut {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for {
		res, err := e.s.ListMemory(e.ctx, p, core.ListMemoryIn{})
		if err == nil {
			for _, f := range res.Facts {
				if f.Text == text {
					return f
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no fact %q remembered", text)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func factList(t *testing.T, e *env, p *core.Principal) []core.MemoryFactOut {
	t.Helper()
	res, err := e.s.ListMemory(e.ctx, p, core.ListMemoryIn{})
	if err != nil {
		t.Fatal(err)
	}
	return res.Facts
}

// jobRowsFor reports the latest capture job row; ran means a run finished (result written),
// even if a later trigger already re-pended it.
func jobRowsFor(t *testing.T, e *env, p *core.Principal, task string) (state, lastErr string, ran bool) {
	t.Helper()
	err := e.s.DB.InOrg(e.ctx, p.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, `SELECT state, last_error, result::text <> '{}' FROM agent_jobs
			WHERE kind = 'memory_capture' AND task_id = $1 ORDER BY created_at DESC LIMIT 1`, task).Scan(&state, &lastErr, &ran)
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}

// Six comments on a task the agent owns run a capture at once; each also gets a reply, so the
// model's reply queue needs as many answers. The captured facts land only in the task's own scope.
func TestCaptureOnAgentOwnedTask(t *testing.T) {
	srv, _ := scriptedModel(t, say("on it"), say("sure"), say("ok"), say("done"), say("right"), say("got it"))
	e, admin, bot := assistantEnv(t, srv, "capown")
	setCapture(t, `{"add": [
		{"scope": "person", "text": "Admin prefers morning standups"},
		{"scope": "project", "text": "Launch tasks ship Thursdays"},
		{"scope": "workspace", "text": "this should be dropped"}]}`)
	task := e.create(t, admin, core.NewTask{Title: "Plan the launch", Assignee: bot.Member.Name})[0]
	waitTriage(t, e, admin, task.ID)
	for i := range captureBurstN {
		if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: fmt.Sprintf("note %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	pf := waitFact(t, e, admin, "Admin prefers morning standups")
	if pf.Scope != "person" || pf.Member == nil || pf.Member.ID != admin.Member.ID || !pf.Noticed || pf.SourceTask != task.ID {
		t.Fatalf("person fact: %+v", pf)
	}
	pjf := waitFact(t, e, admin, "Launch tasks ship Thursdays")
	if pjf.Scope != "project" || !pjf.Noticed {
		t.Fatalf("project fact: %+v", pjf)
	}
	for _, f := range factList(t, e, admin) {
		if f.Text == "this should be dropped" {
			t.Fatalf("out-of-scope capture landed: %+v", f)
		}
	}
}

const captureBurstN = 6

// A finished reply on a task the agent doesn't own queues a capture too — delayed, so the chat can
// keep moving first.
func TestCaptureAfterReplyFinishes(t *testing.T) {
	srv, _ := scriptedModel(t, say("hello"), say("yes"), say("sure"), say("ok"), say("done"), say("noted"), say("gotcha"))
	e, admin, bot := assistantEnv(t, srv, "capreply")
	setCapture(t, `{"add": [{"scope": "person", "text": "Admin is travelling next week"}]}`)
	task := e.create(t, admin, core.NewTask{Title: "Week plan"})[0]
	waitTriage(t, e, admin, task.ID)
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot hi"}); err != nil {
		t.Fatal(err)
	}
	// Wait for the reply to land, then the capture job must exist for this task.
	deadline := time.Now().Add(45 * time.Second)
	for {
		if d, err := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID}); err == nil && len(d.Comments) > 1 && d.Comments[len(d.Comments)-1].Author.ID == bot.Member.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no reply")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var dueIn float64
	deadline = time.Now().Add(45 * time.Second)
	for {
		err := e.s.DB.InOrg(e.ctx, admin.Org.ID, func(tx pgx.Tx) error {
			return tx.QueryRow(e.ctx, `SELECT extract(epoch from due_at - now()) FROM agent_jobs
				WHERE kind = 'memory_capture' AND task_id = $1 AND state = 'pending'`, task.ID).Scan(&dueIn)
		})
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no capture job queued")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if dueIn < 30 || dueIn > 180 {
		t.Fatalf("capture due in %.0fs, want ~120s", dueIn)
	}
	// Keep the chat moving: at six new comments it runs at once.
	for i := 0; i < 5; i++ {
		if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: fmt.Sprintf("@Acme Bot more %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	f := waitFact(t, e, admin, "Admin is travelling next week")
	if f.Scope != "person" || f.SourceTask != task.ID {
		t.Fatalf("%+v", f)
	}
}

// Capture never re-adds what a person removed, dedupes what it already has, and supersedes only in scope.
func TestCaptureRespectsRemovedAndSupersedes(t *testing.T) {
	srv, _ := scriptedModel(t, say("on it"), say("sure"), say("ok"), say("done"), say("right"), say("got it"))
	e, admin, bot := assistantEnv(t, srv, "caprules")
	keep, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Admin reviews PRs daily"})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Admin likes tabs over spaces"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.RemoveMemory(e.ctx, admin, core.MemoryIDIn{ID: gone.Fact.ID}); err != nil {
		t.Fatal(err)
	}
	setCapture(t, fmt.Sprintf(`{"add": [{"scope": "person", "text": "Admin likes tabs over spaces"}],
		"supersede": [{"id": %q, "reason": "now weekly"}]}`, keep.Fact.ID))
	task := e.create(t, admin, core.NewTask{Title: "Chat", Assignee: bot.Member.Name})[0]
	waitTriage(t, e, admin, task.ID)
	for i := range captureBurstN {
		if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: fmt.Sprintf("note %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(45 * time.Second)
	for {
		if s, le, ran := jobRowsFor(t, e, admin, task.ID); ran {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("capture didn't run: state=%s err=%q", s, le)
		}
		time.Sleep(50 * time.Millisecond)
	}
	facts := factList(t, e, admin)
	for _, f := range facts {
		if f.Text == "Admin likes tabs over spaces" {
			t.Fatal("a removed fact came back")
		}
		if f.ID == keep.Fact.ID {
			t.Fatal("the superseded fact still lists")
		}
	}
	// The removed row stays on the record; the superseded one is marked, both invisible.
	var removed, superseded bool
	err = e.s.DB.InOrg(e.ctx, admin.Org.ID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(e.ctx, `SELECT removed_at IS NOT NULL FROM agent_memory WHERE id = $1`, gone.Fact.ID).Scan(&removed); err != nil {
			return err
		}
		return tx.QueryRow(e.ctx, `SELECT superseded_at IS NOT NULL FROM agent_memory WHERE id = $1`, keep.Fact.ID).Scan(&superseded)
	})
	if err != nil || !removed || !superseded {
		t.Fatalf("removed %v, superseded %v, err %v", removed, superseded, err)
	}
}

// A second run over the same chat reads nothing: the cursor sits past what was already read.
func TestCaptureReadsOnce(t *testing.T) {
	srv, _ := scriptedModel(t, say("on it"), say("sure"), say("ok"), say("done"), say("right"), say("got it"))
	e, admin, bot := assistantEnv(t, srv, "caponce")
	setCapture(t, `{"add": [{"scope": "project", "text": "Kickoff moved to Friday"}]}`)
	task := e.create(t, admin, core.NewTask{Title: "Timing", Assignee: bot.Member.Name})[0]
	waitTriage(t, e, admin, task.ID)
	for i := range captureBurstN {
		if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: fmt.Sprintf("note %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	waitFact(t, e, admin, "Kickoff moved to Friday")
	// Queue the same task's capture again by hand; with nothing new since, it must skip.
	err := e.s.DB.InOrg(e.ctx, admin.Org.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.ctx, `INSERT INTO agent_jobs (org_id, kind, task_id, due_at) VALUES ($1, 'memory_capture', $2, now())
			ON CONFLICT (org_id, kind, coalesce(task_id, '')) WHERE state = 'pending' DO UPDATE SET due_at = now()`, admin.Org.ID, task.ID)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	waker := e.create(t, admin, core.NewTask{Title: "Waker", Assignee: bot.Member.Name})[0]
	waitTriage(t, e, admin, waker.ID)
	for i := range captureBurstN {
		if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: waker.ID, Body: fmt.Sprintf("n %d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(45 * time.Second)
	for {
		var skipped bool
		var res string
		_ = e.s.DB.InOrg(e.ctx, admin.Org.ID, func(tx pgx.Tx) error {
			return tx.QueryRow(e.ctx, `SELECT result ? 'skipped' OR (result->>'added')::int = 0, result::text FROM agent_jobs
				WHERE kind = 'memory_capture' AND task_id = $1 ORDER BY created_at DESC LIMIT 1`, task.ID).Scan(&skipped, &res)
		})
		if skipped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("re-queued capture didn't skip the unchanged chat (last result %s)", res)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// And no second copy of the fact.
	var n int
	_ = e.s.DB.InOrg(e.ctx, admin.Org.ID, func(tx pgx.Tx) error {
		return tx.QueryRow(e.ctx, `SELECT count(*) FROM agent_memory WHERE text = 'Kickoff moved to Friday'`).Scan(&n)
	})
	if n != 1 {
		t.Fatalf("kickoff fact rows: %d", n)
	}
}
