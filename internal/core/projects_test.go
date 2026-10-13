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
	if _, err := e.s.UpdateProject(e.ctx, acme, core.UpdateProjectIn{}); err == nil {
		t.Fatal("missing project accepted")
	}
}
