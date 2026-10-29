import { useEffect, useRef, useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";
import { api, type Task } from "../lib";
import { createHistory } from "../undo";
import { typing } from "./shortcuts";
import { dueLabel, isoDate } from "../quickadd";
import { useInbox } from "../pages/notifications";
import { chatInView } from "../agent-activity";

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

type Step = { label: string; undo: () => Promise<unknown>; redo?: () => Promise<unknown> };
const history = createHistory<Step>(50, (s) => !!s.redo);

// remember puts a change on this tab's ⌘Z history. Without `redo`, ⇧⌘Z can't bring it back.
export function remember(label: string, undo: () => Promise<unknown>, redo?: () => Promise<unknown>) {
  const step = { label, undo, redo };
  history.push(step);
  return step;
}

const run = (qc: QueryClient, fn: () => Promise<unknown>, done?: string) =>
  fn().then(() => { qc.invalidateQueries(); if (done) toast(done); }, (e: unknown) => toast.error(e instanceof Error ? e.message : String(e)));

// undoable shows a toast whose Undo button runs `undo` and refreshes everything; ⌘Z does the same.
export function undoable(qc: QueryClient, message: string, undo: () => Promise<unknown>, redo?: () => Promise<unknown>) {
  const step = remember(message, undo, redo);
  toast(message, {
    action: {
      label: "Undo",
      onClick: () => {
        if (history.undo(step)) run(qc, undo);
      },
    },
  });
}

// saveTask applies an update_task patch, through close_task when it closes the task.
export async function saveTask(id: string, patch: Record<string, unknown>) {
  const { status, ...rest } = patch;
  if (status === "done" || status === "cancelled") {
    if (Object.keys(rest).length) await api.tool("update_task", { id, ...rest });
    return api.tool("close_task", { id, status });
  }
  return api.tool("update_task", { id, ...patch });
}

// useUndoKeys wires ⌘Z / ⇧⌘Z (Ctrl elsewhere) to this tab's history. Fields, the rich editor and
// dialogs keep the keys for themselves.
export function useUndoKeys() {
  const qc = useQueryClient();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() !== "z" || !(e.metaKey || e.ctrlKey) || e.altKey || e.defaultPrevented || typing(e.target)) return;
      if (document.querySelector("[role=dialog], [role=menu]")) return;
      e.preventDefault();
      if (!e.shiftKey) {
        const s = history.undo();
        return void (s ? run(qc, s.undo, `Undid: ${s.label}`) : toast("Nothing to undo"));
      }
      const s = history.redo();
      if (s?.redo) run(qc, s.redo, `Redid: ${s.label}`);
      else toast("Nothing to redo");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [qc]);
}

export const reopen = (id: string) => api.tool("update_task", { id, status: "open" });

// Completing a repeating task makes the next one; the toast says when, and Undo takes both back.
export function completedText(base: string, next?: Task) {
  return next?.due ? `${base} · next ${dueLabel(next.due).text}` : base;
}

// undoClose reverts a close (close_task's event_id): the server reopens the task and trashes the next
// occurrence if nobody has touched it, and says when it left it.
async function undoClose(eventId: number) {
  const r = await api.tool<{ message?: string }>("revert_change", { event_id: eventId });
  if (r.message) toast(r.message);
}

// undoableClose offers Undo for a close; redo closes again, and a later undo reverts that close.
export function undoableClose(qc: QueryClient, message: string, id: string, status: string, eventId: number) {
  let ev = eventId;
  undoable(qc, message, () => undoClose(ev), async () => {
    ev = (await api.tool<{ event_id: number }>("close_task", { id, status, today: isoDate(new Date()) })).event_id;
  });
}

const kindText = { mention: "mentioned you", assigned: "assigned you a task", approval: "needs your approval", agent_done: "finished" } as const;

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
      // The agent's reply in a chat you have open needs no toast. Otherwise it gets one even if opening
      // the task already marked it read: the reply sits in a chat that isn't showing.
      if (n.kind === "agent_done" ? chatInView(n.task_id) : n.read) continue;
      const open = () => {
        api.tool("mark_read", { ids: [n.id] }).then(() => qc.invalidateQueries({ queryKey: ["inbox"] }));
        // Approvals are decided on the task, where the request is shown, if the task can still be opened;
        // otherwise (say its project was archived) on the Agents page, which lists every pending request.
        const approvals = qc.getQueryData<{ approvals: { task?: { id: string } }[] }>(["approvals"])?.approvals;
        if (n.kind === "approval" && !approvals?.some((a) => a.task?.id === n.task_id)) navigate({ to: "/settings/agents" });
        else navigate({ to: "/tasks/$id", params: { id: n.task_id } });
      };
      const title = n.kind === "agent_done" ? `${n.actor?.name ?? "The agent"} finished “${n.task_title}”` : `${n.actor?.name ?? "Someone"} ${kindText[n.kind]}`;
      toast(title, {
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
