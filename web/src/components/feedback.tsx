import { useEffect, useRef, useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { toast } from "sonner";
import { api, type Task } from "../lib";
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

// patchTask applies a change to every cached task list and detail at once, returning a rollback.
export function patchTask(qc: QueryClient, id: string, patch: Partial<Task>) {
  markLocal(id);
  const lists = qc.getQueriesData<{ tasks: Task[] }>({ queryKey: ["tasks"] });
  for (const [key, data] of lists) {
    if (data?.tasks) qc.setQueryData(key, { ...data, tasks: data.tasks.map((t) => (t.id === id ? { ...t, ...patch } : t)) });
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
        if (n.kind === "approval") navigate({ to: "/agents" });
        else navigate({ to: "/tasks/$id", params: { id: n.task_id } });
      };
      toast(`${n.actor?.name ?? "Someone"} ${kindText[n.kind]}`, {
        description: n.excerpt || n.task_title,
        action: { label: n.kind === "approval" ? "Review" : "Open", onClick: open },
      });
    }
  }, [inbox.data, navigate, qc]);
}

// useChangeFlash is true for a moment after someone else (a person or an agent) edits the task.
export function useChangeFlash(id: string, updatedAt: string) {
  const prev = useRef(updatedAt);
  const [flash, setFlash] = useState(false);
  useEffect(() => {
    if (prev.current === updatedAt) return;
    prev.current = updatedAt;
    if (changedHere(id)) return;
    setFlash(true);
    const t = setTimeout(() => setFlash(false), 1900);
    return () => clearTimeout(t);
  }, [id, updatedAt]);
  return flash;
}
