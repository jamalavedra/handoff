import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider, useQuery, useQueryClient } from "@tanstack/react-query";
import { createRootRoute, createRoute, createRouter, Link, Outlet, RouterProvider, useRouterState } from "@tanstack/react-router";
import { Activity, Bot, CheckSquare, FolderKanban, Inbox, LogOut, Settings, Trash2, Zap } from "lucide-react";
import "./index.css";
import { api, cn, type Me, type Project } from "./lib";
import { AuthorizePage, LoginPage, SignupPage } from "./pages/auth";
import { ActivityPage, MyTasksPage, ProjectPage, ProjectsPage, ReadyPage, TrashPage } from "./pages/tasks";
import { TaskPage } from "./pages/task";
import { AgentsPage } from "./pages/agents";
import { AdminPage } from "./pages/admin";

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
    <Link to={to} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-zinc-600 hover:bg-zinc-100" activeProps={{ className: "bg-zinc-100 font-medium text-zinc-900" }} activeOptions={{ exact: to === "/" }}>
      <Icon className="size-4" />
      {children}
    </Link>
  );
}

function Layout({ me }: { me: Me }) {
  const qcl = useQueryClient();
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const approvals = useQuery({ queryKey: ["approvals"], queryFn: () => api.tool<{ approvals: unknown[] }>("list_approvals") });
  const logout = async () => {
    await api.post("/api/logout");
    qcl.clear();
    window.location.href = "/login";
  };
  const pending = approvals.data?.approvals.length ?? 0;
  return (
    <div className="flex min-h-screen">
      <aside className="sticky top-0 flex h-screen w-60 shrink-0 flex-col border-r border-zinc-200 bg-white p-3">
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
            {pending > 0 && <span className="rounded-full bg-amber-500 px-1.5 text-xs font-medium text-white">{pending}</span>}
          </NavLink>
          <NavLink to="/trash" icon={Trash2}>Trash</NavLink>
          {me.member?.role === "admin" && <NavLink to="/admin" icon={Settings}>Admin</NavLink>}
        </nav>
        <div className="mt-5 px-2 text-xs font-medium uppercase tracking-wide text-zinc-400">Projects</div>
        <div className="mt-1 flex flex-col gap-0.5 overflow-y-auto">
          {projects.data?.projects.map((p) => (
            <Link key={p.id} to="/projects/$id" params={{ id: p.id }} className="flex items-center justify-between rounded-md px-2 py-1 text-sm text-zinc-600 hover:bg-zinc-100" activeProps={{ className: "bg-zinc-100 text-zinc-900" }}>
              <span className="truncate">{p.name}</span>
              <span className="text-xs text-zinc-400">{p.open_tasks}</span>
            </Link>
          ))}
        </div>
        <div className="mt-auto flex items-center justify-between border-t border-zinc-100 px-2 pt-3 text-sm">
          <div className="min-w-0">
            <div className="truncate font-medium">{me.member?.name}</div>
            <div className="truncate text-xs text-zinc-500">{me.member?.email}</div>
          </div>
          <button onClick={logout} className="rounded p-1 text-zinc-500 hover:bg-zinc-100" title="Sign out" aria-label="Sign out">
            <LogOut className="size-4" />
          </button>
        </div>
      </aside>
      <main className={cn("min-w-0 flex-1 p-6 lg:px-10")}>
        <div className="mx-auto max-w-5xl">
          <Outlet />
        </div>
      </main>
    </div>
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
