import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, cn, type Member, type Task } from "../lib";
import { mentionItems, type MentionItem } from "../mention";
import { Avatar, StatusCircle } from "./app";

export type Item = MentionItem<Member, Task>;

// useMentionItems is what the @ menu offers for the query being typed (null when it's closed):
// matching members, then open tasks from search_tasks, most recently changed first.
export function useMentionItems(query: string | null, members: Member[], exclude?: string): Item[] {
  const q = query?.trim() ?? "";
  const [debounced, setDebounced] = useState(q);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q), 150);
    return () => clearTimeout(t);
  }, [q]);
  const tasks = useQuery({
    queryKey: ["tasks", "mention", debounced],
    queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { query: debounced, sort: "recent", limit: 6 }),
    enabled: query !== null,
    staleTime: 30_000,
  });
  if (query === null) return [];
  // Tasks only show for the query being typed: none while it's still settling or loading.
  const found = debounced === q ? tasks.data?.tasks ?? [] : [];
  return mentionItems(members, found, q, exclude);
}

// MentionMenu is the popover above the composer: people and agents, then tasks. It never takes
// focus; the composer drives it from the keyboard and passes the highlighted index.
export function MentionMenu({ items, active, onPick, onHover }: { items: Item[]; active: number; onPick: (item: Item) => void; onHover: (i: number) => void }) {
  const listRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [active]);
  if (!items.length) return null;
  const firstTask = items.findIndex((x) => x.kind === "task");
  const row = (item: Item, i: number) => (
    <div
      key={item.kind === "member" ? `m${item.member.id}` : `t${item.task.id}`}
      id={`mention-${i}`}
      data-index={i}
      role="option"
      aria-selected={i === active}
      onMouseDown={(e) => { e.preventDefault(); onPick(item); }}
      onMouseMove={() => i !== active && onHover(i)}
      className={cn("mx-1 flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-sm", i === active && "bg-accent")}
    >
      {item.kind === "member" ? (
        <>
          <Avatar m={{ id: item.member.id, name: item.member.name, kind: item.member.kind }} className="size-5" />
          <span className="min-w-0 flex-1 truncate">{item.member.name}</span>
          {item.member.kind === "agent" && <span className="shrink-0 text-xs text-muted-foreground">Agent</span>}
        </>
      ) : (
        <>
          <span className="flex size-5 shrink-0 items-center justify-center"><StatusCircle t={item.task} small /></span>
          <span className="min-w-0 flex-1 truncate">{item.task.title}</span>
          {item.task.project && <span className="max-w-24 shrink-0 truncate text-xs text-muted-foreground">{item.task.project}</span>}
        </>
      )}
    </div>
  );
  const header = (label: string) => <div role="presentation" className="px-3 pt-2 pb-1 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">{label}</div>;
  const members = firstTask < 0 ? items : items.slice(0, firstTask);
  const tasks = firstTask < 0 ? [] : items.slice(firstTask);
  return (
    <div ref={listRef} id="mention-menu" role="listbox" aria-label="Mention a person, agent or task" className="absolute bottom-full left-0 mb-2 max-h-80 w-80 max-w-[calc(100%-0.5rem)] overflow-y-auto rounded-xl border border-border bg-card pb-1 shadow-lg">
      {members.length > 0 && (
        <div role="group" aria-label="People & agents">
          {header("People & agents")}
          {members.map((x, i) => row(x, i))}
        </div>
      )}
      {tasks.length > 0 && (
        <div role="group" aria-label="Tasks">
          {header("Tasks")}
          {tasks.map((x, i) => row(x, firstTask + i))}
        </div>
      )}
    </div>
  );
}
