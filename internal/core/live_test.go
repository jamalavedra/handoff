package core_test

import (
	"testing"

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

func TestMultiplayer(t *testing.T) {
	e := setup(t)
	joan := e.org(t, "acme")
	bot := e.agent(t, joan, "Legal bot", nil)
	id := e.create(t, joan, core.NewTask{Title: "Review contract"})[0].ID
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

	w, err := e.s.WhatChanged(e.ctx, joan, core.WhatChangedIn{})
	if err != nil || len(w.Changes) == 0 {
		t.Fatalf("what_changed: %+v %v", w, err)
	}
	for _, c := range w.Changes {
		if c.Actor.Name != "Legal bot" {
			t.Fatalf("own change in feed: %+v", c)
		}
	}
	if w2, _ := e.s.WhatChanged(e.ctx, joan, core.WhatChangedIn{}); len(w2.Changes) != 0 || w2.Cursor != w.Cursor {
		t.Fatalf("second call: %+v", w2)
	}
	if again, _ := e.s.WhatChanged(e.ctx, joan, core.WhatChangedIn{Since: w.Changes[0].ID - 1}); len(again.Changes) != len(w.Changes) {
		t.Fatalf("since: %+v", again)
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
