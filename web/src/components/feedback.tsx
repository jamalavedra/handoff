import { useEffect, useRef, useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";
import { api, type Task } from "../lib";
import { dueLabel } from "../quickadd";
import { useInbox } from "../pages/notifications";

// Tasks this tab changed in the last few seconds. Rows flash only for changes made elsewhere.
const local = new Map<string, number>();

export function markLocal(id: string) {
  local.set(id, Date.now());
}

export function changedHere(id: string) {
  const at = local.get(id);
  return at !== undefined && Date.now() - at < 5000;
}

// patchTask applies a change to every cached task list at once, returning a rollback.
// Lists are cached either as { tasks: Task[] } or, on the Team board, as a bare Task[].
export function patchTask(qc: QueryClient, id: string, patch: Partial<Task>) {
  markLocal(id);
  const lists = qc.getQueriesData<{ tasks: Task[] } | Task[]>({ queryKey: ["tasks"] });
  const apply = (ts: Task[]) => ts.map((t) => (t.id === id ? { ...t, ...patch } : t));
  for (const [key, data] of lists) {
    if (Array.isArray(data)) qc.setQueryData(key, apply(data));
    else if (data?.tasks) qc.setQueryData(key, { ...data, tasks: apply(data.tasks) });
  }
  return () => lists.forEach(([key, data]) => qc.setQueryData(key, data));
}

// undoable shows a toast whose Undo button runs `undo` and refreshes everything.
export function undoable(qc: QueryClient, message: string, undo: () => Promise<unknown>) {
  toast(message, {
    action: {
      label: "Undo",
      onClick: () => {
        undo().then(() => qc.invalidateQueries(), (e: unknown) => toast.error(e instanceof Error ? e.message : String(e)));
      },
    },
  });
}

export const reopen = (id: string) => api.tool("update_task", { id, status: "open" });

// Completing a repeating task makes the next one; the toast says when, and Undo takes both back.
export function completedText(base: string, next?: Task) {
  return next?.due ? `${base} · next ${dueLabel(next.due).text}` : base;
}

export async function undoClose(id: string, next?: Task) {
  await reopen(id);
  if (next) await api.tool("trash_task", { id: next.id });
}

const kindText = { mention: "mentioned you", assigned: "assigned you a task", approval: "needs your approval" } as const;

// useNotificationToasts pops a toast for each inbox item that arrives while the app is open.
export function useNotificationToasts() {
  const inbox = useInbox();
  const navigate = useNavigate();
  const seen = useRef<Set<number> | null>(null);
  const qc = useQueryClient();
  useEffect(() => {
    const items = inbox.data?.notifications;
    if (!items) return;
    if (!seen.current) {
      // First load: everything already there is old news.
      seen.current = new Set(items.map((n) => n.id));
      return;
    }
    for (const n of items) {
      if (seen.current.has(n.id)) continue;
      seen.current.add(n.id);
      if (n.read) continue;
      const open = () => {
        api.tool("mark_read", { ids: [n.id] }).then(() => qc.invalidateQueries({ queryKey: ["inbox"] }));
        // Approvals are decided on the task, where the request is shown, if the task can still be opened;
        // otherwise (say its project was archived) on the Agents page, which lists every pending request.
        const approvals = qc.getQueryData<{ approvals: { task?: { id: string } }[] }>(["approvals"])?.approvals;
        if (n.kind === "approval" && !approvals?.some((a) => a.task?.id === n.task_id)) navigate({ to: "/settings/agents" });
        else navigate({ to: "/tasks/$id", params: { id: n.task_id } });
      };
      toast(`${n.actor?.name ?? "Someone"} ${kindText[n.kind]}`, {
        description: n.excerpt || n.task_title,
        action: { label: n.kind === "approval" ? "Review" : "Open", onClick: open },
      });
    }
  }, [inbox.data, navigate, qc]);
}

// useTriageReveal is true for a moment after triage finishes, so the tidied title can blur in.
export function useTriageReveal(triage: Task["triage"]) {
  const prev = useRef(triage);
  const [reveal, setReveal] = useState(false);
  useEffect(() => {
    const was = prev.current;
    prev.current = triage;
    if (was !== "pending" || triage === "pending") return;
    setReveal(true);
    const t = setTimeout(() => setReveal(false), 700);
    return () => clearTimeout(t);
  }, [triage]);
  return reveal;
}

// useChangeFlash is true for a moment after someone else (a person or an agent) edits the task.
export function useChangeFlash(id: string, updatedAt: string) {
  const prev = useRef(updatedAt);
  const [flash, setFlash] = useState(false);
  useEffect(() => {
    if (prev.current === updatedAt) return;
    prev.current = updatedAt;
    // Our own edit: no flash, and end any flash still running (its timer was just cleared).
    if (changedHere(id)) return setFlash(false);
    setFlash(true);
    const t = setTimeout(() => setFlash(false), 1900);
    return () => clearTimeout(t);
  }, [id, updatedAt]);
  return flash;
}
