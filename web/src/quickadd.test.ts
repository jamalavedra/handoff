import { test } from "node:test";
import assert from "node:assert/strict";
import { boardColumn, dueLabel, groupByDue, mentionQuery, parseQuickAdd, splitMentions } from "./quickadd.ts";

// Tuesday 13 Oct 2026.
const today = new Date(2026, 9, 13);
const members = [{ id: "m1", name: "Marta" }, { id: "a1", name: "Legal bot" }, { id: "m2", name: "Mar" }];
const projects = [{ id: "p1", name: "Sales" }, { id: "p2", name: "Q4 Launch" }];
const parse = (s: string) => parseQuickAdd(s, { members, projects, today });

test("reads the worked example", () => {
  assert.deepEqual(parse("Send contract to legal tomorrow @marta #Sales !1"), {
    title: "Send contract to legal", due: "2026-10-14", assignee: members[0], project: projects[0], priority: 1,
  });
});

test("matches multi-word names and picks the longest", () => {
  assert.equal(parse("Review NDA @legal-bot").assignee?.id, "a1");
  assert.equal(parse("Review NDA @Legal bot today").assignee?.id, "a1");
  assert.equal(parse("Call @mar").assignee?.id, "m2");
  assert.equal(parse("Call @marta").assignee?.id, "m1");
  assert.equal(parse("Plan #q4-launch").project?.id, "p2");
  assert.equal(parse("Plan @marta-ish party").assignee, undefined);
  assert.equal(parse("Plan @marta-ish party").title, "Plan @marta-ish party");
  assert.equal(parse("Plan #Sales-ops review").project, undefined);
  assert.equal(parse("Plan #Sales, then ship").project?.id, "p1");
});

test("leaves unknown words and emails alone", () => {
  const p = parse("Email joan@openfort.xyz about #hiring and @nobody");
  assert.equal(p.title, "Email joan@openfort.xyz about #hiring and @nobody");
  assert.equal(p.assignee, undefined);
  assert.equal(p.project, undefined);
});

test("dates", () => {
  assert.equal(parse("x today").due, "2026-10-13");
  assert.equal(parse("x friday").due, "2026-10-16");
  assert.equal(parse("x on tuesday").due, "2026-10-20");
  assert.equal(parse("x next week").due, "2026-10-19");
  assert.equal(parse("x in 3 days").due, "2026-10-16");
  assert.equal(parse("x by Oct 20").due, "2026-10-20");
  assert.equal(parse("x 2 jan").due, "2027-01-02");
  assert.equal(parse("x 2026-11-30").due, "2026-11-30");
  assert.equal(parse("x feb 30").due, undefined);
  assert.equal(parse("x feb 29").due, "2028-02-29");
  assert.equal(parse("Book the monday standup room").due, "2026-10-19");
  assert.equal(parse("Fix todays bug").due, undefined);
  assert.equal(parse("x by Oct 20").title, "x");
});

test("priority only as a standalone token", () => {
  assert.equal(parse("Ship p0").priority, 0);
  assert.equal(parse("Upgrade p10 cluster").priority, undefined);
  assert.equal(parse("Wow!1").priority, undefined);
});

test("due labels", () => {
  assert.deepEqual(dueLabel("2026-10-13", today), { text: "Today", tone: "today" });
  assert.deepEqual(dueLabel("2026-10-14", today), { text: "Tomorrow", tone: "soon" });
  assert.deepEqual(dueLabel("2026-10-12", today), { text: "Yesterday", tone: "overdue" });
  assert.deepEqual(dueLabel("2026-10-16", today), { text: "Fri", tone: "soon" });
  assert.deepEqual(dueLabel("2026-10-01", today), { text: "Oct 1", tone: "overdue" });
  assert.deepEqual(dueLabel("2026-11-03", today), { text: "Nov 3", tone: "later" });
  assert.deepEqual(dueLabel("2027-01-02", today), { text: "Jan 2, 2027", tone: "later" });
});

test("groups by due date", () => {
  const tasks = [
    { id: "a", due: "2026-10-20" }, { id: "b" }, { id: "c", due: "2026-10-13" }, { id: "d", due: "2026-10-01" },
    { id: "e", due: "2026-10-14" }, { id: "f", due: "2026-10-12" }, { id: "g" }, { id: "h", due: "2026-10-13" },
  ];
  assert.deepEqual(groupByDue(tasks, today).map((s) => [s.key, s.tasks.map((t) => t.id).join("")]), [
    ["overdue", "df"], ["today", "ch"], ["upcoming", "ea"], ["none", "bg"],
  ]);
  assert.deepEqual(groupByDue([{ id: "x" }], today).map((s) => s.key), ["none"]);
  assert.deepEqual(groupByDue([], today), []);
});

test("puts tasks in board columns", () => {
  assert.equal(boardColumn({ status: "open" }), "todo");
  assert.equal(boardColumn({ status: "blocked" }), "todo");
  assert.equal(boardColumn({ status: "open", claimed_by: { id: "a1" } }), "working");
  assert.equal(boardColumn({ status: "in_progress" }), "working");
  assert.equal(boardColumn({ status: "done", claimed_by: { id: "a1" } }), "done");
  assert.equal(boardColumn({ status: "cancelled" }), "done");
});

test("finds the mention being typed", () => {
  assert.deepEqual(mentionQuery("hi @leg", 7), { start: 3, query: "leg" });
  assert.deepEqual(mentionQuery("@", 1), { start: 0, query: "" });
  assert.deepEqual(mentionQuery("@legal bot", 10), { start: 0, query: "legal bot" });
  assert.equal(mentionQuery("ops@example", 11), null);
  assert.equal(mentionQuery("hi there", 8), null);
  assert.equal(mentionQuery("@a\nb", 4), null);
});

test("splits mentions, longest name first", () => {
  assert.deepEqual(splitMentions("@legal bot check, cc ops@x.com", ["Legal", "Legal bot"]), [
    { text: "@legal bot", mention: true },
    { text: " check, cc ops@x.com", mention: false },
  ]);
  assert.deepEqual(splitMentions("@nobody", ["Marta"]), [{ text: "@nobody", mention: false }]);
  assert.deepEqual(splitMentions("hi @Marta.", ["Marta"]), [
    { text: "hi ", mention: false },
    { text: "@Marta", mention: true },
    { text: ".", mention: false },
  ]);
});

test("repeats", () => {
  assert.deepEqual(parse("Water plants every day"), { title: "Water plants", due: "2026-10-13", repeat: "daily", assignee: undefined, project: undefined });
  assert.equal(parse("Standup every weekday").repeat, "weekdays");
  // Every friday starts this Friday; every tuesday starts today.
  assert.deepEqual([parse("Send report every friday").repeat, parse("Send report every friday").due, parse("Send report every friday").title], ["weekly", "2026-10-16", "Send report"]);
  assert.equal(parse("x every tuesday").due, "2026-10-13");
  assert.equal(parse("1:1 every other week").repeat, "biweekly");
  assert.equal(parse("Pay rent monthly").repeat, "monthly");
  assert.equal(parse("Renew domain yearly").repeat, "yearly");
  // An explicit date wins for the first occurrence.
  assert.deepEqual([parse("Invoice monthly from Oct 20").repeat, parse("Invoice monthly from Oct 20").due], ["monthly", "2026-10-20"]);
  // Plain weekday words still mean a one-off date.
  assert.equal(parse("Plan monday meeting").repeat, undefined);
});
