import { test } from "node:test";
import assert from "node:assert/strict";
import { add, all, box, none, ordered, range, toggle, touched } from "./selection.ts";

const order = ["a", "b", "c", "d", "e"];

test("⌘-click toggles one task and moves the anchor", () => {
  let s = toggle(none, "b");
  assert.deepEqual(s, { ids: ["b"], anchor: "b" });
  s = toggle(s, "d");
  assert.deepEqual(s, { ids: ["b", "d"], anchor: "d" });
  s = toggle(s, "b");
  assert.deepEqual(s, { ids: ["d"], anchor: "b" });
});

test("Shift-click selects a range from the anchor, either way", () => {
  assert.deepEqual(range(toggle(none, "b"), order, "d").ids, ["b", "c", "d"]);
  assert.deepEqual(range(toggle(none, "d"), order, "a").ids, ["a", "b", "c", "d"]);
  // A second Shift-click replaces the range but keeps the anchor.
  const s = range(range(toggle(none, "c"), order, "e"), order, "a");
  assert.deepEqual(s, { ids: ["a", "b", "c"], anchor: "c" });
});

test("a range with no anchor on screen selects just the clicked task", () => {
  assert.deepEqual(range(none, order, "c"), { ids: ["c"], anchor: "c" });
  assert.deepEqual(range({ ids: ["x"], anchor: "x" }, order, "c"), { ids: ["c"], anchor: "c" });
  assert.deepEqual(range(none, order, "zz"), none);
});

test("select all, add and visible order", () => {
  assert.deepEqual(all(order), { ids: order, anchor: "a" });
  assert.deepEqual(add({ ids: ["c"], anchor: "c" }, ["a", "c", "e"]), { ids: ["c", "a", "e"], anchor: "c" });
  assert.deepEqual(ordered({ ids: ["e", "gone", "b"] }, order), ["b", "e"]);
});

test("a marquee selects the rows it touches", () => {
  const rows = order.map((id, i) => ({ id, rect: { left: 0, right: 100, top: i * 40, bottom: i * 40 + 36 } }));
  assert.deepEqual(touched(rows, box(50, 50, 10, 125)), ["b", "c", "d"]);
  assert.deepEqual(touched(rows, box(10, 37, 90, 39)), []);
  assert.deepEqual(touched(rows, box(200, 0, 300, 200)), []);
});
