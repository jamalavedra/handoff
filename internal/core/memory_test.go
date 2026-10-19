package core_test

import (
	"strings"
	"testing"
	"time"

	"github.com/joalavedra/handoff/internal/core"
)

func memberPrincipal(t *testing.T, e *env, admin *core.Principal, email, name string) *core.Principal {
	t.Helper()
	m, err := e.s.Invite(e.ctx, admin, core.InviteIn{Email: email})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.MemberForLogin(e.ctx, admin.Org, email, name); err != nil {
		t.Fatal(err)
	}
	p, err := e.s.Principal(e.ctx, admin.Org, m.ID, "web")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func factTexts(facts []core.MemoryFactOut) []string {
	var out []string
	for _, f := range facts {
		out = append(out, f.Text)
	}
	return out
}

func TestMemoryScopes(t *testing.T) {
	srv, _ := scriptedModel(t, say("hi"))
	e, admin, _ := assistantEnv(t, srv, "mem")
	if _, err := e.s.CreateProject(e.ctx, admin, core.CreateProjectIn{Name: "Board", Visibility: "private"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.CreateProject(e.ctx, admin, core.CreateProjectIn{Name: "Open"}); err != nil {
		t.Fatal(err)
	}
	bp := memberPrincipal(t, e, admin, "bob@mem.test", "Bob")

	// One fact of each scope, plus one on the private project Bob can't see.
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "The workspace ships on Fridays", Scope: "workspace"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "The open project tracks marketing", Scope: "project", Project: "Open"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Board budget is secret", Scope: "project", Project: "Board"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Admin prefers terse replies"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Remember(e.ctx, bp, core.RememberIn{Text: "Bob likes big fonts"}); err != nil {
		t.Fatal(err)
	}

	// Admin sees workspace, both projects and their own person fact — not Bob's.
	mine, err := e.s.ListMemory(e.ctx, admin, core.ListMemoryIn{})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(factTexts(mine.Facts), "\n")
	for _, want := range []string{"ships on Fridays", "tracks marketing", "budget is secret", "terse replies"} {
		if !strings.Contains(got, want) {
			t.Fatalf("admin should see %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "big fonts") {
		t.Fatal("bob's person fact leaked to admin")
	}

	// Bob sees workspace and the visible project, and his own facts — not admin's or Board's.
	bobs, err := e.s.ListMemory(e.ctx, bp, core.ListMemoryIn{})
	if err != nil {
		t.Fatal(err)
	}
	got = strings.Join(factTexts(bobs.Facts), "\n")
	for _, want := range []string{"ships on Fridays", "tracks marketing", "big fonts"} {
		if !strings.Contains(got, want) {
			t.Fatalf("bob should see %q:\n%s", want, got)
		}
	}
	for _, no := range []string{"budget is secret", "terse replies"} {
		if strings.Contains(got, no) {
			t.Fatalf("bob should not see %q", no)
		}
	}

	// A bare fact is a person fact about the caller; a fact about someone else is refused.
	if _, err := e.s.Remember(e.ctx, bp, core.RememberIn{Text: "Admin snores", Member: admin.Member.ID}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("fact about someone else: %v", err)
	}
}

func TestMemoryDedupeSecretsAndManage(t *testing.T) {
	srv, _ := scriptedModel(t, say("hi"))
	e, admin, _ := assistantEnv(t, srv, "memmanage")
	bp := memberPrincipal(t, e, admin, "bob@memmanage.test", "Bob")

	r1, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Standup moved to 9:30", Scope: "workspace"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "  standup   moved to 9:30 ", Scope: "workspace"})
	if err != nil || again.Added || again.Fact.ID != r1.Fact.ID {
		t.Fatalf("the same fact should not be written twice: %+v %v", again, err)
	}
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "the deploy api_key: sk-abcd1234efgh5678", Scope: "workspace"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("a credential-shaped fact must be refused: %v", err)
	}
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: strings.Repeat("x", 501), Scope: "workspace"}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("overlong fact: %v", err)
	}

	// Only admins manage workspace facts.
	if _, err := e.s.PinMemory(e.ctx, bp, core.PinMemoryIn{ID: r1.Fact.ID, Pinned: true}); !core.IsCode(err, core.ErrForbidden) {
		t.Fatalf("bob pinning a workspace fact: %v", err)
	}
	pinned, err := e.s.PinMemory(e.ctx, admin, core.PinMemoryIn{ID: r1.Fact.ID, Pinned: true})
	if err != nil || !pinned.Fact.Pinned {
		t.Fatalf("pin: %+v %v", pinned, err)
	}

	// A person manages their own facts; removal keeps the row but takes it out of every list.
	own, err := e.s.Remember(e.ctx, bp, core.RememberIn{Text: "Bob prefers afternoons"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.PinMemory(e.ctx, bp, core.PinMemoryIn{ID: own.Fact.ID, Pinned: true}); err != nil {
		t.Fatalf("bob pins his own fact: %v", err)
	}
	rm, err := e.s.RemoveMemory(e.ctx, bp, core.MemoryIDIn{ID: own.Fact.ID})
	if err != nil || rm.Fact.RemovedAt == "" {
		t.Fatalf("remove: %+v %v", rm, err)
	}
	if _, err := e.s.RemoveMemory(e.ctx, bp, core.MemoryIDIn{ID: own.Fact.ID}); !core.IsCode(err, core.ErrInvalid) {
		t.Fatalf("removing twice: %v", err)
	}
	live, _ := e.s.ListMemory(e.ctx, bp, core.ListMemoryIn{})
	if strings.Contains(strings.Join(factTexts(live.Facts), "\n"), "prefers afternoons") {
		t.Fatal("a removed fact still lists")
	}
}

// The remember tool saves a person fact about the asker; next turn's prompt shows it.
func TestAssistantRemembers(t *testing.T) {
	srv, reqs := scriptedModel(t,
		calls(toolCall("c1", "remember", map[string]any{"text": "Joan prefers plain-language replies"})),
		say("Noted."),
		say("hi again"),
	)
	e, admin, _ := assistantEnv(t, srv, "memasst")
	if _, err := e.s.AssistantChat(e.ctx, admin, ask("remember that I prefer plain language")); err != nil {
		t.Fatal(err)
	}
	facts, err := e.s.ListMemory(e.ctx, admin, core.ListMemoryIn{Scope: "person"})
	if err != nil || len(facts.Facts) != 1 {
		t.Fatalf("the tool should have saved one person fact: %+v %v", facts, err)
	}
	f := facts.Facts[0]
	if f.Scope != "person" || f.Member == nil || f.Member.ID != admin.Member.ID || f.Noticed {
		t.Fatalf("wrong fact: %+v", f)
	}

	// The next turn loads it, fenced, in the system prompt.
	if _, err := e.s.AssistantChat(e.ctx, admin, ask("hi")); err != nil {
		t.Fatal(err)
	}
	reqsList := reqs()
	sys := reqsList[len(reqsList)-1].Messages[0]["content"].(string)
	if !strings.Contains(sys, "What you remember between jobs") || !strings.Contains(sys, "Joan prefers plain-language replies") {
		t.Fatalf("the fact didn't load:\n%s", sys)
	}
	if !strings.Contains(sys, "(for Admin memasst)") {
		t.Fatalf("person facts name whom they are about:\n%s", sys)
	}
}

// Person facts load only in that person's turns.
func TestMemoryPersonFactsStayPersonal(t *testing.T) {
	srv, reqs := scriptedModel(t, say("one"), say("two"))
	e, admin, _ := assistantEnv(t, srv, "mempriv")
	bp := memberPrincipal(t, e, admin, "bob@mempriv.test", "Bob")
	if _, err := e.s.Remember(e.ctx, bp, core.RememberIn{Text: "Bob's tax id is XX-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Remember(e.ctx, bp, core.RememberIn{Text: "Everyone shares the roadmap", Scope: "workspace"}); err != nil {
		t.Fatal(err)
	}

	if _, err := e.s.AssistantChat(e.ctx, admin, ask("hi")); err != nil {
		t.Fatal(err)
	}
	sys := reqs()[0].Messages[0]["content"].(string)
	if strings.Contains(sys, "tax id") {
		t.Fatalf("bob's person fact loaded for admin:\n%s", sys)
	}
	if !strings.Contains(sys, "Everyone shares the roadmap") {
		t.Fatalf("the workspace fact should load for anyone:\n%s", sys)
	}
	if _, err := e.s.AssistantChat(e.ctx, bp, ask("hi")); err != nil {
		t.Fatal(err)
	}
	sys = reqs()[1].Messages[0]["content"].(string)
	if !strings.Contains(sys, "tax id") {
		t.Fatalf("bob's own fact didn't load for him:\n%s", sys)
	}
}

// A shared turn (a reply on a task) is read by everyone who can see it: workspace facts and the
// task's project facts load, never person facts or another project's.
func TestMemorySharedTurnsDontSeePersonFacts(t *testing.T) {
	model, reqs := scriptedModel(t, say("done."))
	e, admin, bot := assistantEnv(t, model, "memshared")
	if _, err := e.s.CreateProject(e.ctx, admin, core.CreateProjectIn{Name: "Board", Visibility: "private"}); err != nil {
		t.Fatal(err)
	}
	task := e.create(t, admin, core.NewTask{Title: "Plan the trip", Project: "Board"})[0]
	waitTriage(t, e, admin, task.ID)
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Admin's tax id is XX-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Board trips leave Thursdays", Scope: "project", Project: "Board"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Everyone ships Fridays", Scope: "workspace"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Comment(e.ctx, bot, core.CommentIn{ID: task.ID, Body: "first note"}); err != nil {
		t.Fatal(err)
	}
	orig := last(t, mustTask(t, e, admin, task.ID))
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot go", ReplyTo: orig.ID}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		d, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		if c := last(t, d); c.Author.ID == bot.Member.ID && c.ID != orig.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no answer")
		}
		time.Sleep(50 * time.Millisecond)
	}
	rs := reqs()
	if len(rs) == 0 {
		t.Fatal("model not called")
	}
	sys := rs[len(rs)-1].Messages[0]["content"].(string)
	if strings.Contains(sys, "tax id") {
		t.Fatalf("a person fact entered a shared turn:\n%s", sys)
	}
	if !strings.Contains(sys, "Board trips leave Thursdays") || !strings.Contains(sys, "Everyone ships Fridays") {
		t.Fatalf("the task's project and workspace facts should load:\n%s", sys)
	}
}

func mustTask(t *testing.T, e *env, p *core.Principal, id string) core.TaskDetailOut {
	t.Helper()
	d, err := e.s.GetTask(e.ctx, p, core.TaskIDIn{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMemoryUntilAndSharedListing(t *testing.T) {
	srv, _ := scriptedModel(t, say("hi"))
	e, admin, _ := assistantEnv(t, srv, "memuntil")
	if _, err := e.s.CreateProject(e.ctx, admin, core.CreateProjectIn{Name: "Open"}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	future := time.Now().AddDate(0, 1, 0).Format("2006-01-02")

	// An expired fact no longer lists; renewing a fact moves its date.
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Old news", Scope: "workspace", Until: past}); err != nil {
		t.Fatal(err)
	}
	r, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Standup at 9:30", Scope: "workspace", Until: past})
	if err != nil {
		t.Fatal(err)
	}
	live, _ := e.s.ListMemory(e.ctx, admin, core.ListMemoryIn{})
	if len(live.Facts) != 0 {
		t.Fatalf("expired facts should not list: %+v", live.Facts)
	}
	again, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Standup at 9:30", Scope: "workspace", Until: future})
	if err != nil || again.Added {
		t.Fatalf("renewing should reuse the row: %+v %v", again, err)
	}
	live, _ = e.s.ListMemory(e.ctx, admin, core.ListMemoryIn{})
	if len(live.Facts) != 1 || live.Facts[0].Until != future {
		t.Fatalf("the renewed date should stick: %+v", live.Facts)
	}
	_ = r

	// A shared listing: workspace + the named project only — no person facts, no other projects.
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Open ships Tuesdays", Scope: "project", Project: "Open"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.Remember(e.ctx, admin, core.RememberIn{Text: "Admin likes dark mode"}); err != nil {
		t.Fatal(err)
	}
	open, err := e.s.ListMemory(e.ctx, admin, core.ListMemoryIn{Project: "Open"})
	if err != nil || len(open.Facts) != 1 || open.Facts[0].Text != "Open ships Tuesdays" {
		t.Fatalf("project filter: %+v %v", open, err)
	}
	shared, err := e.s.ListMemory(e.ctx, admin, core.ListMemoryIn{Shared: true, Project: open.Facts[0].ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(factTexts(shared.Facts), "\n")
	if !strings.Contains(got, "Open ships Tuesdays") || !strings.Contains(got, "Standup at 9:30") {
		t.Fatalf("shared should list workspace + that project: %s", got)
	}
	if strings.Contains(got, "dark mode") {
		t.Fatal("a person fact leaked into a shared listing")
	}
}
