package core_test

import (
	"testing"

	"github.com/joalavedra/handoff/internal/core"
)

func TestProjectEmoji(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	p, err := e.s.CreateProject(e.ctx, acme, core.CreateProjectIn{Name: "Sales", Emoji: " 💼 "})
	if err != nil || p.Emoji != "💼" {
		t.Fatalf("create: %+v %v", p, err)
	}
	if _, err := e.s.CreateProject(e.ctx, acme, core.CreateProjectIn{Name: "Bad", Emoji: "hi"}); err == nil {
		t.Fatal("letters accepted as emoji")
	}
	flag := "🇦🇩"
	p, err = e.s.UpdateProject(e.ctx, acme, core.UpdateProjectIn{Project: "Sales", Emoji: &flag})
	if err != nil || p.Emoji != flag || p.Name != "Sales" {
		t.Fatalf("update: %+v %v", p, err)
	}
	empty := ""
	p, err = e.s.UpdateProject(e.ctx, acme, core.UpdateProjectIn{Project: p.ID, Emoji: &empty})
	if err != nil || p.Emoji != "" {
		t.Fatalf("clear: %+v %v", p, err)
	}
	inbox := core.DefaultProject
	if _, err := e.s.UpdateProject(e.ctx, acme, core.UpdateProjectIn{Project: "Sales", Name: &inbox}); err == nil {
		t.Fatal("rename onto an existing name accepted")
	}
	other := "Misc"
	if _, err := e.s.UpdateProject(e.ctx, acme, core.UpdateProjectIn{Project: core.DefaultProject, Name: &other}); err == nil {
		t.Fatal("default project renamed")
	}
	desc := "Deals"
	p, err = e.s.UpdateProject(e.ctx, acme, core.UpdateProjectIn{Project: "Sales", Description: &desc})
	if err != nil || p.Description != "Deals" || p.Name != "Sales" {
		t.Fatalf("description: %+v %v", p, err)
	}
	for _, ok := range []string{"💼", "⚖️", "❤", "🇦🇩", "1️⃣", "👩‍💻", "👍🏽", "🏴󠁧󠁢󠁳󠁣󠁴󠁿"} {
		if _, err := e.s.UpdateProject(e.ctx, acme, core.UpdateProjectIn{Project: "Sales", Emoji: &ok}); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"123", "!", "1", "🔥🔥", "🇦", "a💼", "💼 💼"} {
		if _, err := e.s.UpdateProject(e.ctx, acme, core.UpdateProjectIn{Project: "Sales", Emoji: &bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, err := e.s.UpdateProject(e.ctx, acme, core.UpdateProjectIn{}); err == nil {
		t.Fatal("missing project accepted")
	}
}
