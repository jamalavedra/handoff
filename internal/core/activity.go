package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
)

// Progress: while the workspace's agent works (an assistant turn, a reply in a task's chat, a task
// handed to it, triage), each step goes out on handoff_live as it happens, so open screens can show
// what it's doing. A step says what was asked and how it went, never what a tool returned:
//
//	{"org", "kind": "progress", "task"?, "project"?, "member"?, "turn", "step"?, "done"?}
//
// A turn in someone's own assistant names the member, and only they hear it; work on a task names the
// task and its project, and whoever can see that project hears it. done ends the turn.

// ActivityStep is one thing the agent did while working, for the trail under its reply.
type ActivityStep struct {
	Type   string             `json:"type" jsonschema:"thought (what it said it would do, or a summary of its reasoning), tool, lookup (a connected service) or write"`
	Text   string             `json:"text"`
	Tool   string             `json:"tool,omitempty"`
	Source string             `json:"source,omitempty"`
	Query  string             `json:"query,omitempty"`
	Tasks  []AssistantTaskRef `json:"tasks,omitempty"`
	Error  string             `json:"error,omitempty"`
}

const (
	progressMaxSteps = 60  // per turn; a runaway turn stops talking rather than flooding every screen
	thoughtChars     = 280 // a thought is a line or two, not the model's whole reasoning
	stepTextChars    = 200
	progressMaxBytes = 7000 // pg_notify's payload limit is 8000
	progressTimeout  = 2 * time.Second
)

// progress is who hears one turn's steps.
type progress struct {
	org, task, project, member, turn string
	sent                             int
	last                             string
	trail                            []ActivityStep // the steps sent, for the message that ends the turn
}

type progressMsg struct {
	Org     string        `json:"org"`
	Kind    string        `json:"kind"`
	Task    string        `json:"task,omitempty"`
	Project string        `json:"project,omitempty"`
	Member  string        `json:"member,omitempty"`
	Turn    string        `json:"turn,omitempty"`
	Step    *ActivityStep `json:"step,omitempty"`
	Done    bool          `json:"done,omitempty"`
}

// step publishes one step. It never fails the work: a lost step only means a screen shows less.
func (s *Service) progressStep(ctx context.Context, pr *progress, st ActivityStep) {
	if pr == nil || pr.sent >= progressMaxSteps {
		return
	}
	st = st.clipped()
	key := st.Type + "\x00" + st.Text + "\x00" + st.Error
	if key == pr.last {
		return // the same step again (a model repeating itself) isn't news
	}
	pr.sent++
	pr.last = key
	pr.trail = append(pr.trail, st)
	s.publishProgress(ctx, pr, &st, false)
}

// progressDone tells screens the turn is over, so they stop showing it as live.
func (s *Service) progressDone(ctx context.Context, pr *progress) {
	if pr != nil {
		s.publishProgress(ctx, pr, nil, true)
	}
}

// publishProgress never holds up the work: a stalled database costs a step at most progressTimeout.
func (s *Service) publishProgress(ctx context.Context, pr *progress, st *ActivityStep, done bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), progressTimeout)
	defer cancel()
	m := progressMsg{Org: pr.org, Kind: "progress", Task: pr.task, Project: pr.project, Member: pr.member, Turn: pr.turn, Step: st, Done: done}
	b, _ := json.Marshal(m)
	if len(b) > progressMaxBytes && st != nil {
		st.Tasks = nil
		b, _ = json.Marshal(m)
	}
	if len(b) > progressMaxBytes {
		return
	}
	if s.OnProgress != nil {
		s.OnProgress(b)
	}
	if _, err := s.DB.Pool.Exec(ctx, `SELECT pg_notify('handoff_live', $1)`, string(b)); err != nil {
		slog.Debug("progress", "err", err)
	}
}

// clipped keeps a step small enough for a notification and a comment.
func (st ActivityStep) clipped() ActivityStep {
	n := stepTextChars
	if st.Type == "thought" {
		n = thoughtChars
	}
	st.Text = clip(oneLine(st.Text), n)
	st.Query = clip(st.Query, 80)
	st.Error = clip(st.Error, 200)
	if len(st.Tasks) > 10 {
		st.Tasks = st.Tasks[:10]
	}
	tasks := make([]AssistantTaskRef, 0, len(st.Tasks))
	for _, t := range st.Tasks {
		tasks = append(tasks, AssistantTaskRef{ID: t.ID, Title: clip(t.Title, 80)})
	}
	st.Tasks = tasks
	if len(st.Tasks) == 0 {
		st.Tasks = nil
	}
	return st
}

// oneLine folds whitespace, so a paragraph of reasoning reads as one line in the trail.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// thoughts are what the model said alongside its tool calls: a summary of its reasoning when the
// provider returns one (reasoning_content or reasoning), then its stated plan.
func thoughts(m assistantMsg) []string {
	var out []string
	r := m.ReasoningContent
	if r == "" {
		r = m.Reasoning
	}
	if r = oneLine(r); r != "" {
		out = append(out, r)
	}
	if c := oneLine(m.Content); c != "" {
		out = append(out, c)
	}
	return out
}

// activity is a turn's steps as the trail keeps them.
func activity(steps []AssistantStep) []ActivityStep {
	out := make([]ActivityStep, 0, len(steps))
	for _, st := range steps {
		out = append(out, st.activity())
	}
	return out
}

func (st AssistantStep) activity() ActivityStep {
	a := ActivityStep{Type: st.Type, Text: st.Summary, Tool: st.Tool, Tasks: st.Tasks, Error: st.Error}
	if a.Type == "" {
		a.Type = "tool"
	}
	if st.Lookup != nil {
		// The service's own tool name, not the identifier the model was given for it.
		a.Type, a.Source, a.Query, a.Tool = "lookup", st.Lookup.Source, st.Lookup.Query, st.Lookup.Tool
	}
	if a.Type == "thought" {
		a.Tool = ""
	}
	return a.clipped()
}

// firstLine is the start of a message, for a notification.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(strings.TrimLeft(s, "#*-> "))
}
