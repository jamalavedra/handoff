package core_test

import (
	"strings"
	"testing"

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
