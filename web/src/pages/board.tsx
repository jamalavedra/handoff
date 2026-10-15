import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Columns3, List } from "lucide-react";
import { api, cn, type Task } from "../lib";
import { Card, Empty, ErrorText } from "../components/app";
import { boardColumn, isoDate, type BoardColumn } from "../quickadd";
import { TaskRow, useInvalidate } from "./tasks";

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

export function ViewToggle({ view, onChange }: { view: View; onChange: (v: View) => void }) {
  const opts = [
    { v: "list" as const, icon: List, label: "List" },
    { v: "board" as const, icon: Columns3, label: "Board" },
  ];
  return (
    <div role="radiogroup" aria-label="View" className="inline-flex rounded-full bg-accent p-0.5">
      {opts.map(({ v, icon: Icon, label }) => (
        <button key={v} role="radio" aria-checked={view === v} aria-label={label} title={label} onClick={() => onChange(v)}
          className={cn("flex size-7 items-center justify-center rounded-full text-muted-foreground transition-colors", view === v && "bg-card text-foreground shadow-sm")}>
          <Icon className="size-3.5" />
        </button>
      ))}
    </div>
  );
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

// Board shows open tasks plus recently done ones in To do / Working / Done; drag a card to move it.
export function Board({ tasks, done }: { tasks?: Task[]; done?: Task[] }) {
  const invalidate = useInvalidate();
  const [over, setOver] = useState<BoardColumn | null>(null);
  const move = useMutation({ mutationFn: (v: { t: Task; to: BoardColumn }) => moveTask(v.t, v.to), onSettled: invalidate });
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
            <section
              key={c.key}
              aria-label={c.title}
              onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = "move"; setOver(c.key); }}
              onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setOver(null); }}
              onDrop={(e) => {
                e.preventDefault();
                setOver(null);
                const t = byId.get(e.dataTransfer.getData("application/x-handoff-task"));
                if (t && boardColumn(t) !== c.key) move.mutate({ t, to: c.key });
              }}
              className={cn("flex w-[82vw] max-w-xs shrink-0 snap-start flex-col gap-2 self-start rounded-2xl bg-accent p-2 transition-colors md:w-72", over === c.key && "bg-accent ring-2 ring-ring")}
            >
              <h2 className="flex items-baseline gap-2 px-2 pt-1 text-[13px] font-medium text-muted-foreground">
                {c.title}<span className="font-normal text-muted-foreground/70">{items.length}</span>
              </h2>
              {items.map((t) => (
                <div
                  key={t.id}
                  draggable
                  onDragStart={(e) => { e.dataTransfer.setData("application/x-handoff-task", t.id); e.dataTransfer.effectAllowed = "move"; }}
                  className={cn("cursor-grab active:cursor-grabbing", move.isPending && move.variables?.t.id === t.id && "opacity-50")}
                >
                  <TaskRow t={t} />
                </div>
              ))}
              {items.length === 0 && <div className="px-2 py-6 text-center text-sm text-muted-foreground/70">{c.empty}</div>}
            </section>
          );
        })}
      </div>
    </>
  );
}
