import { useState, type ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import { api, cn, type Task } from "../lib";
import { Card, Empty, ErrorText } from "../components/app";
import { boardColumn, isoDate, type BoardColumn } from "../quickadd";
import { TaskRow, useInvalidate } from "./tasks";
import { useTaskDrop } from "../hooks/use-selection";

export type View = "list" | "board";
const key = "handoff-view";

export function useView(): [View, (v: View) => void] {
  const [v, setV] = useState<View>(() => {
    try {
      return localStorage.getItem(key) === "board" ? "board" : "list";
    } catch {
      return "list";
    }
  });
  const set = (n: View) => {
    setV(n);
    try {
      localStorage.setItem(key, n);
    } catch {
      // private mode: keep it for this page only
    }
  };
  return [v, set];
}

const columns: { key: BoardColumn; title: string; empty: string }[] = [
  { key: "todo", title: "To do", empty: "Nothing to do" },
  { key: "working", title: "Working", empty: "Nobody is on anything" },
  { key: "done", title: "Done", empty: "Nothing done yet" },
];

async function moveTask(t: Task, to: BoardColumn) {
  if (to === "done") return api.tool("close_task", { id: t.id, status: "done", today: isoDate(new Date()) });
  if (to === "working") return api.tool("update_task", { id: t.id, status: "in_progress" });
  let status = t.status;
  if (t.claimed_by) {
    await api.tool("release_task", { id: t.id });
    if (status === "in_progress") status = "open"; // release reopens started tasks
  }
  if (status !== "open") await api.tool("update_task", { id: t.id, status: "open" });
}

// DropColumn is a board column that takes dragged task cards (Board and Team).
export function DropColumn({ label, onDrop, children }: { label: string; onDrop: (ids: string[]) => void; children: ReactNode }) {
  const drop = useTaskDrop(onDrop);
  return (
    <section
      aria-label={label}
      {...drop.props}
      className={cn("flex w-[82vw] max-w-xs shrink-0 snap-start flex-col gap-2 self-start rounded-2xl bg-accent p-2 transition-colors md:w-72", drop.over && "bg-accent ring-2 ring-ring")}
    >
      {children}
    </section>
  );
}

// DragCard is a row on a board. The row itself drags, carrying the selection when it is in one.
export function DragCard({ t, moving }: { t: Task; moving: boolean }) {
  return (
    <div className={cn("cursor-grab active:cursor-grabbing", moving && "opacity-50")}>
      <TaskRow t={t} />
    </div>
  );
}

// Board shows open tasks plus recently done ones in To do / Working / Done; drag a card (or a selection) to move it.
export function Board({ tasks, done }: { tasks?: Task[]; done?: Task[] }) {
  const invalidate = useInvalidate();
  const move = useMutation({ mutationFn: (v: { ts: Task[]; to: BoardColumn }) => Promise.all(v.ts.map((t) => moveTask(t, v.to))), onSettled: invalidate });
  if (!tasks) return <Card><Empty>Loading…</Empty></Card>;
  const all = [...tasks, ...(done ?? []).filter((d) => !tasks.some((t) => t.id === d.id))];
  const byId = new Map(all.map((t) => [t.id, t]));
  return (
    <>
      <ErrorText error={move.error} />
      <div className="-mx-4 flex snap-x snap-mandatory gap-3 overflow-x-auto px-4 pb-2 md:mx-0 md:px-0">
        {columns.map((c) => {
          const items = all.filter((t) => boardColumn(t) === c.key);
          return (
            <DropColumn key={c.key} label={c.title} onDrop={(ids) => { const ts = ids.flatMap((id) => byId.get(id) ?? []).filter((t) => boardColumn(t) !== c.key); if (ts.length) move.mutate({ ts, to: c.key }); }}>
              <h2 className="flex items-baseline gap-2 px-2 pt-1 text-[13px] font-medium text-muted-foreground">
                {c.title}<span className="font-normal text-muted-foreground/70">{items.length}</span>
              </h2>
              {items.map((t) => <DragCard key={t.id} t={t} moving={move.isPending && !!move.variables?.ts.includes(t)} />)}
              {items.length === 0 && <div className="px-2 py-6 text-center text-sm text-muted-foreground/70">{c.empty}</div>}
            </DropColumn>
          );
        })}
      </div>
    </>
  );
}
