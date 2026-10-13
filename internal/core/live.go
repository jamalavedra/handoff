package core

import (
	"context"
	"fmt"
	"strconv"
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
	Since int64 `json:"since,omitempty" jsonschema:"cursor from a previous call; omit to continue from your last call"`
	Limit int   `json:"limit,omitempty" jsonschema:"default 50, max 200"`
}

type WhatChangedOut struct {
	Changes []EventOut `json:"changes" jsonschema:"what other people and agents did, oldest first"`
	Cursor  int64      `json:"cursor" jsonschema:"pass as since to page on; the next call without since starts here"`
	More    bool       `json:"more,omitempty"`
}

// WhatChanged lists changes others made to visible tasks since the caller last asked.
// The cursor is a transaction id: every transaction below the snapshot xmin has finished,
// so [cursor, xmin) can't gain events later and nothing slips past the cursor.
func (s *Service) WhatChanged(ctx context.Context, p *Principal, in WhatChangedIn) (WhatChangedOut, error) {
	out := WhatChangedOut{Changes: []EventOut{}}
	err := s.run(ctx, p, func(c *call) error {
		var stored, xmin int64
		var baseline time.Time
		if err := c.tx.QueryRow(c.ctx, `SELECT feed_cursor, seen_baseline, pg_snapshot_xmin(pg_current_snapshot())::text::bigint
			FROM members WHERE id = $1`, p.Member.ID).Scan(&stored, &baseline, &xmin); err != nil {
			return err
		}
		cursor := in.Since
		if cursor <= 0 {
			cursor = stored
		}
		where := "e.xid < $2::text::xid8 AND e.actor_id <> $3 AND e.task_id IS NOT NULL AND t.project_id::text = ANY($1)"
		args := []any{c.visible, strconv.FormatInt(xmin, 10), p.Member.ID}
		if cursor > 0 {
			where += " AND e.xid >= $4::text::xid8"
			args = append(args, strconv.FormatInt(cursor, 10))
		} else {
			where += " AND e.created_at > $4"
			args = append(args, baseline)
		}
		limit := clampLimit(in.Limit, 50, 200)
		evs, err := c.eventsBy(where, args, "e.xid, e.id", limit+1)
		if err != nil {
			return err
		}
		out.Cursor = xmin
		if len(evs) > limit {
			// Page on transaction boundaries so the next cursor never splits one.
			next, page := evs[limit].xid, evs[:limit]
			for len(page) > 0 && page[len(page)-1].xid == next {
				page = page[:len(page)-1]
			}
			if len(page) == 0 {
				one := where + fmt.Sprintf(" AND e.xid = $%d::text::xid8", len(args)+1)
				if page, err = c.eventsBy(one, append(args, strconv.FormatInt(next, 10)), "e.id", 100000); err != nil {
					return err
				}
				next++
			}
			evs, out.Cursor, out.More = page, next, true
		}
		out.Changes = evs
		_, err = c.tx.Exec(c.ctx, `UPDATE members SET feed_cursor = greatest(feed_cursor, $2) WHERE id = $1`, p.Member.ID, out.Cursor)
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
