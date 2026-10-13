package core_test

import (
	"testing"

	"github.com/joalavedra/handoff/internal/core"
)

func unread(t *testing.T, e *env, p *core.Principal) core.InboxOut {
	t.Helper()
	in, err := e.s.Inbox(e.ctx, p, core.InboxIn{})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func TestNotifications(t *testing.T) {
	e := setup(t)
	joan := e.org(t, "acme")
	bot := e.agent(t, joan, "Legal bot", nil)
	legal := e.agent(t, joan, "Legal", nil)
	id := e.create(t, joan, core.NewTask{Title: "Review contract"})[0].ID

	if _, err := e.s.Comment(e.ctx, joan, core.CommentIn{ID: id, Body: "@legal BOT please check, cc ops@example.com and @nobody"}); err != nil {
		t.Fatal(err)
	}
	in := unread(t, e, bot)
	if in.Unread != 1 || len(in.Notifications) != 1 || in.Notifications[0].Kind != "mention" || in.Notifications[0].Actor.ID != joan.Member.ID || in.Notifications[0].TaskTitle != "Review contract" {
		t.Fatalf("mention: %+v", in)
	}
	if n := unread(t, e, legal).Unread; n != 0 {
		t.Fatalf("shorter name matched: %d", n)
	}
	if _, err := e.s.Comment(e.ctx, bot, core.CommentIn{ID: id, Body: "@Legal bot note to self. Done, @" + joan.Member.Name + "."}); err != nil {
		t.Fatal(err)
	}
	// Commenting returns get_task, so the bot has now seen its own mention.
	if b, j := unread(t, e, bot), unread(t, e, joan); b.Unread != 0 || j.Unread != 1 {
		t.Fatalf("self-mention notified, or owner mention missed: bot %+v joan %+v (name %q)", b, j, joan.Member.Name)
	}

	botName := "Legal bot"
	id2 := e.create(t, joan, core.NewTask{Title: "Countersign", Assignee: botName})[0].ID
	if _, err := e.s.UpdateTask(e.ctx, joan, core.UpdateTaskIn{ID: id, Assignee: &botName}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.UpdateTask(e.ctx, joan, core.UpdateTaskIn{ID: id, Assignee: &botName}); err != nil {
		t.Fatal(err)
	}
	if n := unread(t, e, bot).Unread; n != 2 {
		t.Fatalf("assignments: %d", n)
	}
	me := "me"
	if _, err := e.s.UpdateTask(e.ctx, joan, core.UpdateTaskIn{ID: id2, Assignee: &me}); err != nil {
		t.Fatal(err)
	}
	if n := unread(t, e, joan).Unread; n != 1 {
		t.Fatalf("self-assignment notified: %d", n)
	}

	if _, err := e.s.GetTask(e.ctx, bot, core.TaskIDIn{ID: id}); err != nil {
		t.Fatal(err)
	}
	if n := unread(t, e, bot).Unread; n != 1 {
		t.Fatalf("get_task should clear that task's notifications: %d", n)
	}
	if out, err := e.s.MarkRead(e.ctx, bot, core.MarkReadIn{All: true}); err != nil || out.Unread != 0 {
		t.Fatalf("mark all: %+v %v", out, err)
	}
	if _, err := e.s.MarkRead(e.ctx, bot, core.MarkReadIn{}); err == nil {
		t.Fatal("mark_read without ids accepted")
	}

	if out, err := e.s.Trash(e.ctx, bot, core.TrashIn{ID: id2, Reason: "duplicate"}); err != nil || out.Status != "pending_approval" {
		t.Fatalf("trash: %+v %v", out, err)
	}
	j := unread(t, e, joan)
	if j.Unread != 2 || j.Notifications[0].Kind != "approval" || j.Notifications[0].Actor.Name != "Legal bot" {
		t.Fatalf("approval: %+v", j)
	}
	if ctx, err := e.s.GetContext(e.ctx, joan, struct{}{}); err != nil || ctx.InboxUnread != 2 {
		t.Fatalf("context: %+v %v", ctx.InboxUnread, err)
	}
	if out, err := e.s.MarkRead(e.ctx, joan, core.MarkReadIn{Task: id2}); err != nil || out.Unread != 2 {
		t.Fatalf("task mark_read cleared an approval: %+v %v", out, err)
	}
	if out, err := e.s.MarkRead(e.ctx, joan, core.MarkReadIn{IDs: []int64{j.Notifications[0].ID}}); err != nil || out.Unread != 1 {
		t.Fatalf("mark one: %+v %v", out, err)
	}

	globex := e.org(t, "globex")
	if n := unread(t, e, globex); n.Unread != 0 || len(n.Notifications) != 0 {
		t.Fatalf("other company: %+v", n)
	}
}
