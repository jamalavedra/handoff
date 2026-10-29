import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api, type Task } from "../lib";
import { isoDate } from "../quickadd";
import { markLocal, patchTask, undoable, undoClose } from "../components/feedback";

// cachedTasks finds tasks by id in any cached task list.
function cachedTasks(qc: QueryClient, ids: string[]) {
  const byId = new Map<string, Task>();
  for (const [, data] of qc.getQueriesData<{ tasks: Task[] } | Task[]>({ queryKey: ["tasks"] })) {
    for (const t of (Array.isArray(data) ? data : data?.tasks) ?? []) byId.set(t.id, t);
  }
  return ids.flatMap((id) => byId.get(id) ?? []);
}

type Op<R> = {
  label: (n: number) => string;
  skip?: (t: Task) => boolean;
  patch?: Partial<Task>;
  apply: (t: Task) => Promise<R>;
  undo: (t: Task, r: R) => Promise<unknown>;
};

const many = (n: number) => (n === 1 ? "1 task" : `${n} tasks`);

// bulk runs one single-task tool per task, all at once, then offers one Undo that inverts every
// change that went through. shortcut: one request per task; add a bulk tool if 50+ feels slow.
async function bulk<R>(qc: QueryClient, ids: string[], op: Op<R>) {
  const tasks = cachedTasks(qc, ids).filter((t) => !op.skip?.(t));
  if (!tasks.length) return;
  for (const t of tasks) {
    if (op.patch) patchTask(qc, t.id, op.patch);
    else markLocal(t.id);
  }
  const results = await Promise.allSettled(tasks.map(op.apply));
  await qc.invalidateQueries();
  const ok = tasks.flatMap((t, i) => { const r = results[i]; return r.status === "fulfilled" ? [{ t, r: r.value }] : []; });
  const failed = results.find((r) => r.status === "rejected");
  if (failed) toast.error(`${many(tasks.length - ok.length)} failed: ${failed.reason instanceof Error ? failed.reason.message : String(failed.reason)}`);
  if (ok.length) undoable(qc, op.label(ok.length), () => Promise.all(ok.map(({ t, r }) => { markLocal(t.id); return op.undo(t, r); })));
}

const update = (id: string, patch: Record<string, unknown>) => api.tool("update_task", { id, ...patch });

// useBulkActions changes several tasks at once (the ⌘K selection actions and drops onto a project).
export function useBulkActions() {
  const qc = useQueryClient();
  return {
    complete: (ids: string[]) => bulk(qc, ids, {
      label: (n) => `Completed ${many(n)}`,
      skip: (t) => t.status === "done" || t.status === "cancelled",
      patch: { status: "done" },
      apply: (t) => api.tool<{ next?: Task }>("close_task", { id: t.id, status: "done", today: isoDate(new Date()) }),
      undo: (t, r) => undoClose(t.id, r.next),
    }),
    due: (ids: string[], due: string) => bulk(qc, ids, {
      label: (n) => (due ? `Set the due date of ${many(n)}` : `Removed the due date of ${many(n)}`),
      patch: { due: due || undefined },
      apply: (t) => update(t.id, { due }),
      undo: (t) => update(t.id, { due: t.due ?? "" }),
    }),
    priority: (ids: string[], priority: number) => bulk(qc, ids, {
      label: (n) => `Set the priority of ${many(n)}`,
      patch: { priority },
      apply: (t) => update(t.id, { priority }),
      undo: (t) => update(t.id, { priority: t.priority }),
    }),
    assign: (ids: string[], assignee: string, name: string) => bulk(qc, ids, {
      label: (n) => (assignee ? `Assigned ${many(n)} to ${name}` : `Unassigned ${many(n)}`),
      skip: (t) => (t.assignee?.id ?? "") === assignee,
      apply: (t) => update(t.id, { assignee }),
      undo: (t) => update(t.id, { assignee: t.assignee?.id ?? "" }),
    }),
    move: (ids: string[], project: string, name: string) => bulk(qc, ids, {
      label: (n) => `Moved ${many(n)} to ${name}`,
      skip: (t) => t.project_id === project,
      apply: (t) => update(t.id, { project }),
      undo: (t) => update(t.id, { project: t.project_id }),
    }),
    trash: (ids: string[]) => bulk(qc, ids, {
      label: (n) => `Moved ${many(n)} to trash`,
      skip: (t) => !!t.deleted_at,
      apply: (t) => api.tool("trash_task", { id: t.id }),
      undo: (t) => api.tool("restore_task", { id: t.id }),
    }),
  };
}
