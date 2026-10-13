package core_test

import (
	"testing"
	"time"

	"github.com/joalavedra/handoff/internal/core"
)

func unseenIn(t *testing.T, e *env, p *core.Principal, id string) bool {
	t.Helper()
	out, err := e.s.SearchTasks(e.ctx, p, core.SearchTasksIn{})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range out.Tasks {
		if x.ID == id {
			return x.Unseen
		}
	}
	t.Fatalf("task %s not found", id)
	return false
}

// changesFor collects at least want changes, following cursors: a transaction still open
// elsewhere on the server (other test packages share it) holds the snapshot xmin back for a moment.
func changesFor(t *testing.T, e *env, p *core.Principal, in core.WhatChangedIn, want int) core.WhatChangedOut {
	t.Helper()
	all := core.WhatChangedOut{Changes: []core.EventOut{}}
	for i := 0; ; i++ {
		w, err := e.s.WhatChanged(e.ctx, p, in)
		if err != nil {
			t.Fatal(err)
		}
		all.Changes = append(all.Changes, w.Changes...)
		all.Cursor, all.More = w.Cursor, w.More
		if len(all.Changes) >= want || i == 60 {
			return all
		}
		in.Since = w.Cursor
		time.Sleep(50 * time.Millisecond)
	}
}

func TestMultiplayer(t *testing.T) {
	e := setup(t)
	joan := e.org(t, "acme")
	bot := e.agent(t, joan, "Legal bot", nil)
	ts := e.create(t, joan, core.NewTask{Title: "Review contract"}, core.NewTask{Title: "Countersign"})
	id, id2 := ts[0].ID, ts[1].ID
	start, err := e.s.WhatChanged(e.ctx, joan, core.WhatChangedIn{})
	if err != nil || len(start.Changes) != 0 {
		t.Fatalf("own creates in feed: %+v %v", start, err)
	}
	if unseenIn(t, e, joan, id) {
		t.Fatal("own change marked unseen")
	}
	if _, err := e.s.Comment(e.ctx, bot, core.CommentIn{ID: id, Body: "On it"}); err != nil {
		t.Fatal(err)
	}
	if !unseenIn(t, e, joan, id) || unseenIn(t, e, bot, id) {
		t.Fatal("agent's comment should be unseen for joan only")
	}
	d, err := e.s.GetTask(e.ctx, joan, core.TaskIDIn{ID: id})
	if err != nil || len(d.Here) != 1 || d.Here[0].Member.Name != "Legal bot" {
		t.Fatalf("agent presence: %+v %v", d.Here, err)
	}
	if unseenIn(t, e, joan, id) {
		t.Fatal("still unseen after opening")
	}

	if _, err := e.s.Comment(e.ctx, bot, core.CommentIn{ID: id, Body: "Clause 4 changed"}); err != nil {
		t.Fatal(err)
	}
	high := 1
	if _, err := e.s.UpdateTask(e.ctx, joan, core.UpdateTaskIn{ID: id, Priority: &high}); err != nil {
		t.Fatal(err)
	}
	if !unseenIn(t, e, joan, id) {
		t.Fatal("own edit hid someone else's unread comment")
	}

	if _, err := e.s.ClaimTask(e.ctx, bot, core.ClaimTaskIn{ID: id2}); err != nil {
		t.Fatal(err)
	}
	if d, _ = e.s.GetTask(e.ctx, joan, core.TaskIDIn{ID: id2}); len(d.Here) != 1 {
		t.Fatalf("claim presence: %+v", d.Here)
	}

	w := changesFor(t, e, joan, core.WhatChangedIn{}, 3)
	if len(w.Changes) != 3 {
		t.Fatalf("what_changed: %+v", w)
	}
	for _, c := range w.Changes {
		if c.Actor.Name != "Legal bot" {
			t.Fatalf("own change in feed: %+v", c)
		}
	}
	if w2, _ := e.s.WhatChanged(e.ctx, joan, core.WhatChangedIn{}); len(w2.Changes) != 0 {
		t.Fatalf("second call: %+v", w2)
	}
	if again := changesFor(t, e, joan, core.WhatChangedIn{Since: start.Cursor}, 3); len(again.Changes) != 3 {
		t.Fatalf("since: %+v", again)
	}
	page := changesFor(t, e, joan, core.WhatChangedIn{Since: start.Cursor, Limit: 1}, 1)
	if len(page.Changes) != 1 || !page.More {
		t.Fatalf("page: %+v", page)
	}
	rest := changesFor(t, e, joan, core.WhatChangedIn{Since: page.Cursor, Limit: 10}, 2)
	if len(rest.Changes) != 2 || rest.More || rest.Changes[0].ID == page.Changes[0].ID {
		t.Fatalf("rest: %+v", rest)
	}
	if _, err := e.s.WhatChanged(e.ctx, joan, core.WhatChangedIn{Since: "bogus"}); err == nil {
		t.Fatal("bad cursor accepted")
	}

	if _, err := e.s.WatchTask(e.ctx, joan, core.WatchTaskIn{ID: id, Typing: true}); err != nil {
		t.Fatal(err)
	}
	b, _ := e.s.GetTask(e.ctx, bot, core.TaskIDIn{ID: id})
	if len(b.Here) != 1 || !b.Here[0].Typing {
		t.Fatalf("typing: %+v", b.Here)
	}
	if _, err := e.s.WatchTask(e.ctx, joan, core.WatchTaskIn{ID: id, Leave: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ = e.s.GetTask(e.ctx, bot, core.TaskIDIn{ID: id}); len(b.Here) != 0 {
		t.Fatalf("left: %+v", b.Here)
	}

	globex := e.org(t, "globex")
	if _, err := e.s.WatchTask(e.ctx, globex, core.WatchTaskIn{ID: id}); err == nil {
		t.Fatal("other company can watch the task")
	}
	if g, _ := e.s.WhatChanged(e.ctx, globex, core.WhatChangedIn{}); len(g.Changes) != 0 {
		t.Fatalf("other company sees changes: %+v", g)
	}
}
