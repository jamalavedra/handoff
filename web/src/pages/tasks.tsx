import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Flag, Lock, Plus, RotateCcw } from "lucide-react";
import { ago, api, cn, type Event, type Project, type Task } from "../lib";
import { Avatar, Button, Card, DuePill, Empty, ErrorText, Input, PageHeader, Select, TaskCheck, Who } from "../components/ui";
import { useQuickAdd } from "../components/quickadd";
import { groupByDue } from "../quickadd";
import { useMe } from "../main";

export function useInvalidate() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries();
}

export function TaskRow({ t, action }: { t: Task; action?: React.ReactNode }) {
  const me = useMe();
  const invalidate = useInvalidate();
  const closed = t.status === "done" || t.status === "cancelled";
  const toggle = useMutation({
    mutationFn: () => (closed ? api.tool("update_task", { id: t.id, status: "open" }) : api.tool("close_task", { id: t.id, status: "done" })),
    onSuccess: invalidate,
  });
  const waiting = !closed && !t.ready && t.blocked_by.length > 0 && !t.claimed_by;
  const showAssignee = t.assignee && t.assignee.id !== me.data?.member?.id && t.assignee.id !== t.claimed_by?.id;
  return (
    <div>
    <div className="flex min-h-14 items-center gap-3 rounded-2xl border border-zinc-200/70 bg-white px-3 shadow-[0_1px_3px_rgba(0,0,0,0.04)] transition-colors hover:border-zinc-300 sm:min-h-12">
      <TaskCheck t={t} disabled={!!t.deleted_at || toggle.isPending} onToggle={() => toggle.mutate()} />
      <Link to="/tasks/$id" params={{ id: t.id }} className="flex min-w-0 flex-1 items-center gap-1.5 self-stretch py-2">
        <span className={cn("truncate text-[15px] font-medium sm:text-sm", closed && "text-zinc-400 line-through")}>{t.title}</span>
        {t.priority < 2 && !closed && <Flag aria-label={t.priority === 0 ? "Urgent" : "High priority"} className={cn("size-3.5 shrink-0", t.priority === 0 ? "fill-red-500 text-red-500" : "text-orange-500")} />}
      </Link>
      <div className="flex shrink-0 items-center gap-1.5">
        {t.claimed_by && !closed && (
          <span title={`${t.claimed_by.name} is working on it`} className="flex items-center gap-1.5 rounded-full bg-zinc-100 py-0.5 pr-1 pl-2 text-xs font-medium text-zinc-700 sm:pr-2">
            <span className="relative flex size-2"><span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-60", t.claimed_by.kind === "agent" ? "bg-violet-400" : "bg-amber-400")} /><span className={cn("relative inline-flex size-2 rounded-full", t.claimed_by.kind === "agent" ? "bg-violet-500" : "bg-amber-500")} /></span>
            <span className="hidden max-w-32 truncate sm:inline">{t.claimed_by.name}</span>
            <Avatar m={t.claimed_by} className="size-5 sm:hidden" />
          </span>
        )}
        {waiting && <span title={`Waits on ${t.blocked_by.join(", ")}`} className="flex items-center gap-1 rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700"><Lock className="size-3" /><span className="hidden sm:inline">Waiting</span></span>}
        {showAssignee && <Avatar m={t.assignee!} />}
        {t.due && <DuePill due={t.due} done={closed} />}
        {action}
      </div>
    </div>
    {toggle.error && <div className="px-3"><ErrorText error={toggle.error} /></div>}
    </div>
  );
}

export function TaskList({ tasks, empty, action }: { tasks?: Task[]; empty: string; action?: (t: Task) => React.ReactNode }) {
  if (!tasks) return <Card><Empty>Loading…</Empty></Card>;
  if (tasks.length === 0) return <Card><Empty>{empty}</Empty></Card>;
  return <div className="flex flex-col gap-2">{tasks.map((t) => <TaskRow key={t.id} t={t} action={action?.(t)} />)}</div>;
}

export function NewTask({ project }: { project?: string }) {
  const quick = useQuickAdd();
  return (
    <Button className="hidden md:inline-flex" onClick={() => quick.open(project)}>
      <Plus className="size-4" />New task<kbd className="ml-1 rounded border border-white/30 px-1 font-sans text-[10px] text-white/70">N</kbd>
    </Button>
  );
}

export function MyTasksPage() {
  const q = useQuery({ queryKey: ["tasks", "mine"], queryFn: allMyOpenTasks });
  const recent = useQuery({ queryKey: ["tasks", "recent-done"], queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { assignee: "me", status: ["done"], limit: 5 }) });
  const [project, setProject] = useState("");
  const tasks = q.data?.tasks;
  const chips = useMemo(() => {
    const m = new Map<string, { id: string; name: string; n: number }>();
    for (const t of tasks ?? []) {
      const c = m.get(t.project_id) ?? { id: t.project_id, name: t.project, n: 0 };
      c.n++;
      m.set(t.project_id, c);
    }
    return [...m.values()].sort((a, b) => b.n - a.n || a.name.localeCompare(b.name));
  }, [tasks]);
  const active = chips.some((c) => c.id === project) ? project : "";
  const shown = tasks?.filter((t) => !active || t.project_id === active);
  const recentShown = recent.data?.tasks.filter((t) => !active || t.project_id === active);
  return (
    <>
      <PageHeader title="My tasks"><NewTask project={active || undefined} /></PageHeader>
      {chips.length > 1 && (
        <div className="-mx-4 mb-4 flex gap-1.5 overflow-x-auto px-4 [scrollbar-width:none] md:mx-0 md:flex-wrap md:px-0" role="tablist" aria-label="Filter by project">
          <ProjectChip active={!active} onClick={() => setProject("")} name="All" n={tasks?.length ?? 0} />
          {chips.map((c) => <ProjectChip key={c.id} active={active === c.id} onClick={() => setProject(c.id)} name={c.name} n={c.n} />)}
        </div>
      )}
      {!shown || shown.length === 0 ? (
        <TaskList tasks={shown} empty="Nothing assigned to you. Agents can assign tasks to you too." />
      ) : (
        groupByDue(shown).map((s) => (
          <section key={s.key} className="mb-6" aria-label={s.title}>
            <h2 className={cn("mb-2 flex items-baseline gap-2 px-1 text-xs font-semibold uppercase tracking-wide", s.key === "overdue" ? "text-red-600" : "text-zinc-500")}>
              {s.title}<span className="font-normal text-zinc-400">{s.tasks.length}</span>
            </h2>
            <TaskList tasks={s.tasks} empty="" />
          </section>
        ))
      )}
      {!!recentShown?.length && (
        <section className="mt-8" aria-label="Recently done">
          <h2 className="mb-2 px-1 text-xs font-semibold uppercase tracking-wide text-zinc-400">Recently done</h2>
          <TaskList tasks={recentShown} empty="" />
        </section>
      )}
    </>
  );
}

// Chips and groups need the whole list, so page through search_tasks at its max page size.
async function allMyOpenTasks() {
  const page = 500;
  const tasks: Task[] = [];
  for (;;) {
    const r = await api.tool<{ tasks: Task[] }>("search_tasks", { assignee: "me", limit: page, offset: tasks.length });
    tasks.push(...r.tasks);
    if (r.tasks.length < page) return { tasks };
  }
}

function ProjectChip({ active, onClick, name, n }: { active: boolean; onClick: () => void; name: string; n: number }) {
  return (
    <button
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn("flex h-9 shrink-0 items-center gap-1.5 rounded-full border px-3.5 text-sm font-medium transition-colors md:h-8 md:px-3", active ? "border-accent bg-accent text-on-accent" : "border-zinc-200 bg-white text-zinc-700 hover:border-zinc-300")}
    >
      {name}<span className={cn("text-xs", active ? "text-white/70" : "text-zinc-400")}>{n}</span>
    </button>
  );
}

export function ReadyPage() {
  const q = useQuery({ queryKey: ["tasks", "ready"], queryFn: () => api.tool<{ tasks: Task[] }>("list_ready", { limit: 100 }) });
  return (
    <>
      <PageHeader title="Ready to start"><NewTask /></PageHeader>
      <p className="mb-3 text-sm text-zinc-500">Open, unclaimed, and nothing they wait on is unfinished. This is what agents see when they call <code>list_ready</code>.</p>
      <TaskList tasks={q.data?.tasks} empty="Nothing is ready right now." />
    </>
  );
}

export function ProjectsPage() {
  const q = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const [f, setF] = useState({ name: "", visibility: "company" });
  const invalidate = useInvalidate();
  const m = useMutation({ mutationFn: () => api.tool("create_project", f), onSuccess: () => { setF({ name: "", visibility: "company" }); invalidate(); } });
  return (
    <>
      <PageHeader title="Projects" />
      <Card className="mb-4 p-3">
        <form className="flex flex-col gap-2 sm:flex-row" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
          <Input required placeholder="New project name" value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} />
          <Select value={f.visibility} onChange={(e) => setF({ ...f, visibility: e.target.value })} aria-label="Visibility">
            <option value="company">Whole company</option>
            <option value="private">Private</option>
          </Select>
          <Button type="submit">Create</Button>
        </form>
        <ErrorText error={m.error} />
      </Card>
      <Card>
        {q.data?.projects.map((p) => (
          <Link key={p.id} to="/projects/$id" params={{ id: p.id }} className="flex items-center justify-between border-b border-zinc-100 px-4 py-3 last:border-0 hover:bg-zinc-50">
            <div>
              <div className="flex items-center gap-1.5 text-sm font-medium">{p.visibility === "private" && <Lock className="size-3.5 text-zinc-400" />}{p.name}</div>
              {p.members && <div className="text-xs text-zinc-500">{p.members.join(", ")}</div>}
            </div>
            <span className="text-sm text-zinc-500">{p.open_tasks} open</span>
          </Link>
        ))}
      </Card>
    </>
  );
}

export function ProjectPage() {
  const { id } = useParams({ strict: false }) as { id: string };
  const [showDone, setShowDone] = useState(false);
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const q = useQuery({ queryKey: ["tasks", "project", id, showDone], queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { project: id, include_done: showDone, limit: 500 }) });
  const p = projects.data?.projects.find((x) => x.id === id);
  return (
    <>
      <PageHeader title={p?.name ?? "Project"}>
        <label className="flex items-center gap-1.5 text-sm text-zinc-600"><input type="checkbox" checked={showDone} onChange={(e) => setShowDone(e.target.checked)} />Show done</label>
        <NewTask project={id} />
      </PageHeader>
      <TaskList tasks={q.data?.tasks} empty="No tasks here yet." />
    </>
  );
}

export function TrashPage() {
  const q = useQuery({ queryKey: ["tasks", "trash"], queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { trashed: true }) });
  const invalidate = useInvalidate();
  const restore = useMutation({ mutationFn: (id: string) => api.tool("restore_task", { id }), onSuccess: invalidate });
  return (
    <>
      <PageHeader title="Trash" />
      <TaskList tasks={q.data?.tasks} empty="Trash is empty." action={(t) => <Button size="sm" variant="outline" onClick={() => restore.mutate(t.id)}><RotateCcw className="size-3.5" />Restore</Button>} />
    </>
  );
}

const verbs: Record<string, string> = {
  created: "created", updated: "updated", claimed: "started working on", released: "released", closed: "closed", commented: "commented on",
  linked: "added a dependency to", unlinked: "removed a dependency from", trashed: "trashed", restored: "restored",
  approval_requested: "asked to delete", approval_approved: "approved deleting", approval_rejected: "rejected deleting",
  agent_added: "added an agent", agent_revoked: "revoked an agent", agent_permissions_changed: "changed an agent's permissions",
  api_key_created: "created an API key", api_key_revoked: "revoked an API key", project_created: "created a project",
  member_invited: "invited someone", member_changed: "changed a member", org_changed: "changed workspace settings",
};

export function EventLine({ e, showTask = true }: { e: Event; showTask?: boolean }) {
  const detail = e.kind === "closed" ? `${e.data.status}${e.data.reason ? `: ${e.data.reason}` : ""}` : e.kind === "updated" ? Object.keys(e.data).join(", ") : e.kind === "commented" ? String(e.data.excerpt ?? "") : e.kind === "linked" || e.kind === "unlinked" ? `waits on ${e.data.blocker}` : "";
  return (
    <div className="flex gap-3 py-1.5 text-sm">
      <span className="w-14 shrink-0 text-xs leading-5 text-zinc-400 sm:w-20">{ago(e.at)}</span>
      <div className="min-w-0">
        <Who m={e.actor} by={e.on_behalf_of} /> <span className="text-zinc-500">{verbs[e.kind] ?? e.kind}</span>{" "}
        {showTask && e.task_id && <Link to="/tasks/$id" params={{ id: e.task_id }} className="font-medium hover:underline">{e.task_title || e.task_id}</Link>}
        {detail && <span className="text-zinc-500"> — {detail}</span>}
        <span className="ml-2 hidden text-xs text-zinc-400 sm:inline">via {e.via}</span>
      </div>
    </div>
  );
}

export function ActivityPage() {
  const q = useQuery({ queryKey: ["activity"], queryFn: () => api.tool<{ events: Event[] }>("activity", { limit: 200 }) });
  return (
    <>
      <PageHeader title="Activity" />
      <Card className="px-4 py-2">{q.data?.events.length === 0 ? <Empty>No activity yet.</Empty> : q.data?.events.map((e) => <EventLine key={e.id} e={e} />)}</Card>
    </>
  );
}
