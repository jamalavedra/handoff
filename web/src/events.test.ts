import { test } from "node:test";
import assert from "node:assert/strict";
import { eventDetail, revertable, verb } from "./events.ts";
import type { Event } from "./lib.ts";

const ev = (kind: string, data: Record<string, unknown>): Event => ({ id: 1, kind, data, actor: { id: "a", name: "A", kind: "agent" }, via: "mcp", at: "" });

test("task updates name what changed, not how", () => {
  assert.equal(eventDetail(ev("updated", { title: "New", body: "replaced", previous_body: "old", from_project_id: "p" })), "title, text");
});

test("task updates show before and after", () => {
  const e = ev("updated", { title: "New", from_title: "Old", status: "in_progress", from_status: "open", assignee: "Bob", assignee_id: "b", from_assignee_id: "a", body: "appended" });
  assert.equal(eventDetail(e, { a: "Ann" }), "title Old → New, status open → in progress, assignee Ann → Bob, text");
  assert.equal(eventDetail(ev("updated", { due: "", from_due: "2026-11-02" })), "due 2026-11-02 → none");
  assert.ok(revertable(e));
  assert.ok(!revertable(ev("updated", { title: "New" })));
  assert.ok(revertable(ev("todo_updated", { text: "x", done: true, from_done: false })));
  assert.ok(!revertable(ev("todo_updated", { text: "x", moved: true })));
  assert.ok(revertable(ev("todo_updated", { text: "x", moved: true, position: 2, from_position: 0 })));
  assert.ok(revertable(ev("updated", { body: "replaced", previous_body: "old", body_hash: "ab" })));
  assert.equal(eventDetail(ev("updated", { body: "replaced", previous_body: "old", body_hash: "ab" })), "text");
  assert.ok(revertable(ev("note_updated", { note_id: "n", title: "T", body: "replaced", body_hash: "ab" })));
  assert.ok(revertable(ev("todo_removed", { todo_id: "t", text: "x", done: false, result: "", position: 1 })));
  assert.ok(!revertable(ev("todo_removed", { todo_id: "t", text: "x" })));
});

test("todos read as steps", () => {
  assert.equal(verb(ev("todo_updated", { text: "Check what's trending", done: true, result: "6 ideas" })), "ticked a todo on");
  assert.equal(verb(ev("todo_updated", { text: "x", done: false })), "unticked a todo on");
  assert.equal(verb(ev("todo_updated", { text: "x", from: "y" })), "changed a todo on");
  assert.equal(eventDetail(ev("todo_updated", { text: "Check what's trending", done: true, result: "6 ideas" })), "Check what's trending → 6 ideas");
  assert.equal(eventDetail(ev("todos_added", { todos: ["Pick a direction", "Order: by Oct 24"] })), "Pick a direction · Order: by Oct 24");
});

test("note updates say whether the title or the text changed", () => {
  assert.equal(eventDetail(ev("note_updated", { note_id: "n", title: "T", project_id: "p", body: "replaced", previous_body: "x" })), "text");
  assert.equal(eventDetail(ev("note_updated", { note_id: "n", title: "T", from_title: "Old", project_id: "p" })), "title");
  assert.equal(verb(ev("note_created", { note_id: "n", title: "T" })), "wrote a note");
});
