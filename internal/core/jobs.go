package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// Background jobs are the agent's work nobody waits for (memory upkeep, reviews). A job is a row in
// agent_jobs, so a restart or a crash loses nothing: the minute sweep in RunTriage finds what's due
// and a worker claims it with a lease, as triage does. There is one pending job of a kind per task;
// another trigger while it waits sets when it runs (the latest trigger decides), and one that lands
// while it runs makes it run again afterwards.

const (
	jobLease       = 6 * time.Minute
	jobMaxAttempts = 3
	jobKeep        = 30 * 24 * time.Hour // finished jobs stay this long, for the record
)

// agentJob is one claimed job as its handler sees it.
type agentJob struct {
	Org, ID, Kind  string
	Task, Member   string // empty when the job has none
	Attempt        int
	claim          string
	claimedTrigger int
}

// jobKind runs one kind of job. The result is kept on the row; errBackgroundOff skips the job;
// errBackgroundCapped pauses it until the cap allows; any other error is retried with backoff, up
// to jobMaxAttempts.
type jobKind func(ctx context.Context, s *Service, j *agentJob) (map[string]any, error)

// jobKinds are the registered kinds. A pending job of a kind that isn't registered (from a newer
// server during a rolling deploy) is left for a server that knows it.
var jobKinds = map[string]jobKind{}

type jobRef struct{ org, id string }

// enqueueJob schedules a job of kind in delay, or moves the pending one to then. task and member may be empty.
func (c *call) enqueueJob(kind, task, member string, delay time.Duration) error {
	if err := enqueueJob(c.ctx, c.tx, c.p.Org.ID, kind, task, member, delay); err != nil {
		return err
	}
	if delay <= 0 {
		c.after = append(c.after, c.s.wakeJobs)
	}
	return nil
}

func (s *Service) enqueueJob(ctx context.Context, org, kind, task, member string, delay time.Duration) error {
	err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
		return enqueueJob(ctx, tx, org, kind, task, member, delay)
	})
	if err == nil && delay <= 0 {
		s.wakeJobs()
	}
	return err
}

func enqueueJob(ctx context.Context, tx pgx.Tx, org, kind, task, member string, delay time.Duration) error {
	if _, ok := jobKinds[kind]; !ok {
		return errors.New("unknown job kind " + kind)
	}
	_, err := tx.Exec(ctx, `INSERT INTO agent_jobs (org_id, kind, task_id, member_id, due_at)
		VALUES ($1, $2, nullif($3, ''), nullif($4, '')::uuid, now() + make_interval(secs => $5))
		ON CONFLICT (org_id, kind, coalesce(task_id, '')) WHERE state = 'pending' DO UPDATE SET
			triggers = agent_jobs.triggers + 1, due_at = excluded.due_at,
			member_id = coalesce(excluded.member_id, agent_jobs.member_id), updated_at = now()`,
		org, kind, task, member, max(delay, 0).Seconds())
	return err
}

func (s *Service) wakeJobs() {
	if s.jobWake == nil {
		return
	}
	select {
	case s.jobWake <- struct{}{}:
	default:
	}
}

// dueJobs lists jobs ready to run in every workspace, of the kinds this server knows.
func (s *Service) dueJobs(ctx context.Context) []jobRef {
	kinds := make([]string, 0, len(jobKinds))
	for k := range jobKinds {
		kinds = append(kinds, k)
	}
	if len(kinds) == 0 {
		return nil
	}
	rows, err := s.DB.Pool.Query(ctx, `SELECT id::text FROM orgs`)
	if err != nil {
		slog.Error("jobs sweep", "err", err)
		return nil
	}
	orgs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		slog.Error("jobs sweep", "err", err)
		return nil
	}
	clean := time.Since(s.jobsCleaned) > time.Hour
	if clean {
		s.jobsCleaned = time.Now()
	}
	var refs []jobRef
	for _, org := range orgs {
		err := s.DB.InOrg(ctx, org, func(tx pgx.Tx) error {
			if clean {
				if _, err := tx.Exec(ctx, `DELETE FROM agent_jobs WHERE state <> 'pending' AND updated_at < now() - make_interval(secs => $1)`, jobKeep.Seconds()); err != nil {
					return err
				}
			}
			rows, err := tx.Query(ctx, `SELECT id::text FROM agent_jobs WHERE state = 'pending' AND due_at <= now()
				AND (claim_until IS NULL OR claim_until < now()) AND kind = ANY($1) ORDER BY due_at LIMIT 50`, kinds)
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
			for _, id := range ids {
				refs = append(refs, jobRef{org, id})
			}
			return err
		})
		if err != nil {
			slog.Error("jobs sweep", "org", org, "err", err)
		}
	}
	return refs
}

// claimJob takes a due job for this worker, or returns nil when another worker has it or it isn't due.
func (s *Service) claimJob(ctx context.Context, r jobRef) (*agentJob, error) {
	j := &agentJob{Org: r.org}
	err := s.DB.InOrg(ctx, r.org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `UPDATE agent_jobs SET claim = gen_random_uuid(), claim_until = now() + make_interval(secs => $2),
				attempts = attempts + 1, updated_at = now()
			WHERE id::text = $1 AND state = 'pending' AND due_at <= now() AND (claim_until IS NULL OR claim_until < now())
			RETURNING id::text, kind, coalesce(task_id, ''), coalesce(member_id::text, ''), attempts, claim::text, triggers`,
			r.id, jobLease.Seconds()).Scan(&j.ID, &j.Kind, &j.Task, &j.Member, &j.Attempt, &j.claim, &j.claimedTrigger)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

// jobBackoff is how long a failed job waits before its next attempt.
func jobBackoff(attempt int) time.Duration {
	return time.Minute << (2 * min(max(attempt-1, 0), 4)) // 1m, 4m, 16m…
}

// finishJob records how a run ended, if this worker still holds the claim. A job triggered again while
// it ran goes back to pending with a fresh set of attempts, so that trigger isn't lost. A job stopped by
// the token cap waits, pending, until next month (UTC) or until an admin changes the cap.
func (s *Service) finishJob(ctx context.Context, j *agentJob, result map[string]any, runErr error) {
	if errors.Is(runErr, errBackgroundCapped) {
		s.pauseJob(ctx, j)
		return
	}
	state, msg := "done", ""
	switch {
	case errors.Is(runErr, errBackgroundOff):
		state = "skipped"
	case runErr != nil:
		state, msg = "failed", clip(runErr.Error(), 300)
		slog.Warn("job failed", "kind", j.Kind, "job", j.ID, "attempt", j.Attempt, "err", msg)
	}
	if result == nil {
		result = map[string]any{}
	}
	raw, _ := json.Marshal(result)
	ctx = context.WithoutCancel(ctx)
	err := s.DB.InOrg(ctx, j.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agent_jobs SET claim = NULL, claim_until = NULL, updated_at = now(), last_error = $3, result = $4,
				state = CASE WHEN triggers > $5 THEN 'pending'
					WHEN $6 = 'failed' AND attempts < $7 THEN 'pending' ELSE $6 END,
				attempts = CASE WHEN triggers > $5 THEN 0 ELSE attempts END,
				due_at = CASE WHEN triggers > $5 THEN due_at
					WHEN $6 = 'failed' AND attempts < $7 THEN now() + make_interval(secs => $8) ELSE due_at END
			WHERE id::text = $1 AND claim::text = $2`,
			j.ID, j.claim, msg, raw, j.claimedTrigger, state, jobMaxAttempts, jobBackoff(j.Attempt).Seconds())
		return err
	})
	if err != nil {
		slog.Error("job: record", "kind", j.Kind, "job", j.ID, "err", err)
	}
}

// pauseJob puts a job stopped by the cap back to pending, due when the month turns. The run that hit
// the cap doesn't count as an attempt: it made no progress.
func (s *Service) pauseJob(ctx context.Context, j *agentJob) {
	ctx = context.WithoutCancel(ctx)
	err := s.DB.InOrg(ctx, j.Org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE agent_jobs SET claim = NULL, claim_until = NULL, updated_at = now(), last_error = $3,
				attempts = greatest(attempts - 1, 0),
				due_at = (date_trunc('month', now() AT TIME ZONE 'UTC') + interval '1 month') AT TIME ZONE 'UTC'
			WHERE id::text = $1 AND claim::text = $2`, j.ID, j.claim, capPaused)
		return err
	})
	if err != nil {
		slog.Error("job: pause", "kind", j.Kind, "job", j.ID, "err", err)
	}
}

// runJob claims and runs one job.
func (s *Service) runJob(ctx context.Context, r jobRef) {
	j, err := s.claimJob(ctx, r)
	if err != nil {
		slog.Error("job: claim", "job", r.id, "err", err)
		return
	}
	if j == nil {
		return
	}
	run := jobKinds[j.Kind]
	if run == nil {
		s.finishJob(ctx, j, nil, errors.New("unknown job kind"))
		return
	}
	jctx, cancel := context.WithTimeout(ctx, jobLease-time.Minute)
	defer cancel()
	result, err := run(jctx, s, j)
	s.finishJob(ctx, j, result, err)
}
