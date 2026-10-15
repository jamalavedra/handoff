import { test } from "node:test";
import assert from "node:assert/strict";
import { eventDetail, verb } from "./events.ts";
import type { Event } from "./lib.ts";

const ev = (kind: string, data: Record<string, unknown>): Event => ({ id: 1, kind, data, actor: { id: "a", name: "A", kind: "agent" }, via: "mcp", at: "" });

test("task updates name what changed, not how", () => {
  assert.equal(eventDetail(ev("updated", { title: "New", body: "replaced", previous_body: "old", from_project_id: "p" })), "title, text");
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
