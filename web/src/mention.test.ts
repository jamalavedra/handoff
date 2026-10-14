import { test } from "node:test";
import assert from "node:assert/strict";
import { insertAt, matchMembers, mentionItems, mentionText, replaceQuery, step } from "./mention.ts";

const members = [
  { id: "1", name: "Joan Alavedra" },
  { id: "2", name: "Ada" },
  { id: "3", name: "Triage agent" },
  { id: "4", name: "Mariana" },
];
const names = (ms: { name: string }[]) => ms.map((m) => m.name);

test("members starting with the query come first", () => {
  assert.deepEqual(names(matchMembers(members, "a")), ["Ada", "Joan Alavedra", "Triage agent", "Mariana"]);
  assert.deepEqual(names(matchMembers(members, "AG")), ["Triage agent"]);
  assert.deepEqual(names(matchMembers(members, "ari")), ["Mariana"]);
  assert.deepEqual(names(matchMembers(members, "zz")), []);
  assert.deepEqual(names(matchMembers(members, "", 2)), ["Joan Alavedra", "Ada"]);
  assert.equal(matchMembers(members, "a", 2).length, 2);
});

test("menu order is members then tasks, without the current task", () => {
  const tasks = [{ id: "t1", title: "One" }, { id: "t2", title: "Two" }, { id: "t3", title: "Three" }, { id: "t4", title: "Four" }];
  const items = mentionItems(members, tasks, "", "t2");
  assert.deepEqual(items.map((i) => i.kind), ["member", "member", "member", "member", "task", "task", "task"]);
  assert.deepEqual(items.flatMap((i) => (i.kind === "task" ? [i.task.id] : [])), ["t1", "t3", "t4"]);
  assert.deepEqual(mentionItems(members, tasks, "ada").map((i) => i.kind), ["member", "task", "task", "task", "task"]);
  assert.deepEqual(mentionItems([], [], "x"), []);
});

test("the highlight wraps at both ends", () => {
  assert.equal(step(0, 1, 3), 1);
  assert.equal(step(2, 1, 3), 0);
  assert.equal(step(0, -1, 3), 2);
  assert.equal(step(5, 0, 3), 2);
  assert.equal(step(0, 1, 0), 0);
});

test("picking writes a mention or a task link", () => {
  assert.equal(mentionText({ kind: "member", member: { id: "2", name: "Ada" } }), "@Ada ");
  assert.equal(mentionText({ kind: "task", task: { id: "acme-1", title: "Ship [beta]\n now" } }), "[Ship \\[beta\\] now](/tasks/acme-1) ");
  assert.equal(mentionText({ kind: "task", task: { id: "acme-2", title: "  " } }), "[acme-2](/tasks/acme-2) ");
});

test("the typed query is replaced, the rest kept", () => {
  assert.deepEqual(replaceQuery("hi @ad there", 3, 6, "@Ada "), { text: "hi @Ada  there", caret: 8 });
});

test("the @ button inserts at the caret", () => {
  assert.deepEqual(insertAt("", 0), { text: "@", caret: 1 });
  assert.deepEqual(insertAt("hi", 2), { text: "hi @", caret: 4 });
  assert.deepEqual(insertAt("hi ", 3), { text: "hi @", caret: 4 });
  assert.deepEqual(insertAt("ab", 1), { text: "a @b", caret: 3 });
});
