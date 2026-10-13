import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, cn, type Member, type Task } from "../lib";
import { Avatar, Card, Empty, ErrorText, PageHeader } from "../components/ui";
import { TaskRow, useInvalidate } from "./tasks";

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
  const [over, setOver] = useState<string | null>(null);
  const move = useMutation({ mutationFn: async (v: { t: Task; to: string }) => {
      // A handoff ends the previous worker's claim so the new assignee can start.
      if (v.t.claimed_by && v.t.claimed_by.id !== v.to) await api.tool("release_task", { id: v.t.id });
      return api.tool("update_task", { id: v.t.id, assignee: v.to });
    }, onSettled: invalidate });
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
            <section
              key={c.id || "unassigned"}
              aria-label={label}
              onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = "move"; setOver(c.id); }}
              onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setOver(null); }}
              onDrop={(e) => {
                e.preventDefault();
                setOver(null);
                const t = byId.get(e.dataTransfer.getData("application/x-handoff-task"));
                if (t && (t.assignee?.id ?? unassigned) !== c.id) move.mutate({ t, to: c.id });
              }}
              className={cn("flex w-[82vw] max-w-xs shrink-0 snap-start flex-col gap-2 self-start rounded-2xl bg-zinc-100 p-2 transition-colors md:w-72", over === c.id && "bg-accent-soft ring-2 ring-accent")}
            >
              <h2 className="flex items-center gap-2 px-2 pt-1 text-sm font-semibold">
                {c.member && <Avatar m={c.member} />}
                <span className="truncate">{label}</span>
                <span className="ml-auto shrink-0 text-xs font-normal text-zinc-500">{c.tasks.length} open{working ? ` · ${working} working` : ""}</span>
              </h2>
              {c.tasks.map((t) => (
                <div
                  key={t.id}
                  draggable
                  onDragStart={(e) => { e.dataTransfer.setData("application/x-handoff-task", t.id); e.dataTransfer.effectAllowed = "move"; }}
                  className={cn("cursor-grab active:cursor-grabbing", move.isPending && move.variables?.t.id === t.id && "opacity-50")}
                >
                  <TaskRow t={t} />
                </div>
              ))}
              {c.tasks.length === 0 && <div className="px-2 py-6 text-center text-sm text-zinc-400">Nothing assigned</div>}
            </section>
          );
        })}
      </div>
    </>
  );
}
