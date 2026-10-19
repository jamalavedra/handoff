import { StrictMode, useCallback, useState } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRootRoute, createRoute, createRouter, Link, Navigate, Outlet, redirect, RouterProvider, useRouterState } from "@tanstack/react-router";
import { Bell, CheckSquare, ChevronDown, ChevronRight, Search, FolderKanban, Inbox, LogOut, Monitor, Moon, MoreHorizontal, Plus, Settings, Sun, Trash2, Users, Zap } from "lucide-react";
import { setTheme, useTheme, type Theme } from "./theme";
import { useLive } from "./live";
import "./index.css";
import { api, cn, projectName, type Me, type Project } from "./lib";
import { AuthorizePage, LoginPage, SignupPage } from "./pages/auth";
import { ActivityPage, MyTasksPage, ProjectPage, ProjectsPage, TrashPage } from "./pages/tasks";
import { TaskPage } from "./pages/task";
import { NotePage } from "./pages/notes";
import { TeamPage } from "./pages/team";
import { NotificationsPage, useInbox } from "./pages/notifications";
import { AgentsPage } from "./pages/agents";
import { AutomationsPage } from "./pages/automations";
import { AdminPage } from "./pages/admin";
import { ConnectionsPage } from "./pages/connections";
import { AppsPage } from "./pages/apps";
import { MemoryPage } from "./pages/memory";
import { DevicesPage } from "./pages/devices";
import { QuickAddProvider, useQuickAdd } from "./components/quickadd";
import { PageHeader } from "./components/app";
import { Drawer, DrawerContent, DrawerTitle } from "@/components/ui/drawer";
import { Toaster } from "@/components/ui/sonner";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { useNotificationToasts } from "./components/feedback";
import { CommandPaletteProvider, useCommandPalette } from "./components/command-palette";
import { ShortcutsDialog, useShortcuts } from "./components/shortcuts";
import { AssistantFab, AssistantPanel, AssistantProvider, clearAssistant, useAssistantInset, useFab } from "./components/assistant";
import { TabsProvider, TabStrip, useOpenInTab } from "./components/tabs";

const qc = new QueryClient({ defaultOptions: { queries: { refetchInterval: 30000, retry: false, staleTime: 2000 } } });

export function useMe() {
  return useQuery({ queryKey: ["me"], queryFn: () => api.get<Me>("/api/me"), refetchInterval: false });
}

function Shell() {
  const me = useMe();
  const path = useRouterState({ select: (s) => s.location.pathname });
  if (me.isLoading) return null;
  if (me.data?.apex) return <SignupPage me={me.data} />;
  if (path === "/login") return <Outlet />;
  if (!me.data?.member) {
    window.location.replace(`/login?next=${encodeURIComponent(window.location.pathname + window.location.search)}`);
    return null;
  }
  if (path === "/authorize") return <Outlet />;
  return <Layout me={me.data} />;
}

function NavLink({ to, icon: Icon, children }: { to: string; icon: typeof Inbox; children: React.ReactNode }) {
  const tab = useOpenInTab(to);
  return (
    <Link to={to} {...tab} className="group flex min-h-8 items-center gap-2 rounded-[10px] px-2 py-1 text-[14px] font-medium text-foreground/70 transition-colors hover:bg-sidebar-accent/60 data-[status=active]:bg-sidebar-accent data-[status=active]:text-foreground data-[status=active]:shadow-[0_1px_2px_rgba(0,0,0,0.06)]" activeOptions={{ exact: to === "/" || to === "/projects" }}>
      <Icon className="size-4 text-foreground/50 transition-colors group-hover:text-foreground/80 group-data-[status=active]:text-foreground" />
      {children}
    </Link>
  );
}

function SearchButton() {
  const palette = useCommandPalette();
  return (
    <button onClick={palette.open} className="mb-3 flex h-8 items-center gap-2 rounded-[10px] border border-sidebar-border bg-background/60 px-2 text-[13px] text-muted-foreground transition-colors hover:bg-background hover:text-foreground">
      <Search className="size-4" />
      <span className="flex-1 text-left">Search</span>
      <kbd className="rounded border border-border px-1 font-sans text-[10px]">⌘K</kbd>
    </button>
  );
}

const projectsOpenKey = "handoff-sidebar-projects";

function readProjectsOpen() {
  try {
    return localStorage.getItem(projectsOpenKey) !== "closed";
  } catch {
    return true;
  }
}

// ProjectsNav is the sidebar's Projects entry: the label opens the Projects page, the chevron folds the list.
function ProjectsNav({ projects }: { projects: Project[] }) {
  const [open, setOpen] = useState(readProjectsOpen);
  const toggle = (o: boolean) => {
    setOpen(o);
    try {
      localStorage.setItem(projectsOpenKey, o ? "open" : "closed");
    } catch {
      // private mode: remember for this tab only
    }
  };
  return (
    <Collapsible open={open} onOpenChange={toggle}>
      <div className="relative">
        <NavLink to="/projects" icon={FolderKanban}><span className="flex-1">Projects</span></NavLink>
        <CollapsibleTrigger
          aria-label={open ? "Hide projects" : "Show projects"}
          className="absolute top-1/2 right-1 flex size-6 -translate-y-1/2 items-center justify-center rounded-md text-foreground/40 transition-colors hover:bg-sidebar-accent hover:text-foreground"
        >
          <ChevronDown className={cn("size-3.5 transition-transform", !open && "-rotate-90")} />
        </CollapsibleTrigger>
      </div>
      <CollapsibleContent className="mt-0.5 ml-[1.125rem] flex flex-col gap-0.5 border-l border-sidebar-border pl-2">
        {projects.map((p) => <ProjectNavLink key={p.id} p={p} />)}
      </CollapsibleContent>
    </Collapsible>
  );
}

function ProjectNavLink({ p }: { p: Project }) {
  const tab = useOpenInTab(`/projects/${p.id}`);
  return (
    <Link to="/projects/$id" params={{ id: p.id }} {...tab} className="flex min-h-7 items-center justify-between gap-2 rounded-[10px] px-2 py-1 text-[13px] text-foreground/70 transition-colors hover:bg-sidebar-accent/60 data-[status=active]:bg-sidebar-accent data-[status=active]:text-foreground data-[status=active]:shadow-[0_1px_2px_rgba(0,0,0,0.06)]">
      <span className="truncate">{projectName(p)}</span>
      <span className="text-xs text-foreground/40 tabular-nums">{p.open_tasks}</span>
    </Link>
  );
}

function useLayoutData() {
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const approvals = useQuery({ queryKey: ["approvals"], queryFn: () => api.tool<{ approvals: unknown[] }>("list_approvals") });
  const inbox = useInbox();
  return { projects: projects.data?.projects ?? [], pending: approvals.data?.approvals.length ?? 0, unread: inbox.data?.unread ?? 0 };
}

function useLogout() {
  const qcl = useQueryClient();
  return async () => {
    await api.post("/api/logout");
    qcl.clear();
    clearAssistant();
    window.location.href = "/login";
  };
}

function Badge({ n }: { n: number }) {
  return n > 0 ? <span className="rounded-full bg-amber-500 px-1.5 text-xs font-medium text-white">{n}</span> : null;
}

function Layout({ me }: { me: Me }) {
  useLive(true);
  useNotificationToasts();
  const { projects, pending, unread } = useLayoutData();
  const logout = useLogout();
  const [help, setHelp] = useState(false);
  // On phones a task page is a pushed screen: its own back bar, no app bar or tabs.
  const onTask = useRouterState({ select: (st) => st.location.pathname.startsWith("/tasks/") });
  const showHelp = useCallback(() => setHelp(true), []);
  useShortcuts(showHelp);
  return (
    <TabsProvider>
    <QuickAddProvider>
      <AssistantProvider>
      <CommandPaletteProvider onShortcuts={showHelp}>
      <ShortcutsDialog open={help} onOpenChange={setHelp} />
      <div className="min-h-dvh md:flex">
        <aside className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-sidebar-border bg-sidebar p-3 text-sidebar-foreground md:flex">
          <div className="mb-5 px-2 pt-1">
            <div className="truncate font-display text-[17px] font-semibold tracking-tight">{me.org?.name}</div>
            <div className="text-xs text-muted-foreground">Handoff</div>
          </div>
          <SearchButton />
          <nav className="-mx-1 flex min-h-0 flex-col gap-1 overflow-y-auto px-1 pb-2">
            <NavLink to="/" icon={CheckSquare}>My tasks</NavLink>
            <NavLink to="/team" icon={Users}>Team</NavLink>
            <NavLink to="/automations" icon={Zap}>Automations</NavLink>
            <ProjectsNav projects={projects} />
            <NavLink to="/trash" icon={Trash2}>Trash</NavLink>
            <NavLink to="/settings" icon={Settings}>
              <span className="flex-1">Settings</span>
              <Badge n={pending} />
            </NavLink>
          </nav>
          <div className="mt-auto flex items-center justify-between px-2 pb-3">
            <ThemeToggle />
            <SidebarBell unread={unread} />
          </div>
          <div className="flex items-center justify-between border-t border-border px-2 pt-3 text-sm">
            <div className="min-w-0">
              <div className="truncate font-medium">{me.member?.name}</div>
              <div className="truncate text-xs text-muted-foreground">{me.member?.email}</div>
            </div>
            <button onClick={logout} className="rounded p-1 text-muted-foreground hover:bg-accent" title="Sign out" aria-label="Sign out">
              <LogOut className="size-4" />
            </button>
          </div>
        </aside>
        <div className="flex min-w-0 flex-1 flex-col">
          <TabStrip />
          <div className="flex min-w-0 flex-1">
            <div className="flex min-w-0 flex-1 flex-col">
              {!onTask && <MobileTopBar me={me} unread={unread} />}
              <main className={cn("min-w-0 flex-1 px-4 md:px-8 md:pt-8 md:pb-6 lg:px-16", onTask ? "pt-[calc(0.5rem+env(safe-area-inset-top))] pb-[env(safe-area-inset-bottom)]" : "pt-4 pb-[calc(9.5rem+env(safe-area-inset-bottom))] md:pb-24")}>
                <div className="mx-auto max-w-[800px]">
                  <Outlet />
                </div>
              </main>
            </div>
          </div>
        </div>
        <AssistantPanel me={me} />
        <AssistantFab />
        {!onTask && <MobileTabBar pending={pending} unread={unread} />}
        <AppToaster />
      </div>
      </CommandPaletteProvider>
      </AssistantProvider>
    </QuickAddProvider>
    </TabsProvider>
  );
}

// Toasts keep clear of the assistant panel, and stack above the floating assistant button or a
// task's chat bar.
function AppToaster() {
  const inset = useAssistantInset();
  const fab = useFab();
  const bottom = fab.shown ? "5rem" : fab.onTask ? "5.5rem" : undefined;
  return (
    <Toaster
      offset={inset || bottom ? { right: inset, bottom } : undefined}
      mobileOffset={{ bottom: fab.shown ? "calc(9.5rem + env(safe-area-inset-bottom))" : "calc(5.5rem + env(safe-area-inset-bottom))" }}
    />
  );
}

// SidebarBell opens Notifications from the sidebar's footer, with the unread count.
function SidebarBell({ unread }: { unread: number }) {
  const tab = useOpenInTab("/notifications");
  return (
    <Link
      to="/notifications"
      {...tab}
      title="Notifications (G N)"
      aria-label={unread ? `Notifications, ${unread} unread` : "Notifications"}
      className="relative flex size-8 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-sidebar-accent hover:text-foreground data-[status=active]:bg-sidebar-accent data-[status=active]:text-foreground"
    >
      <Bell className="size-4" />
      {unread > 0 && <span className="absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-red-500 px-1 text-[10px] font-semibold text-white tabular-nums">{unread > 9 ? "9+" : unread}</span>}
    </Link>
  );
}

function MobileTopBar({ me, unread }: { me: Me; unread: number }) {
  const quick = useQuickAdd();
  const path = useRouterState({ select: (s) => s.location.pathname });
  const project = path.match(/^\/projects\/([^/]+)/)?.[1];
  return (
    <header className="sticky top-0 z-20 border-b border-border bg-background/90 pt-[env(safe-area-inset-top)] backdrop-blur md:hidden">
      <div className="flex h-14 items-center justify-between px-4">
        <span className="truncate rounded-full border border-border bg-card px-3 py-1 text-sm font-semibold">{me.org?.name}</span>
        <div className="flex items-center gap-2">
          <Link to="/notifications" aria-label={unread ? `Notifications, ${unread} unread` : "Notifications"} className="relative flex size-10 items-center justify-center rounded-full border border-border bg-card">
            <Bell className="size-5" />
            {unread > 0 && <span className="absolute -top-1 -right-1 flex h-5 min-w-5 items-center justify-center rounded-full bg-red-500 px-1 text-[11px] font-semibold text-white">{unread > 9 ? "9+" : unread}</span>}
          </Link>
          <button onClick={() => quick.open(project)} className="flex size-10 items-center justify-center rounded-full border border-border bg-card" aria-label="New task">
            <Plus className="size-5" />
          </button>
        </div>
      </div>
    </header>
  );
}

const moreRoutes = ["/more", "/team", "/automations", "/trash", "/settings"];

const tabClass = (active: boolean) => cn("relative flex flex-col items-center gap-0.5 rounded-xl py-2 text-[11px] font-medium text-foreground/50", active && "bg-accent text-foreground");

function Tab({ to, icon: Icon, label, active, badge }: { to: string; icon: typeof Inbox; label: string; active: boolean; badge?: number }) {
  return (
    <Link to={to} className={tabClass(active)}>
      <Icon className="size-5" />
      {label}
      {!!badge && <span className="absolute top-1 right-[calc(50%-1.25rem)] rounded-full bg-amber-500 px-1.5 text-[10px] leading-4 text-white">{badge}</span>}
    </Link>
  );
}

function MobileTabBar({ pending, unread }: { pending: number; unread: number }) {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const more = moreRoutes.some((r) => path === r || path.startsWith(r + "/"));
  const [open, setOpen] = useState(false);
  return (
    <nav aria-label="Main" className="fixed inset-x-3 bottom-[calc(0.75rem+env(safe-area-inset-bottom))] z-30 grid grid-cols-4 gap-1 rounded-2xl border border-border bg-background/95 p-1 shadow-lg backdrop-blur md:hidden">
      <Tab to="/" icon={CheckSquare} label="Tasks" active={path === "/" || path.startsWith("/tasks/")} />
      <Tab to="/notifications" icon={Bell} label="Inbox" active={path === "/notifications"} badge={unread} />
      <Tab to="/projects" icon={FolderKanban} label="Projects" active={path === "/projects" || path.startsWith("/projects/")} />
      <Drawer open={open} onOpenChange={setOpen}>
        <button type="button" onClick={() => setOpen(true)} className={tabClass(more || open)}>
          <MoreHorizontal className="size-5" />
          More
          {pending > 0 && <span className="absolute top-1 right-[calc(50%-1.25rem)] rounded-full bg-amber-500 px-1.5 text-[10px] leading-4 text-white">{pending}</span>}
        </button>
        <DrawerContent className="px-4 pb-[calc(1rem+env(safe-area-inset-bottom))]">
          <DrawerTitle className="sr-only">More</DrawerTitle>
          <div className="max-h-[75dvh] overflow-y-auto pt-4">
            <MoreContent onNavigate={() => setOpen(false)} />
          </div>
        </DrawerContent>
      </Drawer>
    </nav>
  );
}

function MoreRow({ to, icon: Icon, children, onNavigate }: { to: string; icon: typeof Inbox; children: React.ReactNode; onNavigate?: () => void }) {
  return (
    <Link to={to} onClick={onNavigate} className="flex min-h-12 items-center gap-3 border-b border-border px-4 text-[15px] last:border-0 active:bg-muted">
      <Icon className="size-5 text-muted-foreground" />
      <span className="flex-1">{children}</span>
      <ChevronRight className="size-4 text-muted-foreground/50" />
    </Link>
  );
}

const themes: { t: Theme; icon: typeof Sun; label: string }[] = [
  { t: "system", icon: Monitor, label: "Match device" },
  { t: "light", icon: Sun, label: "Light" },
  { t: "dark", icon: Moon, label: "Dark" },
];

function ThemeToggle() {
  const theme = useTheme();
  return (
    <div role="radiogroup" aria-label="Theme" className="inline-flex rounded-full bg-accent p-0.5">
      {themes.map(({ t, icon: Icon, label }) => (
        <button key={t} role="radio" aria-checked={theme === t} aria-label={label} title={label} onClick={() => setTheme(t)} className={cn("flex size-8 items-center justify-center rounded-full text-muted-foreground transition-colors md:size-7", theme === t && "bg-card text-foreground shadow-sm")}>
          <Icon className="size-4" />
        </button>
      ))}
    </div>
  );
}

function MorePage() {
  return (
    <>
      <PageHeader title="More" />
      <MoreContent />
    </>
  );
}

// MoreContent is the overflow menu: the /more page on direct visits, and the tab bar's drawer.
function MoreContent({ onNavigate }: { onNavigate?: () => void }) {
  const me = useMe();
  const { projects } = useLayoutData();
  const logout = useLogout();
  return (
    <>
      <div className="mb-6 overflow-hidden rounded-xl border border-border bg-card">
        <MoreRow to="/team" icon={Users} onNavigate={onNavigate}>Team</MoreRow>
        <MoreRow to="/automations" icon={Zap} onNavigate={onNavigate}>Automations</MoreRow>
        <MoreRow to="/trash" icon={Trash2} onNavigate={onNavigate}>Trash</MoreRow>
        <MoreRow to="/settings" icon={Settings} onNavigate={onNavigate}>Settings</MoreRow>
      </div>
      {projects.length > 0 && (
        <>
          <h2 className="mb-2 px-1 text-[13px] font-medium text-muted-foreground">Projects</h2>
          <div className="mb-6 overflow-hidden rounded-xl border border-border bg-card">
            {projects.map((p) => (
              <Link key={p.id} to="/projects/$id" params={{ id: p.id }} onClick={onNavigate} className="flex min-h-12 items-center justify-between border-b border-border px-4 text-[15px] last:border-0 active:bg-muted">
                <span className="truncate">{projectName(p)}</span>
                <span className="text-sm text-muted-foreground/70">{p.open_tasks}</span>
              </Link>
            ))}
          </div>
        </>
      )}
      <div className="mb-6 flex items-center justify-between rounded-xl border border-border bg-card px-4 py-3">
        <span className="text-[15px]">Appearance</span>
        <ThemeToggle />
      </div>
      <div className="flex items-center justify-between rounded-xl border border-border bg-card px-4 py-3">
        <div className="min-w-0">
          <div className="truncate font-medium">{me.data?.member?.name}</div>
          <div className="truncate text-sm text-muted-foreground">{me.data?.member?.email}</div>
        </div>
        <button onClick={logout} className="flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-foreground/70 hover:bg-accent"><LogOut className="size-4" />Sign out</button>
      </div>
    </>
  );
}

const root = createRootRoute({
  component: Shell,
  validateSearch: (s: Record<string, unknown>): { task?: string; tab?: string } => ({
    ...s,
    task: typeof s.task === "string" && s.task ? s.task : undefined,
    tab: typeof s.tab === "string" && s.tab ? s.tab : undefined,
  }),
  // Tasks used to open in a side panel at ?task=<id>; those links now land on the task's page.
  beforeLoad: ({ search }) => {
    if (search.task) throw redirect({ to: "/tasks/$id", params: { id: search.task }, replace: true });
  },
});
const r = <P extends string>(path: P, component: () => React.ReactNode) => createRoute({ getParentRoute: () => root, path, component });
const router = createRouter({
  routeTree: root.addChildren([
    r("/", MyTasksPage),
    // Ready is an agent concept (list_ready); old links land on My tasks.
    r("/ready", () => <Navigate to="/" replace />),
    r("/team", TeamPage),
    r("/notifications", NotificationsPage),
    r("/projects", ProjectsPage),
    r("/projects/$id", ProjectPage),
    r("/tasks/$id", TaskPage),
    r("/notes/$id", NotePage),
    r("/automations", AutomationsPage),
    r("/trash", TrashPage),
    r("/settings", AdminPage),
    r("/settings/agents", AgentsPage),
    r("/settings/memory", MemoryPage),
    r("/settings/apps", AppsPage),
    r("/settings/devices", DevicesPage),
    r("/settings/connections", ConnectionsPage),
    r("/settings/activity", ActivityPage),
    // Agents, Activity and Admin moved into Settings; old links still land there.
    r("/admin", () => <Navigate to="/settings" replace />),
    r("/agents", () => <Navigate to="/settings/agents" replace />),
    r("/activity", () => <Navigate to="/settings/activity" replace />),
    r("/more", MorePage),
    r("/login", LoginPage),
    r("/authorize", AuthorizePage),
  ]),
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={qc}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
