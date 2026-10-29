import { test } from "node:test";
import assert from "node:assert/strict";
import { createHistory, inversePatch } from "./undo.ts";
import type { Task } from "./lib.ts";

test("undo and redo walk the history; a new change clears redo", () => {
  const h = createHistory<string>();
  assert.equal(h.undo(), undefined);
  h.push("a");
  h.push("b");
  assert.equal(h.undo(), "b");
  assert.equal(h.undo(), "a");
  assert.equal(h.undo(), undefined);
  assert.equal(h.redo(), "a");
  h.push("c");
  assert.equal(h.redo(), undefined);
  assert.equal(h.undo(), "c");
  assert.equal(h.undo(), "a");
});

test("a toast's Undo takes back its own change, once", () => {
  const h = createHistory<string>();
  h.push("a");
  h.push("b");
  assert.equal(h.undo("a"), "a");
  assert.equal(h.undo("a"), undefined);
  assert.equal(h.undo(), "b");
  assert.equal(h.redo(), "b");
});

test("a change that can't be redone is gone once undone", () => {
  const h = createHistory<string>(50, (s) => s !== "once");
  h.push("a");
  h.push("once");
  assert.equal(h.undo(), "once");
  assert.equal(h.redo(), undefined);
  assert.equal(h.undo(), "a");
  assert.equal(h.redo(), "a");
});

test("the history keeps the newest changes", () => {
  const h = createHistory<number>(2);
  [1, 2, 3].forEach((n) => h.push(n));
  assert.equal(h.undo(), 3);
  assert.equal(h.undo(), 2);
  assert.equal(h.undo(), undefined);
});

test("the inverse of a task edit is its old values", () => {
  const t = { id: "t", title: "Old", status: "done", priority: 2, project_id: "p", labels: [], assignee: { id: "m", name: "M", kind: "human" } } as unknown as Task;
  assert.deepEqual(inversePatch(t, { title: "New", status: "open", due: "2026-11-01", assignee: "" }), { title: "Old", status: "done", due: "", assignee: "m" });
  assert.deepEqual(inversePatch(t, { body: "x", body_mode: "replace" }), {});
});
