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
  const move = useMutation({ mutationFn: (v: { ts: Task[]; to: string }) => Promise.all(v.ts.map(async (t) => {
      // A handoff ends the previous worker's claim so the new assignee can start.
      if (t.claimed_by && t.claimed_by.id !== v.to) await api.tool("release_task", { id: t.id });
      return api.tool("update_task", { id: t.id, assignee: v.to });
    })),
    onSuccess: (_r, v) => {
      const to = members.data?.members.find((m) => m.id === v.to)?.name ?? "Unassigned";
      const what = v.ts.length === 1 ? `“${v.ts[0].title}”` : `${v.ts.length} tasks`;
      const released = v.ts.filter((t) => t.claimed_by && t.claimed_by.id !== v.to);
      // A released claim can't be given back (claims are taken by the worker), so only plain moves offer Undo.
      if (released.length) toast(`Handed ${what} to ${to}`, { description: released.map((t) => `${t.claimed_by!.name} stopped working on “${t.title}”.`).join(" ") });
      else undoable(qc, `Handed ${what} to ${to}`, () => Promise.all(v.ts.map((t) => api.tool("update_task", { id: t.id, assignee: t.assignee?.id ?? "" }))));
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
            <DropColumn key={c.id || "unassigned"} label={label} onDrop={(ids) => { const ts = ids.flatMap((id) => byId.get(id) ?? []).filter((t) => (t.assignee?.id ?? unassigned) !== c.id); if (ts.length) move.mutate({ ts, to: c.id }); }}>
              <h2 className="flex items-center gap-2 px-2 pt-1 text-sm font-semibold">
                {c.member && <Avatar m={c.member} />}
                <span className="truncate">{label}</span>
                <span className="ml-auto shrink-0 text-xs font-normal text-muted-foreground">{c.tasks.length} open{working ? ` · ${working} working` : ""}</span>
              </h2>
              {c.tasks.map((t) => <DragCard key={t.id} t={t} moving={move.isPending && !!move.variables?.ts.includes(t)} />)}
              {c.tasks.length === 0 && <div className="px-2 py-6 text-center text-sm text-muted-foreground/70">Nothing assigned</div>}
            </DropColumn>
          );
        })}
      </div>
    </>
  );
}
