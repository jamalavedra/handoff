// The ⌘Z / ⇧⌘Z history of this tab's own changes: what was done, newest last, and what was undone.

import type { Task } from "./lib";

// canRedo says which undone changes ⇧⌘Z can bring back; the others are gone once undone.
export function createHistory<S>(limit = 50, canRedo: (s: S) => boolean = () => true) {
  let past: S[] = [];
  let future: S[] = [];
  return {
    // A new change starts a new branch: what was undone can no longer be redone.
    push(s: S) {
      past = [...past, s].slice(-limit);
      future = [];
    },
    // undo takes the newest change, or a given one (a toast's Undo button), off the past.
    undo(s: S | undefined = past.at(-1)) {
      if (s === undefined || !past.includes(s)) return undefined;
      past = past.filter((x) => x !== s);
      if (canRedo(s)) future = [...future, s];
      return s;
    },
    redo() {
      const s = future.at(-1);
      if (s === undefined) return undefined;
      future = future.slice(0, -1);
      past = [...past, s];
      return s;
    },
  };
}

export type TaskPatch = Record<string, unknown>;

// inversePatch is the update_task input that puts back what `patch` changes on `t`.
export function inversePatch(t: Task, patch: TaskPatch): TaskPatch {
  const was: TaskPatch = {
    title: t.title, status: t.status, priority: t.priority, assignee: t.assignee?.id ?? "", due: t.due ?? "",
    labels: t.labels, repeat: t.repeat ?? "", project: t.project_id,
  };
  return Object.fromEntries(Object.keys(patch).filter((k) => k in was).map((k) => [k, was[k]]));
}
