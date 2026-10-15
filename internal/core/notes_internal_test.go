package core

import "testing"

func TestTaskContext(t *testing.T) {
	got := taskContext("Buy Halloween costume", "Get it sorted early.\n",
		[]TodoOut{{ID: "t1", Text: "Check what's trending", Done: true, Result: "6 ideas"}, {ID: "t2", Text: "Pick a direction"}},
		[]noteRow{{ID: "acme-n1", Title: "Halloween costume — research", Body: "## Ideas\n- Dolly"}, {ID: "acme-n2", Title: "Old"}})
	want := "Title: Buy Halloween costume\n\nGet it sorted early.\n\nTodos:\n- [x] Check what's trending → 6 ideas (todo id t1)\n- [ ] Pick a direction (todo id t2)\n\n" +
		"Note “Halloween costume — research” (note id acme-n1):\n## Ideas\n- Dolly\n\nAlso a note “Old” (note id acme-n2)."
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := taskContext("Just a title", "", nil, nil); got != "Title: Just a title" {
		t.Fatalf("bare task: %q", got)
	}
}

func TestWorkKind(t *testing.T) {
	for in, want := range map[string]string{"Research": "research", "  shortlist of options": "shortlist", "": "notes", "plan.": "plan"} {
		if got := workKind(in); got != want {
			t.Errorf("workKind(%q) = %q, want %q", in, got, want)
		}
	}
}
