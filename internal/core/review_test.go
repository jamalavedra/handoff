package core_test

import (
	"sync"
	"testing"

	"github.com/joalavedra/handoff/internal/core"
)

func TestExternalIDDoesNotLeakAcrossProjects(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	if _, err := e.s.CreateProject(e.ctx, acme, core.CreateProjectIn{Name: "Board"}); err != nil {
		t.Fatal(err)
	}
	e.create(t, acme, core.NewTask{Title: "Board secret", Body: "salaries", Project: "Board", ExternalID: "ext-1"})
	inbox, _ := e.s.ListProjects(e.ctx, acme, struct{}{})
	var inboxID string
	for _, p := range inbox.Projects {
		if p.Name == core.DefaultProject {
			inboxID = p.ID
		}
	}
	bot := e.agent(t, acme, "Bot", &core.Permissions{Projects: []string{inboxID}, Actions: []string{core.ActRead, core.ActCreate}})
	out, err := e.s.CreateTasks(e.ctx, bot, core.CreateTasksIn{Tasks: []core.NewTask{{Title: "probe", ExternalID: "ext-1"}}})
	if !core.IsCode(err, core.ErrConflict) {
		t.Fatalf("expected conflict, got %+v %v", out, err)
	}
}

func TestConcurrentExternalIDIsIdempotent(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	var wg sync.WaitGroup
	ids := make([]string, 8)
	errs := make([]error, 8)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, err := e.s.CreateTasks(e.ctx, acme, core.CreateTasksIn{Tasks: []core.NewTask{{Title: "same", ExternalID: "dup"}}})
			if err == nil {
				ids[i] = out.Tasks[0].ID
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil || ids[i] != ids[0] {
			t.Fatalf("call %d: id %q vs %q, err %v", i, ids[i], ids[0], errs[i])
		}
	}
}

func TestConcurrentOppositeLinksNeverCycle(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	for round := 0; round < 10; round++ {
		ts := e.create(t, acme, core.NewTask{Title: "a"}, core.NewTask{Title: "b"})
		a, b := ts[0].ID, ts[1].ID
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, pair := range [][2]string{{a, b}, {b, a}} {
			wg.Add(1)
			go func(i int, pair [2]string) {
				defer wg.Done()
				_, errs[i] = e.s.Link(e.ctx, acme, core.LinkIn{Blocker: pair[0], Blocked: pair[1]})
			}(i, pair)
		}
		wg.Wait()
		if errs[0] == nil && errs[1] == nil {
			t.Fatalf("round %d: both opposite links succeeded", round)
		}
	}
}

func TestInProgressUnclaimedIsNotReady(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	tk := e.create(t, acme, core.NewTask{Title: "x"})[0]
	res, err := e.s.UpdateTask(e.ctx, acme, core.UpdateTaskIn{ID: tk.ID, Status: "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Task.Ready {
		t.Fatal("in_progress task without an expired claim reported ready")
	}
}

func TestConcurrentDemotionsKeepAnAdmin(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	bob, err := e.s.Invite(e.ctx, acme, core.InviteIn{Email: "bob@acme.test", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.MemberForLogin(e.ctx, acme.Org, "bob@acme.test", "Bob"); err != nil {
		t.Fatal(err)
	}
	bp, _ := e.s.Principal(e.ctx, acme.Org, bob.ID, "web")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, c := range []struct {
		p  *core.Principal
		id string
	}{{acme, bob.ID}, {bp, acme.Member.ID}} {
		wg.Add(1)
		go func(i int, p *core.Principal, id string) {
			defer wg.Done()
			_, errs[i] = e.s.UpdateMember(e.ctx, p, core.UpdateMemberIn{ID: id, Role: "member"})
		}(i, c.p, c.id)
	}
	wg.Wait()
	if errs[0] == nil && errs[1] == nil {
		t.Fatal("both admins demoted each other")
	}
}
