import { test } from "node:test";
import assert from "node:assert/strict";
import { linkify, moveTodo, oneLine, patchTodo, progressLabel, type Todo } from "./todos.ts";

const todo = (id: string, text: string, position: number): Todo => ({ id, text, done: false, position });
const list = [todo("a", "Check what's trending", 0), todo("b", "Pick a direction", 1), todo("c", "Order: by Oct 24", 2)];

test("progress label", () => {
  assert.equal(progressLabel({ done: 2, total: 5 }), "2/5");
  assert.equal(progressLabel({ done: 0, total: 0 }), "");
  assert.equal(progressLabel(undefined), "");
});

test("linkifies addresses, leaving punctuation", () => {
  assert.deepEqual(linkify("See https://example.com/a, then (https://x.org/b)."), [
    { text: "See " },
    { text: "https://example.com/a", url: "https://example.com/a" },
    { text: ", then (" },
    { text: "https://x.org/b", url: "https://x.org/b" },
    { text: ")." },
  ]);
  assert.deepEqual(linkify("https://en.wikipedia.org/wiki/Dolly_(sheep)"), [{ text: "https://en.wikipedia.org/wiki/Dolly_(sheep)", url: "https://en.wikipedia.org/wiki/Dolly_(sheep)" }]);
  assert.deepEqual(linkify("no links"), [{ text: "no links" }]);
  assert.deepEqual(linkify(""), []);
});

test("one line", () => {
  assert.equal(oneLine("  Order:\n by  Oct 24 "), "Order: by Oct 24");
});

test("patches and moves todos", () => {
  assert.equal(patchTodo(list, "b", { done: true })[1].done, true);
  assert.equal(patchTodo(list, "b", { done: true })[0].done, false);
  assert.deepEqual(moveTodo(list, "c", 0).map((t) => [t.id, t.position]), [["c", 0], ["a", 1], ["b", 2]]);
  assert.deepEqual(moveTodo(list, "a", 9).map((t) => t.id), ["b", "c", "a"]);
  assert.equal(moveTodo(list, "zz", 0), list);
});
