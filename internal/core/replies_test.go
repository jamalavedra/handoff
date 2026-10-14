package core_test

import (
	"strings"
	"testing"
	"time"

	"github.com/joalavedra/handoff/internal/core"
)

// last is the newest comment in a task's chat.
func last(t *testing.T, d core.TaskDetailOut) core.CommentOut {
	t.Helper()
	if len(d.Comments) == 0 {
		t.Fatal("no comments")
	}
	return d.Comments[len(d.Comments)-1]
}

func TestCommentReplies(t *testing.T) {
	e := setup(t)
	admin := e.org(t, "replies")
	bot := e.agent(t, admin, "Acme Bot", nil)
	task := e.create(t, admin, core.NewTask{Title: "Plan the trip"})[0]
	other := e.create(t, admin, core.NewTask{Title: "Something else"})[0]
	d, err := e.s.Comment(e.ctx, bot, core.CommentIn{ID: task.ID, Body: "Option A is the train.\n\nOption B is   the night bus, cheaper but slow."})
	if err != nil {
		t.Fatal(err)
	}
	orig := last(t, d)
	d, err = e.s.Comment(e.ctx, admin, core.CommentIn{ID: other.ID, Body: "elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := last(t, d)

	// A reply to a message on another task, or to no message at all, is refused.
	for _, id := range []string{elsewhere.ID, "nope", "00000000-0000-0000-0000-000000000000"} {
		if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "this one", ReplyTo: id}); !core.IsCode(err, core.ErrInvalid) {
			t.Fatalf("reply to %q: %v", id, err)
		}
	}

	// A reply with a quote: whitespace folded, the excerpt is the quote.
	d, err = e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "this one", ReplyTo: orig.ID, Quote: "  the night\n bus "})
	if err != nil {
		t.Fatal(err)
	}
	r := last(t, d).ReplyTo
	if r == nil || r.ID != orig.ID || r.Author == nil || r.Author.Name != "Acme Bot" || r.Excerpt != "the night bus" || !r.Quoted {
		t.Fatalf("reply_to: %+v", r)
	}
	// Without a quote, the excerpt is the start of the message, on one line.
	d, err = e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "and why?", ReplyTo: orig.ID})
	if err != nil {
		t.Fatal(err)
	}
	if r := last(t, d).ReplyTo; r == nil || r.Quoted || r.Excerpt != "Option A is the train. Option B is the night bus, cheaper but slow." {
		t.Fatalf("reply_to without quote: %+v", r)
	}
	// A long quote is clipped.
	d, err = e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "long", ReplyTo: orig.ID, Quote: strings.Repeat("x", 2000)})
	if err != nil {
		t.Fatal(err)
	}
	if r := last(t, d).ReplyTo; r == nil || len([]rune(r.Excerpt)) != 500 || !strings.HasSuffix(r.Excerpt, "…") {
		t.Fatalf("long quote: %d runes", len([]rune(r.Excerpt)))
	}
	// A plain comment replies to nothing.
	d, err = e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "plain", Quote: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if r := last(t, d).ReplyTo; r != nil {
		t.Fatalf("plain comment replies to %+v", r)
	}
}

func TestAgentSeesWhatIsRepliedTo(t *testing.T) {
	model, reqs := scriptedModel(t, say("The night bus leaves at 23:10."))
	e, admin, bot := assistantEnv(t, model, "replyctx")
	task := e.create(t, admin, core.NewTask{Title: "Plan the trip"})[0]
	waitTriage(t, e, admin, task.ID)
	d, err := e.s.Comment(e.ctx, bot, core.CommentIn{ID: task.ID, Body: "Option A is the train. Option B is the night bus."})
	if err != nil {
		t.Fatal(err)
	}
	orig := last(t, d)
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot when does it leave?", ReplyTo: orig.ID, Quote: "the night bus"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		d, _ = e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
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
	var input []string
	for _, m := range rs[0].Messages {
		if s, ok := m["content"].(string); ok && m["role"] == "user" {
			input = append(input, s)
		}
	}
	want := admin.Member.Name + ": (replying to you: “the night bus”) @Acme Bot when does it leave?"
	if !strings.Contains(strings.Join(input, "\n"), want) {
		t.Fatalf("model input lacks %q:\n%s", want, strings.Join(input, "\n---\n"))
	}
}
