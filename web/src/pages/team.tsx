import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { undoable } from "../components/feedback";
import { toast } from "sonner";
import { api, type Member, type Task } from "../lib";
import { Avatar, Card, Empty, ErrorText, PageHeader } from "../components/app";
import { useInvalidate } from "./tasks";
import { DragCard, DropColumn } from "./board";

const unassigned = "";

async function allOpenTasks() {
  const page = 500;
  const tasks: Task[] = [];
  for (;;) {
    const r = await api.tool<{ tasks: Task[] }>("search_tasks", { limit: page, offset: tasks.length });
    tasks.push(...r.tasks);
    if (r.tasks.length < page) return tasks;
  }
}

// TeamPage shows everyone's open work side by side, busiest first; drag a card to hand it to someone else.
export function TeamPage() {
  const tasks = useQuery({ queryKey: ["tasks", "team"], queryFn: allOpenTasks });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const invalidate = useInvalidate();
  const qc = useQueryClient();
  const move = useMutation({ mutationFn: async (v: { t: Task; to: string }) => {
      // A handoff ends the previous worker's claim so the new assignee can start.
      if (v.t.claimed_by && v.t.claimed_by.id !== v.to) await api.tool("release_task", { id: v.t.id });
      return api.tool("update_task", { id: v.t.id, assignee: v.to });
    },
    onSuccess: (_r, v) => {
      const to = members.data?.members.find((m) => m.id === v.to)?.name ?? "Unassigned";
      // A released claim can't be given back (claims are taken by the worker), so only plain moves offer Undo.
      if (v.t.claimed_by && v.t.claimed_by.id !== v.to) toast(`Handed “${v.t.title}” to ${to}`, { description: `${v.t.claimed_by.name} stopped working on it.` });
      else undoable(qc, `Handed “${v.t.title}” to ${to}`, () => api.tool("update_task", { id: v.t.id, assignee: v.t.assignee?.id ?? "" }));
    },
    onSettled: invalidate });
  const open = tasks.data?.filter((t) => t.status !== "done" && t.status !== "cancelled");
  if (!open || !members.data) {
    return (
      <>
        <PageHeader title="Team" />
        <ErrorText error={tasks.error || members.error} />
        <Card><Empty>Loading…</Empty></Card>
      </>
    );
  }
  const people = members.data.members.filter((m) => m.status !== "removed" && m.status !== "revoked");
  const active = new Set(people.map((m) => m.id));
  const byMember = new Map<string, Task[]>();
  for (const t of open) {
    // Work of removed members shows under Unassigned so it can be handed on.
    const k = t.assignee && active.has(t.assignee.id) ? t.assignee.id : unassigned;
    byMember.set(k, [...(byMember.get(k) ?? []), t]);
  }
  const cols = [
    ...people
      .map((m) => ({ id: m.id, member: m as Member | undefined, tasks: byMember.get(m.id) ?? [] }))
      .sort((a, b) => b.tasks.length - a.tasks.length || a.member!.name.localeCompare(b.member!.name)),
    { id: unassigned, member: undefined, tasks: byMember.get(unassigned) ?? [] },
  ];
  const byId = new Map(open.map((t) => [t.id, t]));
  return (
    <>
      <PageHeader title="Team" />
      <ErrorText error={move.error} />
      <div className="-mx-4 flex snap-x snap-mandatory gap-3 overflow-x-auto px-4 pb-2 md:mx-0 md:px-0">
        {cols.map((c) => {
          const working = c.tasks.filter((t) => t.claimed_by).length;
          const label = c.member?.name ?? "Unassigned";
          return (
            <DropColumn key={c.id || "unassigned"} label={label} onDrop={(id) => { const t = byId.get(id); if (t && (t.assignee?.id ?? unassigned) !== c.id) move.mutate({ t, to: c.id }); }}>
              <h2 className="flex items-center gap-2 px-2 pt-1 text-sm font-semibold">
                {c.member && <Avatar m={c.member} />}
                <span className="truncate">{label}</span>
                <span className="ml-auto shrink-0 text-xs font-normal text-muted-foreground">{c.tasks.length} open{working ? ` · ${working} working` : ""}</span>
              </h2>
              {c.tasks.map((t) => <DragCard key={t.id} t={t} moving={move.isPending && move.variables?.t.id === t.id} />)}
              {c.tasks.length === 0 && <div className="px-2 py-6 text-center text-sm text-muted-foreground/70">Nothing assigned</div>}
            </DropColumn>
          );
        })}
      </div>
    </>
  );
}
