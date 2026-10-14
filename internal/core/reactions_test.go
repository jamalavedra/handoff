package core_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/joalavedra/handoff/internal/core"
)

func TestReactions(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "react")
	bot := e.agent(t, admin, "Acme Bot", nil)
	task := e.create(t, admin, core.NewTask{Title: "Ship it"})[0]
	d, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot can you draft the notes?"})
	if err != nil {
		t.Fatal(err)
	}
	cid := d.Comments[0].ID
	react := func(p *core.Principal, emoji string) bool {
		t.Helper()
		out, err := e.s.React(e.ctx, p, core.ReactIn{Comment: cid, Emoji: emoji})
		if err != nil {
			t.Fatal(err)
		}
		return out.Added
	}
	if !react(bot, "✅") || !react(admin, "✅") || !react(admin, "👀") {
		t.Fatal("reactions not added")
	}
	d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	got := d.Comments[0].Reactions
	if len(got) != 2 || got[0].Emoji != "✅" || got[0].Count != 2 || !got[0].Mine || !slices.Equal(got[0].By, []string{"Acme Bot", "Admin react"}) || got[1].Emoji != "👀" {
		t.Fatalf("reactions: %+v", got)
	}
	// Reacting again takes it back.
	if react(admin, "👀") {
		t.Fatal("second 👀 should remove it")
	}
	d, _ = e.s.GetTask(e.ctx, bot, core.TaskIDIn{ID: task.ID})
	if r := d.Comments[0].Reactions; len(r) != 1 || !r[0].Mine {
		t.Fatalf("after toggle, as the bot: %+v", r)
	}
	if _, err := e.s.React(e.ctx, admin, core.ReactIn{Comment: cid, Emoji: "two words"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("bad emoji: %v", err)
	}
	if _, err := e.s.React(e.ctx, admin, core.ReactIn{Comment: "nope", Emoji: "👍"}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("unknown comment: %v", err)
	}
	reader := e.agent(t, admin, "Reader", &core.Permissions{Projects: []string{"*"}, Actions: []string{core.ActRead}})
	if _, err := e.s.React(e.ctx, reader, core.ReactIn{Comment: cid, Emoji: "👍"}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("read-only agent reacted: %v", err)
	}
}

func TestReactionTogglesAtOnce(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "reactrace")
	task := e.create(t, admin, core.NewTask{Title: "x"})[0]
	d, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.s.React(e.ctx, admin, core.ReactIn{Comment: d.Comments[0].ID, Emoji: "👍"}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent toggle failed: %v", err)
	}
	// Eight toggles cancel out.
	d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	if len(d.Comments[0].Reactions) != 0 {
		t.Fatalf("after 8 toggles: %+v", d.Comments[0].Reactions)
	}
}

func TestChatReachesAssignedAgent(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "chatagent")
	bot := e.agent(t, admin, "Draft bot", nil)
	task := e.create(t, admin, core.NewTask{Title: "Draft the post", Assignee: "Draft bot"})[0]
	// Counted read or unread: the agent opening the task (as Comment does) marks them read.
	mentions := func() int {
		t.Helper()
		in, err := e.s.Inbox(e.ctx, bot, core.InboxIn{})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, x := range in.Notifications {
			if x.Kind == "mention" {
				n++
			}
		}
		return n
	}
	// A plain message in the task's chat reaches the agent that owns it; mentioning it too doesn't double it.
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "Please revise the conclusion"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Draft bot and shorten the intro"}); err != nil {
		t.Fatal(err)
	}
	if n := mentions(); n != 2 {
		t.Fatalf("agent mentions = %d, want 2", n)
	}
	// Its own messages don't notify itself.
	if _, err := e.s.Comment(e.ctx, bot, core.CommentIn{ID: task.ID, Body: "Done, updated the draft."}); err != nil {
		t.Fatal(err)
	}
	if n := mentions(); n != 2 {
		t.Fatalf("after the agent's own message = %d", n)
	}
}
