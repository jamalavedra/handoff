import { createContext, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { Activity, Bell, Bot, Calendar, CalendarX, CheckSquare, CircleCheck, Flag, FolderKanban, Keyboard, Monitor, Moon, Plus, Settings, Sparkles, Sun, Trash2, UserRound, Users, Zap } from "lucide-react";
import { api, projectName, type Member, type Project, type Task } from "../lib";
import { addDays, isoDate } from "../quickadd";
import { useMe } from "../main";
import { useSelection } from "../hooks/use-selection";
import { useBulkActions } from "../hooks/use-bulk";
import { setTheme } from "../theme";
import { CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList, CommandShortcut } from "@/components/ui/command";
import { useQuickAdd } from "./quickadd";
import { typing } from "./shortcuts";
import { useAssistant } from "./assistant";
import { useTabs } from "./tabs";
import { taskHref } from "../tabs";

type Ctx = { open: () => void };
const PaletteContext = createContext<Ctx>({ open: () => {} });
export const useCommandPalette = () => useContext(PaletteContext);

type Item = { id: string; label: string; icon: typeof Plus; keys?: string; run: () => void };
type Step = "due" | "assign" | "project" | "priority";
const priorityText = ["Urgent", "High", "Normal", "Low", "Someday"];

const isMod = (e: React.KeyboardEvent | React.PointerEvent) => e.metaKey || e.ctrlKey;

// CommandPaletteProvider owns ⌘K: jump anywhere, run an action, or find a task by title.
export function CommandPaletteProvider({ onShortcuts, children }: { onShortcuts: () => void; children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const ctx = useMemo<Ctx>(() => ({ open: () => setOpen(true) }), []);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.key === "k" && (e.metaKey || e.ctrlKey)) || (e.key === "/" && !typing(e.target))) {
        e.preventDefault();
        setOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  useEffect(() => { if (!open) setQuery(""); }, [open]);
  return (
    <PaletteContext.Provider value={ctx}>
      {children}
      <CommandDialog open={open} onOpenChange={setOpen} shouldFilter={false} title="Command menu" description="Jump to a page, run an action, or find a task" className="top-24 translate-y-0 rounded-2xl sm:max-w-xl">
        {open && <Palette query={query} setQuery={setQuery} close={() => setOpen(false)} onShortcuts={onShortcuts} />}
      </CommandDialog>
    </PaletteContext.Provider>
  );
}

function Palette({ query, setQuery, close, onShortcuts }: { query: string; setQuery: (q: string) => void; close: () => void; onShortcuts: () => void }) {
  const navigate = useNavigate();
  const quick = useQuickAdd();
  const assistant = useAssistant();
  const path = useRouterState({ select: (s) => s.location.pathname });
  const currentProject = path.match(/^\/projects\/([^/]+)/)?.[1];
  const q = query.trim().toLowerCase();
  const [debounced, setDebounced] = useState(q);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q), 150);
    return () => clearTimeout(t);
  }, [q]);
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const tasks = useQuery({
    queryKey: ["tasks", "palette", debounced],
    queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { query: debounced, limit: 8 }),
    enabled: debounced.length > 1,
    placeholderData: (prev) => prev,
  });
  // ⌘/Ctrl+Enter (or ⌘-click) on a task, page or project opens it in a background tab on desktop.
  // cmdk's onSelect carries no event, so the keydown/pointerdown just before it records the modifier.
  const tabs = useTabs();
  const mod = useRef(false);
  const go = (fn: () => void, href?: string) => () => {
    close();
    if (href && tabs && mod.current) tabs.openInTab(href);
    else fn();
  };
  const to = (path: string) => go(() => navigate({ to: path }), path);
  const actions: Item[] = [
    { id: "new", label: "New task", icon: Plus, keys: "N", run: go(() => quick.open(currentProject)) },
    { id: "assistant", label: "Ask the assistant", icon: Sparkles, keys: "⌘J", run: go(() => assistant.setOpen(true)) },
    { id: "light", label: "Theme: light", icon: Sun, run: go(() => setTheme("light")) },
    { id: "dark", label: "Theme: dark", icon: Moon, run: go(() => setTheme("dark")) },
    { id: "system", label: "Theme: match device", icon: Monitor, run: go(() => setTheme("system")) },
    { id: "keys", label: "Keyboard shortcuts", icon: Keyboard, keys: "?", run: go(onShortcuts) },
  ];
  const pages: Item[] = [
    { id: "/", label: "My tasks", icon: CheckSquare, keys: "G T", run: to("/") },
    { id: "/notifications", label: "Notifications", icon: Bell, keys: "G N", run: to("/notifications") },
    { id: "/team", label: "Team", icon: Users, run: to("/team") },
    { id: "/projects", label: "Projects", icon: FolderKanban, keys: "G P", run: to("/projects") },
    { id: "/automations", label: "Automations", icon: Zap, run: to("/automations") },
    { id: "/settings/agents", label: "Agents", icon: Bot, keys: "G A", run: to("/settings/agents") },
    { id: "/settings/activity", label: "Activity", icon: Activity, run: to("/settings/activity") },
    { id: "/trash", label: "Trash", icon: Trash2, run: to("/trash") },
    { id: "/settings", label: "Settings", icon: Settings, keys: "G S", run: to("/settings") },
  ];
  // With tasks selected, their actions come first; the ones ending in … open a second step.
  const select = useSelection();
  const bulk = useBulkActions();
  const [step, setStep] = useState<Step>();
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members"), enabled: step === "assign" });
  const me = useMe().data?.member;
  const n = select.ids.length;
  const them = n === 1 ? "task" : `${n} tasks`;
  const act = (fn: (ids: string[]) => unknown) => go(() => void fn(select.ids));
  const next = (s: Step) => () => { setStep(s); setQuery(""); };
  const today = new Date();
  const steps: Record<Step, { heading: string; items: Item[] }> = {
    due: { heading: "Set due date", items: [
      { id: "today", label: "Today", icon: Calendar, run: act((ids) => bulk.due(ids, isoDate(today))) },
      { id: "tomorrow", label: "Tomorrow", icon: Calendar, run: act((ids) => bulk.due(ids, isoDate(addDays(today, 1)))) },
      { id: "next-week", label: "Next week", icon: Calendar, run: act((ids) => bulk.due(ids, isoDate(addDays(today, ((8 - today.getDay()) % 7) || 7)))) },
      { id: "none", label: "Remove due date", icon: CalendarX, run: act((ids) => bulk.due(ids, "")) },
    ] },
    assign: { heading: "Assign to", items: [
      ...(me ? [{ id: "me", label: `Me (${me.name})`, icon: UserRound, run: act((ids) => bulk.assign(ids, me.id, me.name)) }] : []),
      ...(members.data?.members ?? []).filter((m) => m.id !== me?.id && m.status !== "removed" && m.status !== "revoked")
        .map((m) => ({ id: m.id, label: m.name, icon: m.kind === "agent" ? Bot : UserRound, run: act((ids) => bulk.assign(ids, m.id, m.name)) })),
      { id: "nobody", label: "Unassigned", icon: UserRound, run: act((ids) => bulk.assign(ids, "", "")) },
    ] },
    project: { heading: "Move to project", items: (projects.data?.projects ?? []).map((p) => ({ id: p.id, label: projectName(p), icon: FolderKanban, run: act((ids) => bulk.move(ids, p.id, projectName(p))) })) },
    priority: { heading: "Set priority", items: priorityText.map((label, i) => ({ id: String(i), label, icon: Flag, run: act((ids) => bulk.priority(ids, i)) })) },
  };
  const selection: Item[] = n ? [
    { id: "complete", label: `Complete ${them}`, icon: CircleCheck, run: act(bulk.complete) },
    { id: "due", label: "Set due date…", icon: Calendar, run: next("due") },
    { id: "assign", label: "Assign to…", icon: UserRound, run: next("assign") },
    { id: "project", label: "Move to project…", icon: FolderKanban, run: next("project") },
    { id: "priority", label: "Set priority…", icon: Flag, run: next("priority") },
    { id: "trash", label: `Move ${them} to trash`, icon: Trash2, run: act((ids) => { select.clear(); return bulk.trash(ids); }) },
  ] : [];
  const match = (s: string) => !q || s.toLowerCase().includes(q);
  if (step) {
    const shown = steps[step].items.filter((i) => match(i.label));
    return (
      <>
        <CommandInput
          placeholder={`${steps[step].heading} for ${them}…`}
          value={query}
          onValueChange={setQuery}
          onKeyDown={(e) => { if (e.key === "Backspace" && !query) setStep(undefined); }}
        />
        <CommandList className="max-h-[min(60dvh,26rem)]">
          <CommandEmpty>{step === "assign" && members.isLoading ? "Loading…" : "No matches."}</CommandEmpty>
          {shown.length > 0 && <Group heading={steps[step].heading} items={shown} />}
        </CommandList>
      </>
    );
  }
  const shownSelection = selection.filter((a) => match(a.label));
  const shownActions = actions.filter((a) => match(a.label));
  const shownPages = pages.filter((p) => match(p.label));
  const shownProjects = (projects.data?.projects ?? []).filter((p) => match(p.name)).slice(0, q ? 6 : 4);
  const found = debounced.length > 1 ? tasks.data?.tasks ?? [] : [];
  const openTask = (id: string) => go(() => navigate({ to: "/tasks/$id", params: { id } }), taskHref(id));
  return (
    <div
      className="contents"
      onKeyDownCapture={(e) => { if (e.key === "Enter") mod.current = isMod(e); }}
      onPointerDownCapture={(e) => { mod.current = isMod(e); }}
    >
      <CommandInput placeholder="Search tasks, pages and actions…" value={query} onValueChange={setQuery} />
      <CommandList className="max-h-[min(60dvh,26rem)]">
        <CommandEmpty>{tasks.isFetching ? "Searching…" : "No matches."}</CommandEmpty>
        {shownSelection.length > 0 && <Group heading={`Selected (${n})`} items={shownSelection} />}
        {found.length > 0 && (
          <CommandGroup heading="Tasks">
            {found.map((t) => (
              <CommandItem key={t.id} value={`task-${t.id}`} onSelect={openTask(t.id)}>
                <CheckSquare className="text-muted-foreground" />
                <span className="truncate">{t.title}</span>
                <span className="ml-auto shrink-0 text-xs text-muted-foreground">{t.project}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}
        {shownActions.length > 0 && <Group heading="Actions" items={shownActions} />}
        {shownPages.length > 0 && <Group heading="Go to" items={shownPages} />}
        {shownProjects.length > 0 && (
          <CommandGroup heading="Projects">
            {shownProjects.map((p) => (
              <CommandItem key={p.id} value={`project-${p.id}`} onSelect={go(() => navigate({ to: "/projects/$id", params: { id: p.id } }), `/projects/${p.id}`)}>
                <FolderKanban className="text-muted-foreground" />
                <span className="truncate">{projectName(p)}</span>
                <span className="ml-auto text-xs text-muted-foreground tabular-nums">{p.open_tasks}</span>
              </CommandItem>
            ))}
          </CommandGroup>
        )}
      </CommandList>
    </div>
  );
}

function Group({ heading, items }: { heading: string; items: Item[] }) {
  return (
    <CommandGroup heading={heading}>
      {items.map(({ id, label, icon: Icon, keys, run }) => (
        <CommandItem key={id} value={`${heading}-${id}`} onSelect={run}>
          <Icon className="text-muted-foreground" />
          {label}
          {keys && <CommandShortcut>{keys}</CommandShortcut>}
        </CommandItem>
      ))}
    </CommandGroup>
  );
}
