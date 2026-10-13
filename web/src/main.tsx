import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRootRoute, createRoute, createRouter, Link, Outlet, RouterProvider, useRouterState } from "@tanstack/react-router";
import { Activity, Bot, CheckSquare, ChevronRight, FolderKanban, Inbox, LogOut, Monitor, Moon, MoreHorizontal, Plus, Settings, Sun, Trash2, Zap } from "lucide-react";
import { setTheme, useTheme, type Theme } from "./theme";
import "./index.css";
import { api, cn, type Me, type Project } from "./lib";
import { AuthorizePage, LoginPage, SignupPage } from "./pages/auth";
import { ActivityPage, MyTasksPage, ProjectPage, ProjectsPage, ReadyPage, TrashPage } from "./pages/tasks";
import { TaskPage } from "./pages/task";
import { AgentsPage } from "./pages/agents";
import { AdminPage } from "./pages/admin";
import { QuickAddProvider, useQuickAdd } from "./components/quickadd";
import { PageHeader } from "./components/ui";

const qc = new QueryClient({ defaultOptions: { queries: { refetchInterval: 5000, retry: false, staleTime: 2000 } } });

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
  return (
    <Link to={to} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-zinc-600 hover:bg-zinc-100" activeProps={{ className: "bg-accent-soft font-medium text-accent" }} activeOptions={{ exact: to === "/" }}>
      <Icon className="size-4" />
      {children}
    </Link>
  );
}

function useLayoutData() {
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const approvals = useQuery({ queryKey: ["approvals"], queryFn: () => api.tool<{ approvals: unknown[] }>("list_approvals") });
  return { projects: projects.data?.projects ?? [], pending: approvals.data?.approvals.length ?? 0 };
}

function useLogout() {
  const qcl = useQueryClient();
  return async () => {
    await api.post("/api/logout");
    qcl.clear();
    window.location.href = "/login";
  };
}

function Badge({ n }: { n: number }) {
  return n > 0 ? <span className="rounded-full bg-amber-500 px-1.5 text-xs font-medium text-white">{n}</span> : null;
}

function Layout({ me }: { me: Me }) {
  const { projects, pending } = useLayoutData();
  const logout = useLogout();
  return (
    <QuickAddProvider>
      <div className="min-h-dvh md:flex">
        <aside className="sticky top-0 hidden h-screen w-60 shrink-0 flex-col border-r border-zinc-200 bg-white p-3 md:flex">
          <div className="mb-4 px-2">
            <div className="text-sm font-semibold">{me.org?.name}</div>
            <div className="text-xs text-zinc-500">Handoff</div>
          </div>
          <nav className="flex flex-col gap-0.5">
            <NavLink to="/" icon={CheckSquare}>My tasks</NavLink>
            <NavLink to="/ready" icon={Zap}>Ready</NavLink>
            <NavLink to="/projects" icon={FolderKanban}>Projects</NavLink>
            <NavLink to="/activity" icon={Activity}>Activity</NavLink>
            <NavLink to="/agents" icon={Bot}>
              <span className="flex-1">Agents</span>
              <Badge n={pending} />
            </NavLink>
            <NavLink to="/trash" icon={Trash2}>Trash</NavLink>
            {me.member?.role === "admin" && <NavLink to="/admin" icon={Settings}>Admin</NavLink>}
          </nav>
          <div className="mt-5 px-2 text-xs font-medium uppercase tracking-wide text-zinc-400">Projects</div>
          <div className="mt-1 flex flex-col gap-0.5 overflow-y-auto">
            {projects.map((p) => (
              <Link key={p.id} to="/projects/$id" params={{ id: p.id }} className="flex items-center justify-between rounded-md px-2 py-1 text-sm text-zinc-600 hover:bg-zinc-100" activeProps={{ className: "bg-zinc-100 text-zinc-900" }}>
                <span className="truncate">{p.name}</span>
                <span className="text-xs text-zinc-400">{p.open_tasks}</span>
              </Link>
            ))}
          </div>
          <div className="mt-auto px-2 pb-3"><ThemeToggle /></div>
          <div className="flex items-center justify-between border-t border-zinc-100 px-2 pt-3 text-sm">
            <div className="min-w-0">
              <div className="truncate font-medium">{me.member?.name}</div>
              <div className="truncate text-xs text-zinc-500">{me.member?.email}</div>
            </div>
            <button onClick={logout} className="rounded p-1 text-zinc-500 hover:bg-zinc-100" title="Sign out" aria-label="Sign out">
              <LogOut className="size-4" />
            </button>
          </div>
        </aside>
        <div className="flex min-w-0 flex-1 flex-col">
          <MobileTopBar me={me} />
          <main className="min-w-0 flex-1 px-4 pt-4 pb-[calc(6rem+env(safe-area-inset-bottom))] md:p-6 lg:px-10">
            <div className="mx-auto max-w-5xl">
              <Outlet />
            </div>
          </main>
        </div>
        <MobileTabBar pending={pending} />
      </div>
    </QuickAddProvider>
  );
}

function MobileTopBar({ me }: { me: Me }) {
  const quick = useQuickAdd();
  const path = useRouterState({ select: (s) => s.location.pathname });
  const project = path.match(/^\/projects\/([^/]+)/)?.[1];
  return (
    <header className="sticky top-0 z-20 border-b border-zinc-200/70 bg-zinc-50/90 pt-[env(safe-area-inset-top)] backdrop-blur md:hidden">
      <div className="flex h-14 items-center justify-between px-4">
        <span className="truncate rounded-full border border-zinc-200 bg-white px-3 py-1 text-sm font-semibold">{me.org?.name}</span>
        <button onClick={() => quick.open(project)} className="flex size-10 items-center justify-center rounded-full border border-zinc-200 bg-white" aria-label="New task">
          <Plus className="size-5" />
        </button>
      </div>
    </header>
  );
}

const moreRoutes = ["/more", "/projects", "/activity", "/trash", "/admin"];

function Tab({ to, icon: Icon, label, active, badge }: { to: string; icon: typeof Inbox; label: string; active: boolean; badge?: number }) {
  return (
    <Link to={to} className={cn("relative flex flex-col items-center gap-0.5 rounded-xl py-2 text-[11px] font-medium text-zinc-500", active && "bg-accent-soft text-accent")}>
      <Icon className="size-5" />
      {label}
      {!!badge && <span className="absolute top-1 right-[calc(50%-1.25rem)] rounded-full bg-amber-500 px-1.5 text-[10px] leading-4 text-white">{badge}</span>}
    </Link>
  );
}

function MobileTabBar({ pending }: { pending: number }) {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const more = moreRoutes.some((r) => path === r || path.startsWith(r + "/"));
  return (
    <nav aria-label="Main" className="fixed inset-x-3 bottom-[calc(0.75rem+env(safe-area-inset-bottom))] z-30 grid grid-cols-4 gap-1 rounded-2xl border border-zinc-200 bg-white/95 p-1 shadow-lg backdrop-blur md:hidden">
      <Tab to="/" icon={CheckSquare} label="Tasks" active={path === "/" || path.startsWith("/tasks/")} />
      <Tab to="/ready" icon={Zap} label="Ready" active={path === "/ready"} />
      <Tab to="/agents" icon={Bot} label="Agents" active={path === "/agents"} badge={pending} />
      <Tab to="/more" icon={MoreHorizontal} label="More" active={more} />
    </nav>
  );
}

function MoreRow({ to, icon: Icon, children }: { to: string; icon: typeof Inbox; children: React.ReactNode }) {
  return (
    <Link to={to} className="flex min-h-12 items-center gap-3 border-b border-zinc-100 px-4 text-[15px] last:border-0 active:bg-zinc-50">
      <Icon className="size-5 text-zinc-500" />
      <span className="flex-1">{children}</span>
      <ChevronRight className="size-4 text-zinc-300" />
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
    <div role="radiogroup" aria-label="Theme" className="inline-flex rounded-full bg-zinc-100 p-0.5">
      {themes.map(({ t, icon: Icon, label }) => (
        <button key={t} role="radio" aria-checked={theme === t} aria-label={label} title={label} onClick={() => setTheme(t)} className={cn("flex size-8 items-center justify-center rounded-full text-zinc-500 transition-colors md:size-7", theme === t && "bg-white text-zinc-900 shadow-sm")}>
          <Icon className="size-4" />
        </button>
      ))}
    </div>
  );
}

function MorePage() {
  const me = useMe();
  const { projects } = useLayoutData();
  const logout = useLogout();
  return (
    <>
      <PageHeader title="More" />
      <div className="mb-6 overflow-hidden rounded-xl border border-zinc-200 bg-white">
        <MoreRow to="/projects" icon={FolderKanban}>Projects</MoreRow>
        <MoreRow to="/activity" icon={Activity}>Activity</MoreRow>
        <MoreRow to="/trash" icon={Trash2}>Trash</MoreRow>
        {me.data?.member?.role === "admin" && <MoreRow to="/admin" icon={Settings}>Admin</MoreRow>}
      </div>
      {projects.length > 0 && (
        <>
          <h2 className="mb-2 px-1 text-xs font-medium uppercase tracking-wide text-zinc-400">Projects</h2>
          <div className="mb-6 overflow-hidden rounded-xl border border-zinc-200 bg-white">
            {projects.map((p) => (
              <Link key={p.id} to="/projects/$id" params={{ id: p.id }} className="flex min-h-12 items-center justify-between border-b border-zinc-100 px-4 text-[15px] last:border-0 active:bg-zinc-50">
                <span className="truncate">{p.name}</span>
                <span className="text-sm text-zinc-400">{p.open_tasks}</span>
              </Link>
            ))}
          </div>
        </>
      )}
      <div className="mb-6 flex items-center justify-between rounded-xl border border-zinc-200 bg-white px-4 py-3">
        <span className="text-[15px]">Appearance</span>
        <ThemeToggle />
      </div>
      <div className="flex items-center justify-between rounded-xl border border-zinc-200 bg-white px-4 py-3">
        <div className="min-w-0">
          <div className="truncate font-medium">{me.data?.member?.name}</div>
          <div className="truncate text-sm text-zinc-500">{me.data?.member?.email}</div>
        </div>
        <button onClick={logout} className="flex items-center gap-1.5 rounded-md px-2 py-1.5 text-sm text-zinc-600 hover:bg-zinc-100"><LogOut className="size-4" />Sign out</button>
      </div>
    </>
  );
}

const root = createRootRoute({ component: Shell });
const r = <P extends string>(path: P, component: () => React.ReactNode) => createRoute({ getParentRoute: () => root, path, component });
const router = createRouter({
  routeTree: root.addChildren([
    r("/", MyTasksPage),
    r("/ready", ReadyPage),
    r("/projects", ProjectsPage),
    r("/projects/$id", ProjectPage),
    r("/tasks/$id", TaskPage),
    r("/activity", ActivityPage),
    r("/agents", AgentsPage),
    r("/trash", TrashPage),
    r("/admin", AdminPage),
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
