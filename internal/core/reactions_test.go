package core_test

import (
	"slices"
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
