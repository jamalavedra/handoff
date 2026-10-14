import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useCanGoBack, useRouter, useRouterState } from "@tanstack/react-router";
import { Activity, ArrowLeft, ArrowRight, Bell, Bot, CheckSquare, FolderKanban, LayoutGrid, Plus, Settings, Trash2, Users, X, Zap, Plug } from "lucide-react";
import { api, cn, type Project, type Task } from "../lib";
import { agentOnIt } from "../quickadd";
import * as tabs from "../tabs";
import { StatusCircle } from "./app";
import { typing } from "./shortcuts";

// Tabs are a desktop thing: phones have the bottom tab bar.
const desktop = window.matchMedia("(min-width: 768px)");

function useDesktop() {
  return useSyncExternalStore(
    (l) => {
      desktop.addEventListener("change", l);
      return () => desktop.removeEventListener("change", l);
    },
    () => desktop.matches,
  );
}

const storeKey = "handoff-tabs";
const historyKey = "handoff-history";
const newId = () => Math.random().toString(36).slice(2, 10);

function read(): unknown {
  try {
    return JSON.parse(localStorage.getItem(storeKey) ?? "null");
  } catch {
    return null;
  }
}

type Ctx = {
  enabled: boolean;
  state: tabs.Tabs;
  canForward: boolean;
  openInTab: (href: string) => void;
  newTab: () => void;
  select: (id: string) => void;
  close: (id: string) => void;
  drop: (id: string) => void;
};
const TabsContext = createContext<Ctx | null>(null);

// useOpenInTab gives a link the browser gestures for a background tab: ⌘/Ctrl+click and middle-click.
// Off on phones, where the link behaves as before.
export function useOpenInTab(href: string) {
  const ctx = useContext(TabsContext);
  if (!ctx?.enabled) return {};
  return {
    onClick: (e: React.MouseEvent) => {
      if (e.button !== 0 || !(e.metaKey || e.ctrlKey) || e.shiftKey || e.altKey) return;
      e.preventDefault();
      ctx.openInTab(href);
    },
    onAuxClick: (e: React.MouseEvent) => {
      if (e.button !== 1) return;
      e.preventDefault();
      ctx.openInTab(href);
    },
  };
}

// useTabs is for callers that open tabs themselves (menus, the command menu); null on phones.
export function useTabs() {
  const ctx = useContext(TabsContext);
  return ctx?.enabled ? ctx : null;
}

// TabsProvider keeps the open tabs in step with the router and in localStorage.
export function TabsProvider({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const enabled = useDesktop();
  const href = useRouterState({ select: (s) => s.location.pathname + s.location.searchStr });
  const [state, setState] = useState(() => tabs.restore(read(), href, newId()));
  const latest = useRef(state);
  latest.current = state;

  // History bookkeeping: whether the last move was Back/Forward, and how far Forward can go
  // (kept per browser tab so a reload doesn't forget it, see tabs.resumeHistory).
  const traverse = useRef(false);
  const [canForward, setCanForward] = useState(false);
  useEffect(() => {
    const entry = () => {
      const st = router.history.location.state as { __TSR_index?: number; __TSR_key?: string };
      return { index: st.__TSR_index ?? 0, key: st.__TSR_key ?? "" };
    };
    let saved: unknown = null;
    try {
      saved = JSON.parse(sessionStorage.getItem(historyKey) ?? "null");
    } catch {
      // storage blocked: forward only knows this page's moves
    }
    const first = entry();
    let marks = tabs.resumeHistory(saved, first.index, first.key);
    setCanForward(first.index < marks.max);
    return router.history.subscribe(({ action }) => {
      const { index, key } = entry();
      traverse.current = action.type !== "PUSH" && action.type !== "REPLACE";
      marks = tabs.stepHistory(marks, index, key, action.type === "PUSH");
      setCanForward(index < marks.max);
      try {
        sessionStorage.setItem(historyKey, JSON.stringify(marks));
      } catch {
        // as above
      }
    });
  }, [router]);

  // Tabs only follow the router on desktop: phone-width navigation leaves them alone, and coming
  // back to desktop brings the current page in the way a fresh link would.
  const wasEnabled = useRef(enabled);
  useEffect(() => {
    const t = traverse.current;
    traverse.current = false;
    const returning = enabled && !wasEnabled.current;
    wasEnabled.current = enabled;
    if (!enabled) return;
    setState((s) => (returning ? tabs.arrive(s, href, newId()) : tabs.visit(s, href, t)));
  }, [href, enabled]);

  useEffect(() => {
    if (!enabled) return;
    try {
      localStorage.setItem(storeKey, JSON.stringify(state));
    } catch {
      // private mode: tabs last until reload
    }
  }, [state, enabled]);

  // move commits a new state and shows its active tab.
  const move = useCallback(
    (next: tabs.Tabs, replace = false) => {
      latest.current = next;
      setState(next);
      const to = next.tabs.find((t) => t.id === next.active)?.href;
      const here = router.history.location.pathname + router.history.location.search;
      if (to && to !== here) router.history[replace ? "replace" : "push"](to);
    },
    [router],
  );

  const ctx = useMemo<Ctx>(
    () => ({
      enabled,
      state,
      canForward,
      openInTab: (to) => setState((s) => tabs.open(s, to, newId())),
      newTab: () => move(tabs.fresh(latest.current, newId())),
      select: (id) => move(tabs.activate(latest.current, id)),
      // Closing doesn't leave a history entry behind.
      close: (id) => move(tabs.close(latest.current, id, newId()), true),
      // A background tab whose task is gone quietly disappears; the open one shows its error instead.
      drop: (id) => setState((s) => (s.active === id ? s : tabs.close(s, id, newId()))),
    }),
    [enabled, state, canForward, move],
  );

  // Browsers own ⌘/Ctrl+W, ⌘T and Ctrl+Tab, so tabs use Alt (⌥ on a Mac): W closes, T opens, 1–9 jump.
  useEffect(() => {
    if (!enabled) return;
    const onKey = (e: KeyboardEvent) => {
      if (!e.altKey || e.metaKey || e.ctrlKey || e.shiftKey || e.defaultPrevented || typing(e.target)) return;
      if (document.querySelector("[role=dialog], [role=menu]")) return;
      const s = latest.current;
      if (e.code === "KeyW") {
        e.preventDefault();
        ctx.close(s.active);
      } else if (e.code === "KeyT") {
        e.preventDefault();
        ctx.newTab();
      } else if (/^Digit[1-9]$/.test(e.code)) {
        e.preventDefault();
        const n = Number(e.code.slice(5));
        // Like a browser, 9 is always the last tab.
        const t = n === 9 ? s.tabs[s.tabs.length - 1] : s.tabs[n - 1];
        if (t) ctx.select(t.id);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [enabled, ctx]);

  return <TabsContext.Provider value={ctx}>{children}</TabsContext.Provider>;
}

const stripButton = "flex size-7 shrink-0 items-center justify-center rounded-[8px] text-foreground/50 transition-colors hover:bg-sidebar-accent/60 hover:text-foreground disabled:pointer-events-none disabled:opacity-35";

// TabStrip is the slim row above the page on desktop: history arrows, the open tabs, and +.
export function TabStrip() {
  const ctx = useContext(TabsContext);
  const router = useRouter();
  const canBack = useCanGoBack();
  const activeRef = useRef<HTMLDivElement>(null);
  const active = ctx?.state.active;
  useEffect(() => activeRef.current?.scrollIntoView({ block: "nearest", inline: "nearest" }), [active]);
  if (!ctx?.enabled) return null;
  return (
    <div className="sticky top-0 z-20 flex h-11 shrink-0 items-center gap-1 border-b border-sidebar-border bg-sidebar px-2 text-sidebar-foreground">
      <button className={stripButton} disabled={!canBack} onClick={() => router.history.back()} aria-label="Back" title="Back">
        <ArrowLeft className="size-4" />
      </button>
      <button className={stripButton} disabled={!ctx.canForward} onClick={() => router.history.forward()} aria-label="Forward" title="Forward">
        <ArrowRight className="size-4" />
      </button>
      <div role="tablist" aria-label="Open tabs" className="ml-1 flex min-w-0 flex-1 items-center gap-1 overflow-x-auto py-1 [scrollbar-width:none]">
        {ctx.state.tabs.map((t) => (
          <TabItem key={t.id} ref={t.id === active ? activeRef : undefined} tab={t} active={t.id === active} only={ctx.state.tabs.length === 1} />
        ))}
        <button className={stripButton} onClick={ctx.newTab} aria-label="New tab" title="New tab (⌥T)">
          <Plus className="size-4" />
        </button>
      </div>
    </div>
  );
}

function TabItem({ tab, active, only, ref }: { tab: tabs.Tab; active: boolean; only: boolean; ref?: React.Ref<HTMLDivElement> }) {
  const ctx = useContext(TabsContext)!;
  const close = () => ctx.close(tab.id);
  return (
    <div
      ref={ref}
      onAuxClick={(e) => {
        if (e.button !== 1) return;
        e.preventDefault();
        close();
      }}
      className={cn(
        "group relative flex h-7 max-w-52 min-w-28 flex-1 basis-52 items-center rounded-[8px] text-[13px] transition-colors",
        active ? "bg-sidebar-accent text-foreground shadow-[0_1px_2px_rgba(0,0,0,0.06)]" : "text-foreground/65 hover:bg-sidebar-accent/60 hover:text-foreground",
      )}
    >
      <a
        role="tab"
        aria-selected={active}
        href={tab.href}
        onClick={(e) => {
          // ⌘/Shift-click still hands the location to the browser.
          if (e.metaKey || e.ctrlKey || e.shiftKey) return;
          e.preventDefault();
          ctx.select(tab.id);
        }}
        className="flex h-full min-w-0 flex-1 items-center gap-2 rounded-[8px] pr-7 pl-2.5 outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring"
      >
        <TabLabel href={tab.href} onMissing={active ? undefined : () => ctx.drop(tab.id)} />
      </a>
      {!only && (
        <button
          onClick={close}
          aria-label="Close tab"
          title="Close tab (⌥W)"
          className={cn("absolute right-1 flex size-5 items-center justify-center rounded-[6px] text-foreground/45 opacity-0 transition hover:bg-foreground/10 hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100", active && "opacity-100")}
        >
          <X className="size-3.5" />
        </button>
      )}
    </div>
  );
}

const pages: Record<string, { label: string; icon: typeof Bell }> = {
  "/": { label: "My tasks", icon: CheckSquare },
  "/notifications": { label: "Notifications", icon: Bell },
  "/team": { label: "Team", icon: Users },
  "/projects": { label: "Projects", icon: FolderKanban },
  "/settings/activity": { label: "Activity", icon: Activity },
  "/settings/agents": { label: "Agents", icon: Bot },
  "/settings/connections": { label: "Connections", icon: Plug },
  "/trash": { label: "Trash", icon: Trash2 },
  "/automations": { label: "Automations", icon: Zap },
  "/settings": { label: "Settings", icon: Settings },
  // Tabs saved before these moved into Settings keep their names until they're opened.
  "/agents": { label: "Agents", icon: Bot },
  "/activity": { label: "Activity", icon: Activity },
  "/admin": { label: "Settings", icon: Settings },
  "/more": { label: "More", icon: LayoutGrid },
};

const iconBox = "flex size-4 shrink-0 items-center justify-center";

function Title({ children, muted }: { children: string; muted?: boolean }) {
  return <span title={children} className={cn("truncate", muted && "text-muted-foreground")}>{children}</span>;
}

function TabLabel({ href, onMissing }: { href: string; onMissing?: () => void }) {
  const t = tabs.target(href);
  if (t.kind === "task") return <TaskLabel id={t.id} onMissing={onMissing} />;
  if (t.kind === "project") return <ProjectLabel id={t.id} />;
  const page = pages[t.path] ?? { label: "Handoff", icon: LayoutGrid };
  return (
    <>
      <span className={iconBox}><page.icon className="size-3.5 text-foreground/50" /></span>
      <Title>{page.label}</Title>
    </>
  );
}

// Shares the task page's cache entry; a tab only reads it, it doesn't poll or mark anything read.
// cachedTask finds a task in whatever the app already loaded: its detail, or any task list
// (cached as { tasks } or, on the Team board, a bare array).
function cachedTask(qc: QueryClient, id: string) {
  const detail = qc.getQueryData<{ task: Task }>(["task", id])?.task;
  if (detail) return detail;
  for (const [, data] of qc.getQueriesData<{ tasks: Task[] } | Task[]>({ queryKey: ["tasks"] })) {
    const t = (Array.isArray(data) ? data : data?.tasks)?.find((x) => x.id === id);
    if (t) return t;
  }
}

// A background tab must not open the task: get_task marks it seen, which would clear its unseen
// dot before anyone looked. So it reads the cache, else searches by id (search doesn't mark).
// The open tab is the task page itself, whose detail query is already running.
async function peekTask(qc: QueryClient, id: string): Promise<{ tasks: Task[] }> {
  const cached = cachedTask(qc, id);
  if (cached) return { tasks: [cached] };
  const find = (input: object) => api.tool<{ tasks: Task[] }>("search_tasks", { query: id, include_done: true, limit: 5, ...input }).then((r) => r.tasks.filter((t) => t.id === id));
  const found = await find({});
  return { tasks: found.length ? found : await find({ trashed: true }) };
}

function TaskLabel({ id, onMissing }: { id: string; onMissing?: () => void }) {
  const qc = useQueryClient();
  const background = !!onMissing;
  const detail = useQuery({ queryKey: ["task", id], queryFn: () => api.tool<{ task: Task }>("get_task", { id }), enabled: !background, refetchInterval: false, staleTime: 60_000 });
  // Keyed under ["tasks"] so list refreshes and optimistic patches reach it too.
  const peek = useQuery({ queryKey: ["tasks", "tab", id], queryFn: () => peekTask(qc, id), enabled: background, refetchInterval: false, staleTime: 30_000 });
  // Only background tabs go (onMissing is unset on the open one, which shows its own error).
  const missing = background && peek.data?.tasks.length === 0;
  const drop = useRef(onMissing);
  drop.current = onMissing;
  useEffect(() => { if (missing) drop.current?.(); }, [missing]);
  const t = background ? peek.data?.tasks[0] : detail.data?.task;
  const failed = background ? missing || peek.isError : detail.isError;
  return (
    <>
      <span className={iconBox}>{t ? <StatusCircle t={t} small /> : <CheckSquare className="size-3.5 text-foreground/40" />}</span>
      {t ? <Title>{t.title}</Title> : <Title muted>{failed ? "Task not found" : "Loading…"}</Title>}
      {t && agentOnIt(t) && <span aria-hidden className="agent-border pointer-events-none absolute inset-0 rounded-[8px]" />}
    </>
  );
}

function ProjectLabel({ id }: { id: string }) {
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const p = projects.data?.projects.find((x) => x.id === id);
  return (
    <>
      <span className={iconBox}>{p?.emoji ? <span className="text-[13px] leading-none">{p.emoji}</span> : <FolderKanban className="size-3.5 text-foreground/50" />}</span>
      <Title muted={!p}>{p?.name ?? (projects.isLoading ? "Loading…" : "Project")}</Title>
    </>
  );
}
