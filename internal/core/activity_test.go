package core_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joalavedra/handoff/internal/core"
)

type progressOut struct {
	Org     string             `json:"org"`
	Kind    string             `json:"kind"`
	Task    string             `json:"task"`
	Project string             `json:"project"`
	Member  string             `json:"member"`
	Turn    string             `json:"turn"`
	Step    *core.ActivityStep `json:"step"`
	Done    bool               `json:"done"`
}

// captureProgress collects the progress the service publishes for one workspace.
func captureProgress(e *env, org string) func() []progressOut {
	var mu sync.Mutex
	var got []progressOut
	e.s.OnProgress = func(b []byte) {
		var m progressOut
		if json.Unmarshal(b, &m) == nil && m.Org == org {
			mu.Lock()
			got = append(got, m)
			mu.Unlock()
		}
	}
	return func() []progressOut {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(got)
	}
}

func TestAssistantThoughtsAndProgress(t *testing.T) {
	srv, _ := scriptedModel(t,
		map[string]any{
			"content":           "I'll look for the launch tasks first.",
			"reasoning_content": "The person wants\n\nlaunch work. " + strings.Repeat("x", 600),
			"tool_calls":        []map[string]any{toolCall("c1", "search_tasks", map[string]any{"query": "launch"})},
		},
		say("Nothing about the launch yet."),
	)
	e, admin, _ := assistantEnv(t, srv, "thoughts")
	got := captureProgress(e, admin.Org.ID)

	// The real channel too: a listener on handoff_live hears the steps.
	conn, err := e.s.DB.Pool.Acquire(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(e.ctx, "LISTEN handoff_live"); err != nil {
		t.Fatal(err)
	}

	in := ask("what's up with the launch?")
	in.Turn = "turn-1"
	out, err := e.s.AssistantChat(e.ctx, admin, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Steps) != 3 || out.Steps[0].Type != "thought" || out.Steps[1].Type != "thought" || out.Steps[2].Type != "tool" {
		t.Fatalf("steps: %+v", out.Steps)
	}
	if !strings.HasPrefix(out.Steps[0].Summary, "The person wants launch work.") || len([]rune(out.Steps[0].Summary)) > 281 {
		t.Fatalf("reasoning not summarised: %q", out.Steps[0].Summary)
	}
	if out.Steps[1].Summary != "I'll look for the launch tasks first." || out.Reply != "Nothing about the launch yet." {
		t.Fatalf("plan or reply: %+v", out)
	}

	// Each step went out as it happened, to the person alone, then the turn ended.
	ps := got()
	if len(ps) != 4 || !ps[3].Done || ps[3].Step != nil {
		t.Fatalf("progress: %+v", ps)
	}
	for i, p := range ps {
		if p.Kind != "progress" || p.Member != admin.Member.ID || p.Turn != "turn-1" || p.Task != "" {
			t.Fatalf("progress %d: %+v", i, p)
		}
	}
	if ps[0].Step.Type != "thought" || ps[1].Step.Text != "I'll look for the launch tasks first." || ps[2].Step.Type != "tool" || ps[2].Step.Tool != "search_tasks" {
		t.Fatalf("progress steps: %+v %+v %+v", ps[0].Step, ps[1].Step, ps[2].Step)
	}
	if !strings.Contains(ps[2].Step.Text, "Searched tasks") {
		t.Fatalf("tool step text: %q", ps[2].Step.Text)
	}

	heard := 0
	for heard < 4 {
		ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
		n, err := conn.Conn().WaitForNotification(ctx)
		cancel()
		if err != nil {
			t.Fatalf("heard %d progress notifications: %v", heard, err)
		}
		var m progressOut
		if json.Unmarshal([]byte(n.Payload), &m) == nil && m.Kind == "progress" && m.Org == admin.Org.ID {
			if len(n.Payload) > 8000 {
				t.Fatalf("payload too big: %d", len(n.Payload))
			}
			heard++
		}
	}
}

func TestChatReplyKeepsTrailAndTellsThePerson(t *testing.T) {
	srv, _ := scriptedModel(t,
		map[string]any{"content": "Let me read the task.", "tool_calls": []map[string]any{toolCall("c1", "get_task", map[string]any{"id": "x"})}},
		say("It's on track.\n\nMore detail here."),
	)
	e, admin, bot := assistantEnv(t, srv, "trail")
	got := captureProgress(e, admin.Org.ID)
	task := e.create(t, admin, core.NewTask{Title: "Ship the launch"})[0]
	waitTriage(t, e, admin, task.ID)
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot how is it going?"}); err != nil {
		t.Fatal(err)
	}
	var reply *core.CommentOut
	for i := 0; i < 200 && reply == nil; i++ {
		d, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		for _, c := range d.Comments {
			if c.Author.ID == bot.Member.ID {
				reply = &c
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if reply == nil {
		t.Fatal("no reply")
	}
	if len(reply.Activity) != 2 || reply.Activity[0].Type != "thought" || reply.Activity[0].Text != "Let me read the task." || reply.Activity[1].Type != "tool" || reply.Activity[1].Tool != "get_task" {
		t.Fatalf("activity: %+v", reply.Activity)
	}

	// The person who asked hears it's done, once.
	var in core.InboxOut
	for i := 0; i < 100; i++ {
		var err error
		if in, err = e.s.Inbox(e.ctx, admin, core.InboxIn{}); err != nil {
			t.Fatal(err)
		}
		if len(in.Notifications) > 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if len(in.Notifications) != 1 || in.Notifications[0].Kind != "agent_done" || in.Notifications[0].Excerpt != "It's on track." ||
		in.Notifications[0].Actor.ID != bot.Member.ID || in.Notifications[0].TaskID != task.ID {
		t.Fatalf("inbox: %+v", in.Notifications)
	}
	// The agent itself isn't told about its own work.
	if bin, err := e.s.Inbox(e.ctx, bot, core.InboxIn{}); err != nil || slices.ContainsFunc(bin.Notifications, func(n core.NotificationOut) bool { return n.Kind == "agent_done" }) {
		t.Fatalf("agent inbox: %+v %v", bin, err)
	}

	// Its steps went to the task's viewers (by project, nobody in particular), then the turn ended.
	ended := func(p progressOut) bool { return p.Done && strings.HasPrefix(p.Turn, "agent-") }
	var ps []progressOut
	for i := 0; i < 100 && !slices.ContainsFunc(ps, ended); i++ {
		time.Sleep(25 * time.Millisecond)
		ps = got()
	}
	var steps []string
	for _, p := range ps {
		if !strings.HasPrefix(p.Turn, "agent-") {
			continue
		}
		if p.Task != task.ID || p.Project != task.ProjectID || p.Member != "" {
			t.Fatalf("reply progress scope: %+v", p)
		}
		if p.Step != nil {
			steps = append(steps, p.Step.Type)
		} else if p.Done {
			steps = append(steps, "done")
		}
	}
	if strings.Join(steps, ",") != "thought,tool,done" {
		t.Fatalf("reply progress: %v", steps)
	}
}

func TestFailedReplyIsNotDone(t *testing.T) {
	srv, _ := scriptedModel(t, map[string]any{"_status": http.StatusServiceUnavailable})
	e, admin, bot := assistantEnv(t, srv, "replyfail")
	task := e.create(t, admin, core.NewTask{Title: "Ship it"})[0]
	waitTriage(t, e, admin, task.ID)
	if _, err := e.s.Comment(e.ctx, admin, core.CommentIn{ID: task.ID, Body: "@Acme Bot go"}); err != nil {
		t.Fatal(err)
	}
	var reply *core.CommentOut
	for i := 0; i < 200 && reply == nil; i++ {
		d, _ := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
		for _, c := range d.Comments {
			if c.Author.ID == bot.Member.ID {
				reply = &c
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if reply == nil || !strings.HasPrefix(reply.Body, "I couldn't finish that") {
		t.Fatalf("reply: %+v", reply)
	}
	in, err := e.s.Inbox(e.ctx, admin, core.InboxIn{})
	if err != nil || slices.ContainsFunc(in.Notifications, func(n core.NotificationOut) bool { return n.Kind == "agent_done" }) {
		t.Fatalf("a failed reply said it was done: %+v %v", in.Notifications, err)
	}
}

func TestTriageWorkTellsTheCreator(t *testing.T) {
	srv := fakeModel(t, http.StatusOK, map[string]any{"title": "Compare Pixel 8 and 9", "work": "| | 8 | 9 |"}, new(atomic.Value))
	e := triageEnv(t, srv)
	admin := e.org(t, "triagedone")
	bot := e.agent(t, admin, "Acme Bot", nil)
	if _, err := e.s.SetTriage(e.ctx, admin, core.SetTriageIn{Agent: "Acme Bot", BaseURL: srv.URL + "/v1", Model: "m", Credential: "k"}); err != nil {
		t.Fatal(err)
	}
	got := captureProgress(e, admin.Org.ID)
	task := e.create(t, admin, core.NewTask{Title: "compare pixel 8 vs 9"})[0]
	waitTriage(t, e, admin, task.ID)
	in, err := e.s.Inbox(e.ctx, admin, core.InboxIn{})
	if err != nil {
		t.Fatal(err)
	}
	if len(in.Notifications) != 1 || in.Notifications[0].Kind != "agent_done" || in.Notifications[0].Actor.ID != bot.Member.ID ||
		!strings.HasPrefix(in.Notifications[0].Excerpt, "I tidied the title") {
		t.Fatalf("inbox: %+v", in.Notifications)
	}
	// Its note keeps the trail that was shown live.
	d, err := e.s.GetTask(e.ctx, admin, core.TaskIDIn{ID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Comments) != 1 || len(d.Comments[0].Activity) != 2 || d.Comments[0].Activity[0].Tool != "get_task" || d.Comments[0].Activity[1].Text != "Writing it up" {
		t.Fatalf("triage note trail: %+v", d.Comments)
	}
	// Triage showed it at work on the task.
	for i := 0; i < 100; i++ {
		if ps := got(); len(ps) > 0 && ps[len(ps)-1].Done {
			if ps[0].Turn != "triage-"+task.ID || ps[0].Task != task.ID || ps[0].Step == nil || ps[0].Step.Tool != "get_task" {
				t.Fatalf("triage progress: %+v", ps)
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("triage progress never ended: %+v", got())
}
