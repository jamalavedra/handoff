package beads_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/joalavedra/handoff/internal/beads"
	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/core"
	"github.com/joalavedra/handoff/internal/testutil"
)

func TestRoundTripKeepsCommentsAndStatuses(t *testing.T) {
	ctx := context.Background()
	base, _ := url.Parse("http://handoff.test")
	s := core.New(testutil.DB(t), &config.Config{BaseURL: base})
	org, admin, err := s.CreateOrg(ctx, core.NewOrg{Slug: "acme", Name: "Acme", AdminEmail: "a@acme.test", AdminName: "A"})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := s.Principal(ctx, org, admin.ID, "web")
	in := strings.Join([]string{
		`{"id":"bd-1","title":"wip","status":"in_progress","priority":2,"comments":[{"author":"x","text":"hello","created_at":"2026-01-01T00:00:00Z"}]}`,
		`{"id":"bd-2","title":"dropped","status":"closed","priority":2,"labels":["cancelled"]}`,
	}, "\n")
	if _, err := beads.Import(ctx, s, p, "", strings.NewReader(in)); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := beads.Export(ctx, s, p, "", &buf); err != nil {
		t.Fatal(err)
	}
	got := map[string]beads.Issue{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var is beads.Issue
		if err := json.Unmarshal([]byte(line), &is); err != nil {
			t.Fatal(err)
		}
		got[is.ID] = is
	}
	if got["bd-1"].Status != "in_progress" {
		t.Fatalf("bd-1 status %q", got["bd-1"].Status)
	}
	if len(got["bd-1"].Comments) != 1 || !strings.Contains(got["bd-1"].Comments[0].Text, "hello") {
		t.Fatalf("bd-1 comments %+v", got["bd-1"].Comments)
	}
	res, err := s.SearchTasks(ctx, p, core.SearchTasksIn{IncludeDone: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range res.Tasks {
		if tk.Title == "dropped" && tk.Status != "cancelled" {
			t.Fatalf("cancelled imported as %q", tk.Status)
		}
	}
	if is := got["bd-2"]; is.Status != "closed" || len(is.Labels) != 1 {
		t.Fatalf("bd-2 exported as %+v", is)
	}
}
