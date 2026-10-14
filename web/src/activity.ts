// The agent's activity: what it does while it works (live, from /api/live) and the trail it leaves on
// its reply. Pure state here; the hook is in agent-activity.ts and the view in components/activity-trail.tsx.

import type { Step, TaskRef } from "./assistant-history";

export type ActivityStep = {
  type: "thought" | "tool" | "lookup" | "write";
  text: string;
  tool?: string;
  source?: string;
  query?: string;
  tasks?: TaskRef[];
  error?: string;
};

// Progress is one live message: a step of a turn, or the turn ending (done).
export type Progress = { kind: "progress"; task?: string; turn?: string; step?: ActivityStep; done?: boolean };

export type Trail = { turn?: string; steps: ActivityStep[]; working: boolean };

export const idle: Trail = { steps: [], working: false };

const maxSteps = 60;

// follows says whether a message belongs to what a view shows: one assistant turn, or a task.
export function follows(m: Progress, want: { task?: string; turn?: string }) {
  if (want.turn) return m.turn === want.turn;
  return !!want.task && m.task === want.task;
}

// advance adds a message to the trail. A new turn starts afresh; done clears it, since by then the
// reply (which keeps its own trail) has landed.
export function advance(t: Trail, m: Progress): Trail {
  if (m.done) return m.turn === t.turn || !t.turn ? idle : t;
  if (!m.step) return t;
  const steps = m.turn === t.turn ? t.steps : [];
  return { turn: m.turn, steps: [...steps, m.step].slice(-maxSteps), working: true };
}

const writeTools = ["create_tasks", "update_task", "close_task", "comment"];

// fromSteps reads an assistant reply's steps (older ones have no type) as a trail.
export function fromSteps(steps: Step[]): ActivityStep[] {
  return steps.map((s) => {
    const type = s.lookup ? "lookup" : s.type ?? (writeTools.includes(s.tool) ? "write" : "tool");
    return { type, text: s.summary, tool: s.lookup?.tool || s.tool || undefined, source: s.lookup?.source, query: s.lookup?.query, tasks: s.tasks, error: s.error };
  });
}

// toolName drops the prefix a connected tool carries for the model (ext_<connection>__), for trails
// saved before steps carried the service's own tool name.
const toolName = (fn = "") => fn.replace(/^ext_.*?__/, "");

// line is how a step reads in one line: a lookup as "Source · “query”", anything else as its text.
export function line(s: ActivityStep) {
  if (s.type === "lookup" && s.source) return `${s.source} · ${s.query ? `“${s.query}”` : toolName(s.tool)}`;
  return s.text;
}

// summary is the folded trail: a headline for what the agent did, how many steps (thoughts aren't
// steps) and how many failed.
export function summary(steps: ActivityStep[]) {
  const n = steps.filter((s) => s.type !== "thought").length;
  const failed = steps.filter((s) => s.error).length;
  const label = n === 0 ? "reasoning" : n === 1 ? "1 step" : `${n} steps`;
  const sources = [...new Set(steps.filter((s) => s.type === "lookup" && s.source).map((s) => s.source!))];
  const headline = sources.length ? `Looked up ${sources.join(", ")}`
    : wrote(steps) ? "Made changes"
    : n === 0 ? "Thought it through"
    : "Checked the tasks";
  return { headline, label, failed };
}

// wrote says whether the turn changed something, so its trail starts open (changes one click away).
export const wrote = (steps: ActivityStep[]) => steps.some((s) => s.type === "write" && !s.error);
