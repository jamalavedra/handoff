import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { ArrowUpDown, Calendar, CalendarCheck, Circle, Clock, Columns3, Eye, EyeOff, Flag, Grip, Link2, List, ListFilter, Pencil, Plus, Rows3, Search, SlidersHorizontal, SquarePlus, ToggleRight, Type, X } from "lucide-react";
import { cn } from "../lib";
import { activeFilters, defaultPrefs, dueFilterLabel, emptyFilter, propLabel, readPrefs, rowsLabel, sortLabel, statusFilterLabel, type DueFilter, type Filter, type ListPrefs, type Prop, type Rows, type Sort } from "../tasklist";
import type { View } from "../pages/board";
import { DropdownMenu, DropdownMenuCheckItem, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuSub, DropdownMenuSubContent, DropdownMenuSubTrigger, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { useTabs } from "./tabs";
import { useQuickAdd } from "./quickadd";

// How task lists are shown is a personal choice, kept in this browser for every list.
const key = "handoff-task-list";

export function useListPrefs(): [ListPrefs, (patch: Partial<ListPrefs>) => void] {
  const [p, setP] = useState<ListPrefs>(() => { try { return readPrefs(localStorage.getItem(key)); } catch { return defaultPrefs; } });
  const set = (patch: Partial<ListPrefs>) => setP((cur) => {
    const next = { ...cur, ...patch };
    try { localStorage.setItem(key, JSON.stringify(next)); } catch { /* private mode: this page only */ }
    return next;
  });
  return [p, set];
}

const priorityText = ["Urgent", "High", "Normal", "Low", "Someday"];
const toggle = <X,>(xs: X[], x: X) => (xs.includes(x) ? xs.filter((y) => y !== x) : [...xs, x]);

const sortIcon: Record<Sort, typeof Grip> = { manual: Grip, title: Type, status: Circle, due: CalendarCheck, created: Calendar, updated: Clock };

const iconButton = "relative flex size-9 items-center justify-center rounded-full text-foreground/70 outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/40 data-[state=open]:bg-accent data-[state=open]:text-foreground [&_svg]:size-[18px]";

function Dot() {
  return <span aria-hidden className="absolute top-1.5 right-1.5 size-1.5 rounded-full bg-foreground" />;
}

// The sort choices, shared by the Sort button and Settings → Sort.
function SortItems({ prefs, onChange }: { prefs: ListPrefs; onChange: (p: Partial<ListPrefs>) => void }) {
  return (
    <>
      {(Object.keys(sortLabel) as Sort[]).map((s) => {
        const Icon = sortIcon[s];
        return <DropdownMenuCheckItem key={s} checked={prefs.sort === s} onSelect={() => onChange({ sort: s })}><Icon />{sortLabel[s]}</DropdownMenuCheckItem>;
      })}
    </>
  );
}

// The filters, shared by the Filter button and Settings → Filter: each kind opens its values to the
// side, and picking one keeps the menu open to pick more.
function FilterItems({ prefs, onChange }: { prefs: ListPrefs; onChange: (p: Partial<ListPrefs>) => void }) {
  const f = prefs.filter;
  const set = (patch: Partial<Filter>) => onChange({ filter: { ...f, ...patch } });
  const count = (n: number) => (n ? <span className="tabular-nums">{n}</span> : undefined);
  return (
    <>
      <DropdownMenuSub>
        <DropdownMenuSubTrigger value={count(f.status.length)}><Circle />Status</DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          {(["open", "in_progress"] as const).map((s) => <DropdownMenuCheckItem key={s} keepOpen checked={f.status.includes(s)} onSelect={() => set({ status: toggle(f.status, s) })}>{statusFilterLabel[s]}</DropdownMenuCheckItem>)}
        </DropdownMenuSubContent>
      </DropdownMenuSub>
      <DropdownMenuSub>
        <DropdownMenuSubTrigger value={count(f.due.length)}><CalendarCheck />Due date</DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          {(Object.keys(dueFilterLabel) as DueFilter[]).map((d) => <DropdownMenuCheckItem key={d} keepOpen checked={f.due.includes(d)} onSelect={() => set({ due: toggle(f.due, d) })}>{dueFilterLabel[d]}</DropdownMenuCheckItem>)}
        </DropdownMenuSubContent>
      </DropdownMenuSub>
      <DropdownMenuSub>
        <DropdownMenuSubTrigger value={count(f.priority.length)}><Flag />Priority</DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          {priorityText.map((label, i) => <DropdownMenuCheckItem key={i} keepOpen checked={f.priority.includes(i)} onSelect={() => set({ priority: toggle(f.priority, i) })}>{label}</DropdownMenuCheckItem>)}
        </DropdownMenuSubContent>
      </DropdownMenuSub>
      {activeFilters(f) > 0 && (
        <>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => onChange({ filter: emptyFilter })}><X />Clear filters</DropdownMenuItem>
        </>
      )}
    </>
  );
}

// TaskToolbar is the top right of a task list: search, Sort, Filter, View settings, Copy link and
// New. Pass view/onView where the list can also be a board, and onEdit where the list (a project)
// can be renamed.
export function TaskToolbar({ prefs, onChange, query, onQuery, view, onView, link, onEdit }: {
  prefs: ListPrefs;
  onChange: (p: Partial<ListPrefs>) => void;
  query: string;
  onQuery: (q: string) => void;
  view?: View;
  onView?: (v: View) => void;
  link: string; // this list's address, for Copy link and Open in new tab
  onEdit?: () => void;
}) {
  const board = view === "board";
  const n = activeFilters(prefs.filter);
  const tabs = useTabs();
  const quick = useQuickAdd();
  const [searching, setSearching] = useState(!!query);
  const search = useRef<HTMLInputElement>(null);
  useEffect(() => { if (searching) search.current?.focus(); }, [searching]);
  const copyLink = () => {
    const url = `${window.location.origin}${link}`;
    navigator.clipboard?.writeText(url).then(() => toast("Link copied", { description: url }), () => toast.error("Couldn't copy the link"));
  };
  const toggleProp = (p: Prop) => onChange({ hidden: toggle(prefs.hidden, p) });

  return (
    <div className="flex items-center gap-0.5">
      {searching ? (
        <div className="mr-1 flex h-9 items-center gap-1.5 rounded-full bg-accent pr-1 pl-3 motion-safe:animate-in motion-safe:fade-in-0">
          <Search className="size-4 shrink-0 text-muted-foreground" />
          <input
            ref={search}
            value={query}
            onChange={(e) => onQuery(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); onQuery(""); setSearching(false); } }}
            onBlur={() => { if (!query) setSearching(false); }}
            placeholder="Search tasks"
            aria-label="Search tasks"
            className="w-32 bg-transparent text-sm outline-none placeholder:text-muted-foreground/70 sm:w-44"
          />
          <button type="button" aria-label="Close search" onClick={() => { onQuery(""); setSearching(false); }} className="flex size-7 items-center justify-center rounded-full text-muted-foreground hover:bg-foreground/10"><X className="size-3.5" /></button>
        </div>
      ) : (
        <button type="button" aria-label="Search" title="Search" onClick={() => setSearching(true)} className={iconButton}><Search /></button>
      )}

      {!board && (
        <DropdownMenu>
          <DropdownMenuTrigger aria-label={`Sort: ${sortLabel[prefs.sort]}`} title="Sort" className={iconButton}><ArrowUpDown />{prefs.sort !== defaultPrefs.sort && <Dot />}</DropdownMenuTrigger>
          <DropdownMenuContent className="w-60"><SortItems prefs={prefs} onChange={onChange} /></DropdownMenuContent>
        </DropdownMenu>
      )}

      <DropdownMenu>
        <DropdownMenuTrigger aria-label={n ? `Filter, ${n} on` : "Filter"} title="Filter" className={iconButton}>
          <ListFilter />
          {n > 0 && <span className="absolute top-0.5 right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-foreground px-1 text-[10px] font-semibold text-background tabular-nums">{n}</span>}
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-56"><FilterItems prefs={prefs} onChange={onChange} /></DropdownMenuContent>
      </DropdownMenu>

      <DropdownMenu>
        <DropdownMenuTrigger aria-label="View settings" title="View settings" className={iconButton}><SlidersHorizontal /></DropdownMenuTrigger>
        <DropdownMenuContent className="w-64">
          <DropdownMenuLabel>View settings</DropdownMenuLabel>
          {onView && (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger value={board ? "Board" : "List"}>{board ? <Columns3 /> : <List />}Layout</DropdownMenuSubTrigger>
              <DropdownMenuSubContent>
                <DropdownMenuCheckItem checked={!board} onSelect={() => onView("list")}><List />List</DropdownMenuCheckItem>
                <DropdownMenuCheckItem checked={board} onSelect={() => onView("board")}><Columns3 />Board</DropdownMenuCheckItem>
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          )}
          <DropdownMenuSub>
            <DropdownMenuSubTrigger value={rowsLabel[prefs.rows]}><Rows3 />Rows</DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              {(Object.keys(rowsLabel) as Rows[]).map((r) => <DropdownMenuCheckItem key={r} checked={prefs.rows === r} onSelect={() => onChange({ rows: r })}>{rowsLabel[r]}</DropdownMenuCheckItem>)}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger value={prefs.hidden.length ? `${Object.keys(propLabel).length - prefs.hidden.length} shown` : undefined}><ToggleRight />Properties</DropdownMenuSubTrigger>
            <DropdownMenuSubContent>
              {(Object.keys(propLabel) as Prop[]).map((p) => <DropdownMenuCheckItem key={p} keepOpen checked={!prefs.hidden.includes(p)} onSelect={() => toggleProp(p)}>{propLabel[p]}</DropdownMenuCheckItem>)}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger value={n ? <span className="tabular-nums">{n}</span> : undefined}><ListFilter />Filter</DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-56"><FilterItems prefs={prefs} onChange={onChange} /></DropdownMenuSubContent>
          </DropdownMenuSub>
          {!board && (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger value={sortLabel[prefs.sort]}><ArrowUpDown />Sort</DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-60"><SortItems prefs={prefs} onChange={onChange} /></DropdownMenuSubContent>
            </DropdownMenuSub>
          )}
          {!board && (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger><EyeOff />Show</DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-60">
                <DropdownMenuCheckItem keepOpen checked={prefs.showDone} onSelect={() => onChange({ showDone: !prefs.showDone })}><Eye />Completed tasks</DropdownMenuCheckItem>
                <DropdownMenuCheckItem keepOpen checked={prefs.group} onSelect={() => onChange({ group: !prefs.group })}><CalendarCheck />Due date sections</DropdownMenuCheckItem>
              </DropdownMenuSubContent>
            </DropdownMenuSub>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuLabel>List settings</DropdownMenuLabel>
          {onEdit && <DropdownMenuItem onSelect={onEdit}><Pencil />Edit list</DropdownMenuItem>}
          <DropdownMenuItem onSelect={copyLink}><Link2 />Copy link</DropdownMenuItem>
          {tabs && <DropdownMenuItem onSelect={() => tabs.openInTab(link)}><SquarePlus />Open in new tab</DropdownMenuItem>}
        </DropdownMenuContent>
      </DropdownMenu>

      <button type="button" aria-label="Copy link" title="Copy link" onClick={copyLink} className={cn(iconButton, "max-sm:hidden")}><Link2 /></button>
      <button type="button" onClick={() => quick.open()} title="New task (N)" className="ml-1 flex h-9 items-center gap-1.5 rounded-full px-3 text-sm font-medium text-foreground transition-colors hover:bg-accent max-md:hidden">
        <Plus className="size-4" />New
      </button>
    </div>
  );
}
