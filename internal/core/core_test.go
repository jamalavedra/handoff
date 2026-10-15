package core_test

import (
	"context"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/core"
	"github.com/joalavedra/handoff/internal/testutil"
)

type env struct {
	s   *core.Service
	ctx context.Context
	rt  *swapRT
}

func setup(t *testing.T) *env {
	t.Helper()
	d := testutil.DB(t)
	base, _ := url.Parse("http://handoff.test")
	return &env{s: core.New(d, &config.Config{BaseURL: base}), ctx: context.Background()}
}

func (e *env) org(t *testing.T, slug string) *core.Principal {
	t.Helper()
	org, admin, err := e.s.CreateOrg(e.ctx, core.NewOrg{Slug: slug, Name: slug, AdminEmail: "admin@" + slug + ".test", AdminName: "Admin " + slug})
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.s.Principal(e.ctx, org, admin.ID, "web")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *env) agent(t *testing.T, owner *core.Principal, name string, perms *core.Permissions) *core.Principal {
	t.Helper()
	m, err := e.s.CreateAgent(e.ctx, owner, core.CreateAgentIn{Name: name, Permissions: perms})
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.s.Principal(e.ctx, owner.Org, m.ID, "mcp")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *env) create(t *testing.T, p *core.Principal, tasks ...core.NewTask) []core.TaskOut {
	t.Helper()
	out, err := e.s.CreateTasks(e.ctx, p, core.CreateTasksIn{Tasks: tasks})
	if err != nil {
		t.Fatal(err)
	}
	return out.Tasks
}

func TestOrgIsolation(t *testing.T) {
	e := setup(t)
	acme, globex := e.org(t, "acme"), e.org(t, "globex")
	at := e.create(t, acme, core.NewTask{Title: "Acme secret", ExternalID: "x1"})[0]

	if _, err := e.s.GetTask(e.ctx, globex, core.TaskIDIn{ID: at.ID}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("globex read acme task: %v", err)
	}
	res, err := e.s.SearchTasks(e.ctx, globex, core.SearchTasksIn{Query: "secret"})
	if err != nil || len(res.Tasks) != 0 {
		t.Fatalf("globex search leaked: %v %v", res.Tasks, err)
	}
	if _, err := e.s.UpdateTask(e.ctx, globex, core.UpdateTaskIn{ID: at.ID, Title: ptr("pwned")}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("globex updated acme task: %v", err)
	}
	if _, err := e.s.ClaimTask(e.ctx, globex, core.ClaimTaskIn{ID: at.ID}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("globex claimed acme task: %v", err)
	}
	// Same external_id in another org is a different task, not a dedupe hit.
	gt := e.create(t, globex, core.NewTask{Title: "Globex task", ExternalID: "x1"})[0]
	if gt.ID == at.ID {
		t.Fatal("external_id deduped across orgs")
	}
	// Acme's members can't be resolved from globex.
	if _, err := e.s.CreateTasks(e.ctx, globex, core.CreateTasksIn{Tasks: []core.NewTask{{Title: "t", Assignee: "admin@acme.test"}}}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("globex assigned to acme member: %v", err)
	}
	// Raw database access without app.org_id sees nothing.
	var n int
	if err := e.s.DB.Pool.QueryRow(e.ctx, `SELECT count(*) FROM tasks`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("unscoped query saw %d tasks (%v)", n, err)
	}
	// And with app.org_id set to globex, a write into acme fails the policy.
	tx, _ := e.s.DB.Pool.Begin(e.ctx)
	defer tx.Rollback(e.ctx)
	_, _ = tx.Exec(e.ctx, `SELECT set_config('app.org_id', $1, true)`, globex.Org.ID)
	if _, err := tx.Exec(e.ctx, `INSERT INTO tasks (org_id, id, project_id, title, created_by) VALUES ($1, 'zz-1', gen_random_uuid(), 'x', gen_random_uuid())`, acme.Org.ID); err == nil {
		t.Fatal("RLS allowed a cross-org insert")
	}
}

func TestReadyClaimClose(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	legal := e.agent(t, acme, "Legal bot", nil)
	sales := e.agent(t, acme, "Sales bot", nil)
	ts := e.create(t, sales,
		core.NewTask{Title: "Send contract", ExternalID: "send"},
		core.NewTask{Title: "Review contract", ExternalID: "review", BlockedBy: []string{"send"}},
		core.NewTask{Title: "Countersign", ExternalID: "sign", BlockedBy: []string{"review"}},
	)
	send, review, sign := ts[0], ts[1], ts[2]
	if !send.Ready || review.Ready || sign.Ready {
		t.Fatalf("ready flags wrong: %v %v %v", send.Ready, review.Ready, sign.Ready)
	}
	if send.CreatedBy.Name != "Sales bot" || send.OnBehalfOf.Name != "Admin acme" {
		t.Fatalf("attribution wrong: %+v %+v", send.CreatedBy, send.OnBehalfOf)
	}
	// Retrying the batch returns the same tasks.
	again, _ := e.s.CreateTasks(e.ctx, sales, core.CreateTasksIn{Tasks: []core.NewTask{{Title: "Send contract", ExternalID: "send"}}})
	if again.Existed != 1 || again.Tasks[0].ID != send.ID {
		t.Fatalf("retry duplicated: %+v", again)
	}
	if _, err := e.s.ClaimTask(e.ctx, legal, core.ClaimTaskIn{ID: review.ID}); !core.IsCode(err, core.ErrConflict) {
		t.Fatalf("claimed a blocked task: %v", err)
	}
	// Two agents race for one task: exactly one wins.
	var wg sync.WaitGroup
	wins := make(chan string, 2)
	for _, a := range []*core.Principal{legal, sales} {
		wg.Add(1)
		go func(a *core.Principal) {
			defer wg.Done()
			if _, err := e.s.ClaimTask(e.ctx, a, core.ClaimTaskIn{ID: send.ID}); err == nil {
				wins <- a.Member.Name
			}
		}(a)
	}
	wg.Wait()
	close(wins)
	var winners []string
	for w := range wins {
		winners = append(winners, w)
	}
	if len(winners) != 1 {
		t.Fatalf("winners: %v", winners)
	}
	winner := sales
	if winners[0] == "Legal bot" {
		winner = legal
	}
	ready, _ := e.s.ListReady(e.ctx, legal, core.ListReadyIn{})
	if len(ready.Tasks) != 0 {
		t.Fatalf("claimed task still ready: %v", ready.Tasks)
	}
	closed, err := e.s.CloseTask(e.ctx, winner, core.CloseTaskIn{ID: send.ID, Reason: "sent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(closed.Unblocked) != 1 || closed.Unblocked[0].ID != review.ID {
		t.Fatalf("unblocked: %+v", closed.Unblocked)
	}
	if _, err := e.s.Link(e.ctx, legal, core.LinkIn{Blocker: sign.ID, Blocked: review.ID}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("cycle allowed: %v", err)
	}
	d, _ := e.s.GetTask(e.ctx, acme, core.TaskIDIn{ID: send.ID})
	kinds := []string{}
	for _, ev := range d.Events {
		kinds = append(kinds, ev.Kind)
	}
	if len(kinds) != 3 || kinds[0] != "created" || kinds[1] != "claimed" || kinds[2] != "closed" {
		t.Fatalf("events: %v", kinds)
	}
	err = e.s.DB.InOrg(e.ctx, acme.Org.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(e.ctx, `UPDATE events SET kind = 'x'`)
		return err
	})
	if err == nil {
		t.Fatal("events are mutable")
	}
}

func TestPermissionsAndApprovals(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	secret, err := e.s.CreateProject(e.ctx, acme, core.CreateProjectIn{Name: "Board", Visibility: "private"})
	if err != nil {
		t.Fatal(err)
	}
	inbox, _ := e.s.ListProjects(e.ctx, acme, struct{}{})
	var inboxID string
	for _, p := range inbox.Projects {
		if p.Name == core.DefaultProject {
			inboxID = p.ID
		}
	}
	reader := e.agent(t, acme, "Reader", &core.Permissions{Projects: []string{inboxID}, Actions: []string{core.ActRead}})
	bt := e.create(t, acme, core.NewTask{Title: "Board only", Project: "Board"})[0]
	it := e.create(t, acme, core.NewTask{Title: "Inbox task"})[0]

	if _, err := e.s.GetTask(e.ctx, reader, core.TaskIDIn{ID: bt.ID}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("scoped agent saw other project: %v", err)
	}
	if _, err := e.s.UpdateTask(e.ctx, reader, core.UpdateTaskIn{ID: it.ID, Title: ptr("x")}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("read-only agent updated: %v", err)
	}
	if _, err := e.s.CreateTasks(e.ctx, reader, core.CreateTasksIn{Tasks: []core.NewTask{{Title: "x"}}}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("read-only agent created: %v", err)
	}

	// A private project's tasks are hidden from people who aren't members, and from their agents.
	org := acme.Org
	bob, err := e.s.Invite(e.ctx, acme, core.InviteIn{Email: "bob@acme.test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.MemberForLogin(e.ctx, org, "bob@acme.test", "Bob"); err != nil {
		t.Fatal(err)
	}
	bp, _ := e.s.Principal(e.ctx, org, bob.ID, "web")
	if _, err := e.s.GetTask(e.ctx, bp, core.TaskIDIn{ID: bt.ID}); !core.IsCode(err, core.ErrNotFound) {
		t.Fatalf("non-member saw private task: %v", err)
	}
	if _, err := e.s.MemberForLogin(e.ctx, org, "eve@evil.test", ""); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("stranger signed in: %v", err)
	}
	_ = secret

	// Default agents need approval to delete.
	bot := e.agent(t, acme, "Bot", nil)
	tr, err := e.s.Trash(e.ctx, bot, core.TrashIn{ID: it.ID, Reason: "dup"})
	if err != nil || tr.Status != "pending_approval" {
		t.Fatalf("trash: %+v %v", tr, err)
	}
	if _, err := e.s.GetTask(e.ctx, bot, core.TaskIDIn{ID: it.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.DecideApproval(e.ctx, bp, core.DecideIn{ID: tr.ApprovalID, Approve: true}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("non-owner approved: %v", err)
	}
	aps, _ := e.s.ListApprovals(e.ctx, acme, struct{}{})
	if len(aps.Approvals) != 1 {
		t.Fatalf("approvals: %+v", aps)
	}
	if _, err := e.s.DecideApproval(e.ctx, acme, core.DecideIn{ID: tr.ApprovalID, Approve: true}); err != nil {
		t.Fatal(err)
	}
	trashed, _ := e.s.SearchTasks(e.ctx, acme, core.SearchTasksIn{Trashed: true})
	if len(trashed.Tasks) != 1 || trashed.Tasks[0].ID != it.ID {
		t.Fatalf("trash: %+v", trashed.Tasks)
	}
	if _, err := e.s.Restore(e.ctx, acme, core.TaskIDIn{ID: it.ID}); err != nil {
		t.Fatal(err)
	}

	// Body replace needs confirm.
	if _, err := e.s.UpdateTask(e.ctx, bot, core.UpdateTaskIn{ID: it.ID, Body: ptr("new"), BodyMode: "replace"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("replace without confirm: %v", err)
	}

	// Revoking an agent stops it immediately.
	if _, err := e.s.UpdateAgent(e.ctx, acme, core.UpdateAgentIn{ID: bot.Member.ID, Revoke: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Principal(e.ctx, org, bot.Member.ID, "mcp"); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("revoked agent authenticated: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestMain(m *testing.M) { os.Exit(m.Run()) }
