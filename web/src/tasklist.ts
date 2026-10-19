// How a task list is shown: its order, what's left out, and whether it's split by due date.
// Pure helpers (no imports, so node's test runner loads them as they are); the toolbar in
// components/task-toolbar.tsx edits these.

const isoDate = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

// manual is the list's own order: priority, then due date, as the server and agents rank work.
export type Sort = "manual" | "title" | "status" | "due" | "created" | "updated";
export type DueFilter = "overdue" | "today" | "week" | "none";
export type Filter = { status: ("open" | "in_progress")[]; priority: number[]; due: DueFilter[] };
export type Rows = "compact" | "comfortable";
// What a row shows besides its title; each can be hidden.
export type Prop = "due" | "assignee" | "todos" | "repeat" | "note";
export type ListPrefs = { sort: Sort; group: boolean; showDone: boolean; rows: Rows; hidden: Prop[]; filter: Filter };

export const sortLabel: Record<Sort, string> = { manual: "Manual", title: "Title", status: "Status", due: "Due date", created: "Date added", updated: "Last edited" };
export const dueFilterLabel: Record<DueFilter, string> = { overdue: "Overdue", today: "Today", week: "Next 7 days", none: "No date" };
export const statusFilterLabel: Record<Filter["status"][number], string> = { open: "Todo", in_progress: "In progress" };
export const rowsLabel: Record<Rows, string> = { compact: "Compact", comfortable: "Comfortable" };
export const propLabel: Record<Prop, string> = { due: "Due date", assignee: "Assignee", todos: "Todo progress", repeat: "Repeat", note: "Agent status" };

export const emptyFilter: Filter = { status: [], priority: [], due: [] };
export const defaultPrefs: ListPrefs = { sort: "manual", group: false, showDone: false, rows: "comfortable", hidden: [], filter: emptyFilter };

// activeFilters counts the filter options picked, for the Filter button's badge.
export const activeFilters = (f: Filter) => f.status.length + f.priority.length + f.due.length;

type Sortable = { title: string; status: string; priority: number; due?: string; created_at: string; updated_at: string };
const statusRank: Record<string, number> = { in_progress: 0, open: 1, done: 2, cancelled: 3 };

// sortTasks orders a copy of the list. Ties keep the input order (the server's: priority, then due).
export function sortTasks<T extends Sortable>(tasks: T[], sort: Sort): T[] {
  if (sort === "manual") return [...tasks];
  const byDue = (a: T, b: T) => (a.due ? (b.due ? a.due.localeCompare(b.due) : -1) : b.due ? 1 : 0);
  const cmp: Record<Exclude<Sort, "manual">, (a: T, b: T) => number> = {
    title: (a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: "base", numeric: true }),
    status: (a, b) => (statusRank[a.status] ?? 9) - (statusRank[b.status] ?? 9),
    due: (a, b) => byDue(a, b) || a.priority - b.priority,
    created: (a, b) => b.created_at.localeCompare(a.created_at),
    updated: (a, b) => b.updated_at.localeCompare(a.updated_at),
  };
  return [...tasks].sort(cmp[sort]);
}

type Filterable = { title: string; status: string; priority: number; due?: string };

// filterTasks keeps the tasks matching every group that has a pick; within a group, any pick matches.
// Closed tasks pass the status filter (they're shown only when asked for, as done). query matches
// words of the title, in any order.
export function filterTasks<T extends Filterable>(tasks: T[], f: Filter, now: Date = new Date(), query = ""): T[] {
  const today = isoDate(now);
  const week = isoDate(new Date(now.getFullYear(), now.getMonth(), now.getDate() + 7));
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const dueIs = (d: DueFilter, due?: string) =>
    d === "none" ? !due : !!due && (d === "overdue" ? due < today : d === "today" ? due === today : due >= today && due <= week);
  return tasks.filter((t) =>
    (!f.status.length || t.status === "done" || t.status === "cancelled" || f.status.includes(t.status as "open")) &&
    (!f.priority.length || f.priority.includes(t.priority)) &&
    (!f.due.length || f.due.some((d) => dueIs(d, t.due))) &&
    (!words.length || words.every((w) => t.title.toLowerCase().includes(w))),
  );
}

export type Section<T> = { key: string; title: string; tasks: T[] };

// arrange filters, sorts and (if asked) splits the list into due-date sections. Ungrouped, it's one
// untitled section. Grouped, sections follow the due date and each keeps the chosen order.
export function arrange<T extends Sortable & Filterable>(tasks: T[], p: Pick<ListPrefs, "sort" | "group" | "filter">, now: Date = new Date(), query = ""): Section<T>[] {
  const shown = sortTasks(filterTasks(tasks, p.filter, now, query), p.sort);
  if (!p.group) return shown.length ? [{ key: "all", title: "", tasks: shown }] : [];
  const today = isoDate(now);
  const sections: Section<T>[] = [
    { key: "overdue", title: "Overdue", tasks: [] },
    { key: "today", title: "Today", tasks: [] },
    { key: "upcoming", title: "Upcoming", tasks: [] },
    { key: "none", title: "No date", tasks: [] },
  ];
  for (const t of shown) sections[!t.due ? 3 : t.due < today ? 0 : t.due === today ? 1 : 2].tasks.push(t);
  return sections.filter((s) => s.tasks.length > 0);
}

// readPrefs takes saved preferences, keeping only what's still valid.
export function readPrefs(raw: string | null): ListPrefs {
  try {
    const v = JSON.parse(raw ?? "{}") as Partial<ListPrefs>;
    const f = (v.filter ?? {}) as Partial<Filter>;
    const pick = <X>(xs: unknown, ok: (x: unknown) => boolean) => (Array.isArray(xs) ? (xs.filter(ok) as X[]) : []);
    return {
      sort: v.sort && v.sort in sortLabel ? v.sort : defaultPrefs.sort,
      group: v.group === true,
      showDone: v.showDone === true,
      rows: v.rows === "compact" ? "compact" : "comfortable",
      hidden: pick(v.hidden, (x) => typeof x === "string" && x in propLabel),
      filter: {
        status: pick(f.status, (x) => x === "open" || x === "in_progress"),
        priority: pick(f.priority, (x) => typeof x === "number" && x >= 0 && x <= 4),
        due: pick(f.due, (x) => typeof x === "string" && x in dueFilterLabel),
      },
    };
  } catch {
    return defaultPrefs;
  }
}
