package core_test

import (
	"testing"

	"github.com/joalavedra/handoff/internal/core"
)

func TestSearchRecentPutsLatestClosedFirst(t *testing.T) {
	e := setup(t)
	acme := e.org(t, "acme")
	urgent, low := 0, 4
	ts := e.create(t, acme, core.NewTask{Title: "Urgent, closed first", Priority: &urgent}, core.NewTask{Title: "Low, closed last", Priority: &low})
	for _, id := range []string{ts[0].ID, ts[1].ID} {
		if _, err := e.s.CloseTask(e.ctx, acme, core.CloseTaskIn{ID: id, Status: "done"}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := e.s.SearchTasks(e.ctx, acme, core.SearchTasksIn{Status: []string{"done"}, Sort: "recent", Limit: 1})
	if err != nil || len(out.Tasks) != 1 || out.Tasks[0].ID != ts[1].ID {
		t.Fatalf("recent: %+v %v", out.Tasks, err)
	}
	if _, err := e.s.SearchTasks(e.ctx, acme, core.SearchTasksIn{Sort: "bogus"}); err == nil {
		t.Fatal("bad sort accepted")
	}
}
