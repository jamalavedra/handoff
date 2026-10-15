import { test } from "node:test";
import assert from "node:assert/strict";
import { activate, arrive, close, fresh, open, restore, resumeHistory, stepHistory, tabHref, target, visit, type Tabs } from "./tabs.ts";

const s0: Tabs = { tabs: [{ id: "a", href: "/" }, { id: "b", href: "/activity" }], active: "a" };
const hrefs = (s: Tabs) => s.tabs.map((t) => t.href);

test("reads what a location shows", () => {
  assert.deepEqual(target("/tasks/acme-a9s2"), { kind: "task", id: "acme-a9s2" });
  assert.deepEqual(target("/projects/p1?task=acme-1"), { kind: "project", id: "p1" });
  assert.deepEqual(target("/?task=acme-1"), { kind: "page", path: "/" });
  assert.deepEqual(target("/projects/p1?view=board"), { kind: "project", id: "p1" });
  assert.deepEqual(target("/activity"), { kind: "page", path: "/activity" });
  assert.deepEqual(target("/notes/acme-n4b2i"), { kind: "note", id: "acme-n4b2i" });
  assert.deepEqual(target("/"), { kind: "page", path: "/" });
});

test("the active tab follows navigation", () => {
  const s = visit(s0, "/team", false);
  assert.deepEqual(hrefs(s), ["/team", "/activity"]);
  assert.equal(s.active, "a");
  assert.equal(visit(s, "/team", false), s);
  // A plain navigation onto another tab's location doesn't switch tabs.
  assert.deepEqual(hrefs(visit(s0, "/activity", false)), ["/activity", "/activity"]);
});

test("back onto another tab's location switches to it", () => {
  const s = visit(s0, "/activity", true);
  assert.equal(s.active, "b");
  assert.deepEqual(hrefs(s), ["/", "/activity"]);
  assert.deepEqual(hrefs(visit(s0, "/trash", true)), ["/trash", "/activity"]);
});

test("background tabs open in order after the active one", () => {
  let s = open(s0, "/tasks/t1", "c");
  s = open(s, "/tasks/t2", "d");
  assert.deepEqual(hrefs(s), ["/", "/tasks/t1", "/tasks/t2", "/activity"]);
  assert.equal(s.active, "a");
  // Already open: no duplicate.
  assert.equal(open(s, "/tasks/t1", "e"), s);
  assert.equal(open(s, "/tasks/t1", "e", { foreground: true }).active, "c");
  // From another tab, the run starts fresh.
  s = open(activate(s, "c"), "/tasks/t3", "f");
  assert.deepEqual(hrefs(s), ["/", "/tasks/t1", "/tasks/t3", "/tasks/t2", "/activity"]);
});

test("foreground tabs become active", () => {
  const s = open(s0, "/", "c", { foreground: true });
  assert.deepEqual([s.active, hrefs(s)], ["a", ["/", "/activity"]]);
  // A new tab (+) is new even when My tasks is already open, and goes last.
  const d = fresh(s0, "c");
  assert.deepEqual([d.active, hrefs(d)], ["c", ["/", "/activity", "/"]]);
  const t = open(s0, "/team", "c", { foreground: true });
  assert.equal(t.active, "c");
  assert.deepEqual(hrefs(t), ["/", "/team", "/activity"]);
});

test("closing picks a neighbour", () => {
  const s: Tabs = { tabs: [{ id: "a", href: "/" }, { id: "b", href: "/x" }, { id: "c", href: "/y" }], active: "b" };
  assert.equal(close(s, "b", "n").active, "c");
  assert.equal(close({ ...s, active: "c" }, "c", "n").active, "b");
  assert.deepEqual(close(s, "a", "n"), { tabs: s.tabs.slice(1), active: "b" });
  assert.equal(close(s, "zz", "n"), s);
  assert.deepEqual(close({ tabs: [{ id: "a", href: "/x" }], active: "a" }, "a", "n"), { tabs: [{ id: "n", href: "/" }], active: "n" });
  assert.equal(activate(s, "zz"), s);
});

test("restores saved tabs around the loaded URL", () => {
  // A reload lands on its own tab.
  assert.deepEqual(restore({ ...s0, active: "b" }, "/activity", "n"), { ...s0, active: "b" });
  // Another link opens beside the active tab.
  const s = restore(s0, "/tasks/t9", "n");
  assert.deepEqual(hrefs(s), ["/", "/tasks/t9", "/activity"]);
  assert.equal(s.active, "n");
  // Junk and duplicates are dropped.
  assert.deepEqual(restore(null, "/", "n"), { tabs: [{ id: "n", href: "/" }], active: "n" });
  assert.deepEqual(restore({ tabs: "x" }, "/", "n"), { tabs: [{ id: "n", href: "/" }], active: "n" });
  const junk = { tabs: [{ id: "a", href: "/" }, { id: "a", href: "/x" }, { id: "b", href: "/" }, { id: "c", href: "http://evil" }, { id: "e", href: "//evil" }, null, { id: "d", href: "/y" }], active: "gone" };
  assert.deepEqual(restore(junk, "/y", "n"), { tabs: [{ id: "a", href: "/" }, { id: "b", href: "/" }, { id: "d", href: "/y" }], active: "d" });
  // Reloading the second of two tabs on the same page stays on it.
  assert.equal(restore({ tabs: [{ id: "a", href: "/" }, { id: "b", href: "/" }], active: "b" }, "/", "n").active, "b");
});

test("background order survives a reload", () => {
  let s: Tabs = { tabs: [{ id: "a", href: "/" }], active: "a" };
  s = open(open(s, "/b", "b"), "/c", "c");
  s = restore(JSON.parse(JSON.stringify(s)), "/", "n");
  assert.deepEqual(hrefs(open(s, "/d", "d")), ["/", "/b", "/c", "/d"]);
  // An opener that points at a closed tab is dropped.
  const r = restore({ tabs: [{ id: "a", href: "/" }, { id: "b", href: "/b", opener: "gone" }], active: "a" }, "/", "n");
  assert.equal(r.tabs[1].opener, undefined);
});

test("arriving from outside", () => {
  assert.equal(arrive(s0, "/", "n"), s0);
  assert.equal(arrive(s0, "/activity", "n").active, "b");
  assert.deepEqual(hrefs(arrive(s0, "/team", "n")), ["/", "/team", "/activity"]);
});

test("a task in the side panel is held as its full page", () => {
  assert.equal(tabHref("/?task=acme-1"), "/tasks/acme-1");
  assert.equal(tabHref("/projects/p1?view=board&task=a%2Fb"), "/tasks/a%2Fb");
  assert.equal(tabHref("/projects/p1?view=board"), "/projects/p1?view=board");
  assert.equal(tabHref("/tasks/acme-1"), "/tasks/acme-1");
  assert.equal(tabHref("/?task="), "/?task=");
  assert.equal(tabHref("/tasks/a?task=b"), "/tasks/a?task=b");
  assert.deepEqual(target("/tasks/%E0%A4"), { kind: "task", id: "%E0%A4" });

  // Opening a task in the panel turns the active tab into that task; selecting it shows the page.
  const s = visit(s0, "/?task=acme-1", false);
  assert.deepEqual(hrefs(s), ["/tasks/acme-1", "/activity"]);
  assert.equal(visit(s, "/tasks/acme-1", false), s);
  assert.deepEqual(hrefs(visit(s, "/", false)), ["/", "/activity"]);
  assert.deepEqual(hrefs(open(s0, "/projects/p1?task=acme-2", "n")), ["/", "/tasks/acme-2", "/activity"]);
  assert.equal(open(s, "/?task=acme-1", "n", { foreground: true }).active, "a");
  assert.deepEqual(hrefs(arrive(s0, "/team?task=acme-3", "n")), ["/", "/tasks/acme-3", "/activity"]);

  // Tabs saved with a panel location come back as task pages.
  const saved = { tabs: [{ id: "a", href: "/?task=acme-1" }, { id: "b", href: "/team" }], active: "b" };
  assert.deepEqual(hrefs(restore(saved, "/team", "n")), ["/tasks/acme-1", "/team"]);
});

test("forward bookkeeping belongs to one history", () => {
  // Two pushes, then back: forward is available, and survives a reload of the same entry.
  let m = resumeHistory(null, 0, "k0");
  m = stepHistory(m, 1, "k1", true);
  m = stepHistory(m, 2, "k2", true);
  m = stepHistory(m, 1, "k1", false);
  assert.equal(m.max, 2);
  const saved = JSON.parse(JSON.stringify(m));
  assert.equal(resumeHistory(saved, 1, "k1").max, 2);
  // A new browser tab inherits the storage but not the history: its entry key doesn't match.
  assert.equal(resumeHistory(saved, 0, "fresh").max, 0);
  assert.equal(resumeHistory(saved, 1, "other").max, 1);
  // A push cuts off the forward entries.
  assert.deepEqual(stepHistory(m, 2, "k3", true), { keys: ["k0", "k1", "k3"], max: 2 });
  assert.equal(stepHistory(stepHistory(m, 2, "k3", true), 1, "k1", false).max, 2);
});
