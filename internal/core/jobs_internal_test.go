package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// testJob is a job kind whose behaviour each test sets.
var testJob struct {
	sync.Mutex
	run func(ctx context.Context, s *Service, j *agentJob) (map[string]any, error)
}

func init() {
	jobKinds["test"] = func(ctx context.Context, s *Service, j *agentJob) (map[string]any, error) {
		testJob.Lock()
		run := testJob.run
		testJob.Unlock()
		return run(ctx, s, j)
	}
}

func setTestJob(t *testing.T, run func(ctx context.Context, s *Service, j *agentJob) (map[string]any, error)) {
	t.Helper()
	testJob.Lock()
	testJob.run = run
	testJob.Unlock()
	t.Cleanup(func() { testJob.Lock(); testJob.run = nil; testJob.Unlock() })
}

type jobRow struct {
	id, state, lastError string
	triggers, attempts   int
	due                  time.Time
}

func jobRows(t *testing.T, s *Service, org string) []jobRow {
	t.Helper()
	var out []jobRow
	err := s.DB.InOrg(t.Context(), org, func(tx pgx.Tx) error {
		rows, err := tx.Query(t.Context(), `SELECT id::text, state, last_error, triggers, attempts, due_at FROM agent_jobs ORDER BY created_at`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (jobRow, error) {
			var j jobRow
			return j, r.Scan(&j.id, &j.state, &j.lastError, &j.triggers, &j.attempts, &j.due)
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// makeDue moves every pending job's due time into the past, as if its delay had passed.
func makeDue(t *testing.T, s *Service, org string) {
	t.Helper()
	err := s.DB.InOrg(t.Context(), org, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(), `UPDATE agent_jobs SET due_at = now() - interval '1 second' WHERE state = 'pending'`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestJobDebounce(t *testing.T) {
	srv := newModelServer(t, `{}`)
	s, p := bgEnv(t, srv.Server)
	ctx := t.Context()
	if err := s.enqueueJob(ctx, p.Org.ID, "nope", "", "", 0); err == nil {
		t.Fatal("unknown kind queued")
	}
	// Two triggers for one task: one job, due at the later trigger's time.
	if err := s.enqueueJob(ctx, p.Org.ID, "test", "T-1", "", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := s.enqueueJob(ctx, p.Org.ID, "test", "T-1", p.Member.ID, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	// Another task, and one with no task, are their own jobs.
	if err := s.enqueueJob(ctx, p.Org.ID, "test", "T-2", "", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := s.enqueueJob(ctx, p.Org.ID, "test", "", "", time.Hour); err != nil {
		t.Fatal(err)
	}
	rows := jobRows(t, s, p.Org.ID)
	if len(rows) != 3 || rows[0].triggers != 2 || time.Until(rows[0].due) > 3*time.Minute {
		t.Fatalf("jobs: %+v", rows)
	}
	// Nothing is due yet.
	if refs := s.dueJobs(ctx); len(refs) != 0 {
		t.Fatalf("due early: %v", refs)
	}
}

func TestJobRun(t *testing.T) {
	srv := newModelServer(t, `{}`)
	s, p := bgEnv(t, srv.Server)
	ctx := t.Context()
	var ran atomic.Int32
	setTestJob(t, func(ctx context.Context, s *Service, j *agentJob) (map[string]any, error) {
		ran.Add(1)
		if j.Task != "T-1" || j.Member != p.Member.ID || j.Attempt != 1 {
			t.Errorf("job: %+v", j)
		}
		return map[string]any{"ok": true}, nil
	})
	if err := s.enqueueJob(ctx, p.Org.ID, "test", "T-1", p.Member.ID, 0); err != nil {
		t.Fatal(err)
	}
	refs := s.dueJobs(ctx)
	if len(refs) != 1 {
		t.Fatalf("due: %v", refs)
	}
	// Two workers race for it: one runs it.
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { s.runJob(ctx, refs[0]) })
	}
	wg.Wait()
	if ran.Load() != 1 {
		t.Fatalf("ran %d times", ran.Load())
	}
	if rows := jobRows(t, s, p.Org.ID); rows[0].state != "done" {
		t.Fatalf("after run: %+v", rows)
	}
	// A new trigger after it finished is a new job.
	if err := s.enqueueJob(ctx, p.Org.ID, "test", "T-1", p.Member.ID, 0); err != nil {
		t.Fatal(err)
	}
	if rows := jobRows(t, s, p.Org.ID); len(rows) != 2 || rows[1].state != "pending" {
		t.Fatalf("second trigger: %+v", rows)
	}
}

func TestJobRetriggeredWhileRunning(t *testing.T) {
	srv := newModelServer(t, `{}`)
	s, p := bgEnv(t, srv.Server)
	ctx := t.Context()
	var ran atomic.Int32
	setTestJob(t, func(ctx context.Context, s *Service, j *agentJob) (map[string]any, error) {
		if ran.Add(1) == 1 {
			// Something happens on the task while the job works on it.
			if err := s.enqueueJob(ctx, j.Org, "test", j.Task, "", time.Minute); err != nil {
				t.Error(err)
			}
		}
		return nil, nil
	})
	if err := s.enqueueJob(ctx, p.Org.ID, "test", "T-1", "", 0); err != nil {
		t.Fatal(err)
	}
	s.runJob(ctx, s.dueJobs(ctx)[0])
	rows := jobRows(t, s, p.Org.ID)
	if len(rows) != 1 || rows[0].state != "pending" || rows[0].attempts != 0 || time.Until(rows[0].due) < 30*time.Second {
		t.Fatalf("retriggered job: %+v", rows)
	}
	makeDue(t, s, p.Org.ID)
	s.runJob(ctx, s.dueJobs(ctx)[0])
	if rows := jobRows(t, s, p.Org.ID); rows[0].state != "done" || ran.Load() != 2 {
		t.Fatalf("second run: %+v, ran %d", rows, ran.Load())
	}
}

func TestJobFailures(t *testing.T) {
	srv := newModelServer(t, `{}`)
	s, p := bgEnv(t, srv.Server)
	ctx := t.Context()
	setTestJob(t, func(context.Context, *Service, *agentJob) (map[string]any, error) {
		return nil, errors.New("model endpoint returned 500: boom")
	})
	if err := s.enqueueJob(ctx, p.Org.ID, "test", "T-1", "", 0); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= jobMaxAttempts; attempt++ {
		refs := s.dueJobs(ctx)
		if len(refs) != 1 {
			t.Fatalf("attempt %d: due %v", attempt, refs)
		}
		s.runJob(ctx, refs[0])
		row := jobRows(t, s, p.Org.ID)[0]
		if row.lastError == "" || row.attempts != attempt {
			t.Fatalf("attempt %d: %+v", attempt, row)
		}
		if attempt < jobMaxAttempts {
			// Backs off before the next try.
			if row.state != "pending" || time.Until(row.due) < 30*time.Second {
				t.Fatalf("attempt %d: no backoff: %+v", attempt, row)
			}
			makeDue(t, s, p.Org.ID)
		} else if row.state != "failed" {
			t.Fatalf("gave up as %q", row.state)
		}
	}

	// A job with no model to use is skipped, not failed.
	setTestJob(t, func(ctx context.Context, s *Service, j *agentJob) (map[string]any, error) {
		_, _, err := s.backgroundModel(ctx, j.Org)
		return nil, err
	})
	if _, err := s.DisableTriage(ctx, p, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if err := s.enqueueJob(ctx, p.Org.ID, "test", "T-2", "", 0); err != nil {
		t.Fatal(err)
	}
	s.runJob(ctx, s.dueJobs(ctx)[0])
	if rows := jobRows(t, s, p.Org.ID); rows[1].state != "skipped" {
		t.Fatalf("no model: %+v", rows[1])
	}
}

// A job queued to run now wakes the worker loop instead of waiting for the minute sweep.
func TestJobWake(t *testing.T) {
	srv := newModelServer(t, `{}`)
	s, p := bgEnv(t, srv.Server)
	done := make(chan string, 1)
	setTestJob(t, func(ctx context.Context, s *Service, j *agentJob) (map[string]any, error) {
		done <- j.Task
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.RunTriage(ctx)
	time.Sleep(100 * time.Millisecond) // past the startup sweep
	err := s.run(t.Context(), p, func(c *call) error { return c.enqueueJob("test", "T-9", "", 0) })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case task := <-done:
		if task != "T-9" {
			t.Fatalf("ran %s", task)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("job not woken")
	}
}
