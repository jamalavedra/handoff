package core

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// HereOut is someone else who has a task open.
type HereOut struct {
	Member *MemberRef `json:"member"`
	Typing bool       `json:"typing,omitempty"`
}

const (
	humanHere = 30 * time.Second // the web app renews this every 15s while a task is open
	agentHere = 60 * time.Second // agents don't heartbeat; reading or claiming a task counts
	typingFor = "6 seconds"
)

func (c *call) markSeen(taskID string) error {
	_, err := c.tx.Exec(c.ctx, `INSERT INTO task_seen (org_id, member_id, task_id) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, member_id, task_id) DO UPDATE SET seen_at = now()`, c.p.Org.ID, c.p.Member.ID, taskID)
	return err
}

func (c *call) markHere(taskID string, d time.Duration, typing *bool) error {
	_, err := c.tx.Exec(c.ctx, `INSERT INTO presence (org_id, task_id, member_id, here_until, typing_until)
		VALUES ($1, $2, $3, now() + $4 * interval '1 second', CASE WHEN $5::bool THEN now() + interval '`+typingFor+`' ELSE '-infinity' END)
		ON CONFLICT (org_id, task_id, member_id) DO UPDATE SET here_until = EXCLUDED.here_until,
			typing_until = CASE WHEN $6::bool THEN EXCLUDED.typing_until ELSE presence.typing_until END`,
		c.p.Org.ID, taskID, c.p.Member.ID, d.Seconds(), typing != nil && *typing, typing != nil)
	return err
}

func (c *call) here(taskID string) ([]HereOut, error) {
	rows, err := c.tx.Query(c.ctx, `SELECT member_id::text, typing_until > now() FROM presence
		WHERE task_id = $1 AND here_until > now() AND member_id <> $2 ORDER BY member_id`, taskID, c.p.Member.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HereOut{}
	for rows.Next() {
		var id string
		var h HereOut
		if err := rows.Scan(&id, &h.Typing); err != nil {
			return nil, err
		}
		if h.Member = c.ref(id); h.Member != nil {
			out = append(out, h)
		}
	}
	return out, rows.Err()
}

type WatchTaskIn struct {
	ID     string `json:"id"`
	Typing bool   `json:"typing,omitempty" jsonschema:"you are writing a comment; shows as typing for a few seconds"`
	Leave  bool   `json:"leave,omitempty" jsonschema:"you closed the task"`
}

type WatchTaskOut struct {
	Here []HereOut `json:"here" jsonschema:"others who have this task open"`
}

// WatchTask tells others you have a task open (and whether you're typing) for the next 30 seconds.
func (s *Service) WatchTask(ctx context.Context, p *Principal, in WatchTaskIn) (WatchTaskOut, error) {
	var out WatchTaskOut
	err := s.run(ctx, p, func(c *call) error {
		r, err := c.getTask(in.ID, false, false)
		if err != nil {
			return err
		}
		if in.Leave {
			_, err = c.tx.Exec(c.ctx, `UPDATE presence SET here_until = now(), typing_until = '-infinity'
				WHERE task_id = $1 AND member_id = $2`, r.ID, p.Member.ID)
		} else {
			err = c.markHere(r.ID, humanHere, &in.Typing)
		}
		if err != nil {
			return err
		}
		out.Here, err = c.here(r.ID)
		return err
	})
	return out, err
}

type WhatChangedIn struct {
	Since string `json:"since,omitempty" jsonschema:"cursor from a previous call; omit to continue from your last call"`
	Limit int    `json:"limit,omitempty" jsonschema:"default 50, max 200"`
}

type WhatChangedOut struct {
	Changes []EventOut `json:"changes" jsonschema:"what other people and agents did, oldest first"`
	Cursor  string     `json:"cursor" jsonschema:"pass as since to page on; the next call without since starts here"`
	More    bool       `json:"more,omitempty"`
}

// feedPos is a position in the change feed: events are ordered by (transaction id, event id).
type feedPos struct{ xid, id int64 }

func (f feedPos) String() string       { return fmt.Sprintf("%d:%d", f.xid, f.id) }
func (f feedPos) after(g feedPos) bool { return f.xid > g.xid || (f.xid == g.xid && f.id > g.id) }

func parseFeedPos(s string) (feedPos, error) {
	a, b, ok := strings.Cut(s, ":")
	x, err1 := strconv.ParseInt(a, 10, 64)
	id, err2 := strconv.ParseInt(b, 10, 64)
	if !ok || err1 != nil || err2 != nil || x < 0 || id < 0 {
		return feedPos{}, errf(ErrInvalid, "since must be a cursor returned by what_changed")
	}
	return feedPos{x, id}, nil
}

// WhatChanged lists changes others made to visible tasks since the caller last asked.
// Event ids are handed out before commit, so the feed is ordered by transaction instead:
// every transaction below the snapshot xmin has finished, so nothing can appear behind the cursor later.
func (s *Service) WhatChanged(ctx context.Context, p *Principal, in WhatChangedIn) (WhatChangedOut, error) {
	out := WhatChangedOut{Changes: []EventOut{}}
	err := s.run(ctx, p, func(c *call) error {
		var stored string
		var baseline time.Time
		var xmin int64
		if err := c.tx.QueryRow(c.ctx, `SELECT feed_after, seen_baseline, pg_snapshot_xmin(pg_current_snapshot())::text::bigint
			FROM members WHERE id = $1`, p.Member.ID).Scan(&stored, &baseline, &xmin); err != nil {
			return err
		}
		since := in.Since
		if since == "" {
			since = stored
		}
		// Changes to visible tasks, and to notes in visible projects (a note needn't have a task).
		where := `e.xid < $2::text::xid8 AND e.actor_id <> $3 AND (CASE WHEN e.data->>'note_id' IS NOT NULL
			THEN e.data->>'project_id' = ANY($1) ELSE e.task_id IS NOT NULL AND t.project_id = ANY($1::uuid[]) END)`
		args := []any{c.visible, strconv.FormatInt(xmin, 10), p.Member.ID}
		next := feedPos{xmin, 0}
		if since != "" {
			pos, err := parseFeedPos(since)
			if err != nil {
				return err
			}
			where += " AND (e.xid, e.id) > ($4::text::xid8, $5)"
			args = append(args, strconv.FormatInt(pos.xid, 10), pos.id)
			if pos.after(next) {
				next = pos
			}
		} else {
			where += " AND e.created_at > $4"
			args = append(args, baseline)
		}
		limit := clampLimit(in.Limit, 50, 200)
		evs, err := c.eventsBy(where, args, "e.xid, e.id", limit+1)
		if err != nil {
			return err
		}
		if len(evs) > limit {
			evs, out.More = evs[:limit], true
			next = feedPos{evs[limit-1].xid, evs[limit-1].ID}
		}
		out.Changes, out.Cursor = evs, next.String()
		_, err = c.tx.Exec(c.ctx, `UPDATE members SET feed_after = $2 WHERE id = $1`, p.Member.ID, out.Cursor)
		return err
	})
	return out, err
}

// VisibleProjects is the set of project ids the caller can see, for filtering live updates.
func (s *Service) VisibleProjects(ctx context.Context, p *Principal) (map[string]bool, error) {
	out := map[string]bool{}
	err := s.run(ctx, p, func(c *call) error {
		for _, id := range c.visible {
			out[id] = true
		}
		return nil
	})
	return out, err
}
