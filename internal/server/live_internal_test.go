package server

import (
	"encoding/json"
	"testing"
)

func TestForViewer(t *testing.T) {
	visible := map[string]bool{"p1": true}
	step := json.RawMessage(`{"type":"thought","text":"I'll check"}`)
	cases := []struct {
		name   string
		m      liveMsg
		member string
		ok     bool
	}{
		{"own assistant turn", liveMsg{Kind: "progress", Member: "me", Turn: "t1", Step: step}, "me", true},
		{"someone else's assistant turn", liveMsg{Kind: "progress", Member: "you", Turn: "t1", Step: step}, "me", false},
		{"task in a visible project", liveMsg{Kind: "progress", Task: "x-1", Project: "p1", Turn: "agent-1", Step: step}, "me", true},
		{"task in a hidden project", liveMsg{Kind: "progress", Task: "x-2", Project: "p2", Turn: "agent-2", Step: step}, "me", false},
		{"task progress without a project", liveMsg{Kind: "progress", Task: "x-3", Turn: "agent-3", Step: step}, "me", false},
		{"event", liveMsg{Kind: "event", Task: "x-1", Project: "p1", Actor: "a"}, "me", true},
		{"event elsewhere", liveMsg{Kind: "event", Task: "x-2", Project: "p2", Actor: "a"}, "me", false},
	}
	for _, c := range cases {
		out, ok := forViewer(c.m, c.member, visible)
		if ok != c.ok {
			t.Errorf("%s: ok=%v", c.name, ok)
			continue
		}
		if ok && c.m.Kind == "progress" && (out.Turn != c.m.Turn || string(out.Step) != string(step)) {
			t.Errorf("%s: %+v", c.name, out)
		}
		if ok && c.m.Kind == "event" && out.Step != nil {
			t.Errorf("%s: event carries a step", c.name)
		}
	}
	// A task moved out of sight: viewers of the old project only hear "refresh".
	out, ok := forViewer(liveMsg{Kind: "event", Task: "x-1", Project: "p2", FromProject: "p1", Actor: "a"}, "me", visible)
	if !ok || out.Task != "" || out.Actor != "" {
		t.Errorf("moved: %+v %v", out, ok)
	}
}
