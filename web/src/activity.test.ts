import { test } from "node:test";
import assert from "node:assert/strict";
import { advance, follows, fromSteps, idle, line, summary, wrote, type Progress } from "./activity.ts";

const thought: Progress = { kind: "progress", task: "x-1", turn: "agent-1", step: { type: "thought", text: "I'll check the venues" } };
const lookup: Progress = { kind: "progress", task: "x-1", turn: "agent-1", step: { type: "lookup", text: "Web: search", source: "Web", query: "venues sf", tool: "search" } };

test("follows a turn, or a task", () => {
  assert.ok(follows(thought, { turn: "agent-1" }));
  assert.ok(!follows(thought, { turn: "t-2" }));
  assert.ok(follows(thought, { task: "x-1" }));
  assert.ok(!follows(thought, { task: "x-2" }));
  assert.ok(!follows({ kind: "progress", turn: "t" }, {}));
});

test("builds the live trail and clears it when the turn ends", () => {
  let t = advance(idle, thought);
  t = advance(t, lookup);
  assert.equal(t.working, true);
  assert.deepEqual(t.steps.map((s) => s.type), ["thought", "lookup"]);
  // Another turn on the task starts afresh.
  const other = advance(t, { ...thought, turn: "triage-x-1" });
  assert.equal(other.steps.length, 1);
  // The end of some other turn doesn't clear this one; its own does.
  assert.equal(advance(t, { kind: "progress", task: "x-1", turn: "agent-9", done: true }), t);
  assert.deepEqual(advance(t, { kind: "progress", task: "x-1", turn: "agent-1", done: true }), idle);
});

test("reads assistant steps, old and new", () => {
  const steps = fromSteps([
    { tool: "", type: "thought", summary: "Let me look" },
    { tool: "update_task", summary: "Updated “Plan”", tasks: [{ id: "a-1", title: "Plan" }] },
    { tool: "ext_web_search", summary: "Web: search · sf", lookup: { source: "Web", tool: "search", query: "sf" } },
    { tool: "search_tasks", summary: "Searched tasks", error: "boom" },
  ]);
  assert.deepEqual(steps.map((s) => s.type), ["thought", "write", "lookup", "tool"]);
  assert.equal(line(steps[2]), "Web · “sf”");
  assert.equal(line({ type: "lookup", text: "", source: "Centaur", tool: "ext_centaur__whoami" }), "Centaur · whoami");
  assert.equal(line(steps[1]), "Updated “Plan”");
  assert.deepEqual(summary(steps), { headline: "Looked up Web", label: "3 steps", failed: 1 });
  assert.ok(wrote(steps));
  assert.deepEqual(summary(steps.slice(0, 1)), { headline: "Thought it through", label: "reasoning", failed: 0 });
  assert.equal(summary(steps.slice(0, 2)).headline, "Made changes");
  assert.equal(summary(steps.slice(3)).headline, "Checked the tasks");
  assert.equal(summary([...steps, { type: "write", text: "Handed it back", tool: "ask_person" }]).headline, "Needs your input");
});
