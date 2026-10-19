import { test } from "node:test";
import assert from "node:assert/strict";
import { activeFilters, arrange, defaultPrefs, emptyFilter, filterTasks, readPrefs, sortTasks } from "./tasklist.ts";

const today = new Date(2026, 9, 13); // Oct 13, 2026
const t = (id: string, o: { priority?: number; due?: string; status?: string; title?: string; created?: string; updated?: string } = {}) => ({
  id, title: o.title ?? id, priority: o.priority ?? 2, due: o.due, status: o.status ?? "open",
  created_at: o.created ?? "2026-10-01T00:00:00Z", updated_at: o.updated ?? "2026-10-01T00:00:00Z",
});
const ids = (xs: { id: string }[]) => xs.map((x) => x.id).join("");

const tasks = [
  t("a", { priority: 2, due: "2026-10-20", created: "2026-10-03T00:00:00Z", updated: "2026-10-05T00:00:00Z", title: "Zebra" }),
  t("b", { priority: 0, created: "2026-10-02T00:00:00Z", title: "apple" }),
  t("c", { priority: 2, due: "2026-10-01", status: "in_progress", created: "2026-10-04T00:00:00Z", updated: "2026-10-09T00:00:00Z", title: "Mango" }),
  t("d", { priority: 1, due: "2026-10-13", title: "banana" }),
];

test("sorts", () => {
  assert.equal(ids(sortTasks(tasks, "manual")), "abcd"); // the server's order, as it came
  assert.equal(ids(sortTasks(tasks, "status")), "cabd"); // in progress first
  assert.equal(ids(sortTasks(tasks, "due")), "cdab");
  assert.equal(ids(sortTasks(tasks, "created")), "cabd");
  assert.equal(ids(sortTasks(tasks, "updated")), "cabd");
  assert.equal(ids(sortTasks(tasks, "title")), "bdca");
  assert.equal(ids(tasks), "abcd"); // the input is left alone
});

test("filters", () => {
  assert.equal(ids(filterTasks(tasks, emptyFilter, today)), "abcd");
  assert.equal(ids(filterTasks(tasks, { ...emptyFilter, status: ["in_progress"] }, today)), "c");
  assert.equal(ids(filterTasks(tasks, { ...emptyFilter, priority: [0, 1] }, today)), "bd");
  assert.equal(ids(filterTasks(tasks, { ...emptyFilter, due: ["overdue", "none"] }, today)), "bc");
  assert.equal(ids(filterTasks(tasks, { ...emptyFilter, due: ["week"] }, today)), "ad");
  assert.equal(ids(filterTasks(tasks, { ...emptyFilter, due: ["today"], priority: [2] }, today)), "");
  // Done tasks (shown only when asked for) aren't hidden by a status pick.
  assert.equal(ids(filterTasks([t("x", { status: "done" })], { ...emptyFilter, status: ["open"] }, today)), "x");
  assert.equal(ids(filterTasks(tasks, emptyFilter, today, "an")), "cd"); // mANgo, bANana
  assert.equal(ids(filterTasks(tasks, emptyFilter, today, "BAN  na")), "d");
  assert.equal(activeFilters({ status: ["open"], priority: [0, 1], due: [] }), 3);
});

test("arranges flat by default, by due date when grouped", () => {
  assert.deepEqual(arrange(tasks, defaultPrefs, today).map((s) => [s.key, ids(s.tasks)]), [["all", "abcd"]]);
  assert.deepEqual(arrange(tasks, { ...defaultPrefs, group: true }, today).map((s) => [s.key, ids(s.tasks)]), [["overdue", "c"], ["today", "d"], ["upcoming", "a"], ["none", "b"]]);
  assert.deepEqual(arrange([], defaultPrefs, today), []);
});

test("reads saved preferences, dropping what's invalid", () => {
  assert.deepEqual(readPrefs(null), defaultPrefs);
  assert.deepEqual(readPrefs("not json"), defaultPrefs);
  assert.deepEqual(readPrefs(JSON.stringify({ sort: "due", group: true, showDone: true, rows: "compact", hidden: ["due", "nope"], filter: { status: ["open", "bogus"], priority: [1, 9], due: ["today", "x"] } })), {
    sort: "due", group: true, showDone: true, rows: "compact", hidden: ["due"], filter: { status: ["open"], priority: [1], due: ["today"] },
  });
  assert.equal(readPrefs(JSON.stringify({ sort: "priority" })).sort, "manual");
});
