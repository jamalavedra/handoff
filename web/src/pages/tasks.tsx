import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Bot, CircleAlert, Flag, Lock, Plus, Repeat as RepeatIcon, RotateCcw } from "lucide-react";
import { ago, api, cn, projectName, type Event, type Project, type Task } from "../lib";
import { EmojiPicker } from "../components/emoji";
import { Avatar, Card, DuePill, Empty, ErrorText, PageHeader, TaskCheck, Who } from "../components/app";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { useQuickAdd } from "../components/quickadd";
import { agentOnIt, groupByDue } from "../quickadd";
import { useMe } from "../main";
import { SettingsHeader } from "./settings";
import { Board, useView, ViewToggle } from "./board";
import { TaskLink, usePanelTask } from "../components/tasklink";
import { completedText, patchTask, reopen, undoable, undoClose, useChangeFlash, useTriageReveal } from "../components/feedback";
import { Skeleton } from "@/components/ui/skeleton";

export function useInvalidate() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries();
}

export function TaskRow({ t, action }: { t: Task; action?: React.ReactNode }) {
  const me = useMe();
  const invalidate = useInvalidate();
  const qc = useQueryClient();
  const closed = t.status === "done" || t.status === "cancelled";
  // The checkbox answers immediately; the server catches up, and Undo reopens.
  // `wasClosed` is the state at click time: the optimistic patch flips `closed` before onSuccess runs.
  const toggle = useMutation({
    mutationFn: (wasClosed: boolean) => (wasClosed ? reopen(t.id) : api.tool<{ next?: Task }>("close_task", { id: t.id, status: "done" })),
    onMutate: (wasClosed) => patchTask(qc, t.id, { status: wasClosed ? "open" : "done" }),
    onError: (_e, _v, rollback) => rollback?.(),
    onSuccess: (r, wasClosed) => {
      if (wasClosed) return;
      const next = (r as { next?: Task }).next;
      undoable(qc, completedText(`Completed “${t.title}”`, next), () => undoClose(t.id, next));
    },
    onSettled: invalidate,
  });
  const flash = useChangeFlash(t.id, t.updated_at);
  const reveal = useTriageReveal(t.triage);
  const agentWorking = agentOnIt(t);
  const waiting = !closed && !t.ready && t.blocked_by.length > 0 && !t.claimed_by;
  const showAssignee = t.assignee && t.assignee.id !== me.data?.member?.id && t.assignee.id !== t.claimed_by?.id;
  const selected = usePanelTask() === t.id;
  return (
    <div>
    <div data-task-row className={cn("flex min-h-14 items-center gap-3 rounded-2xl border border-border bg-card px-3 transition-colors hover:bg-muted/60 md:min-h-11 md:rounded-xl md:border-transparent md:bg-transparent md:hover:bg-muted data-[kbd-focus]:bg-muted data-[kbd-focus]:ring-1 data-[kbd-focus]:ring-ring", flash && "change-flash", agentWorking && "agent-border [--agent-border-offset:1px]", selected && "border-ring ring-1 ring-ring md:bg-accent md:ring-0 md:hover:bg-accent")}>
      <TaskCheck t={t} disabled={!!t.deleted_at || toggle.isPending} onToggle={() => toggle.mutate(closed)} />
      <TaskLink id={t.id} className="flex min-w-0 flex-1 flex-col justify-center self-stretch py-2">
        <span className="flex min-w-0 items-center gap-1.5">
        {t.unseen && !closed && !selected && <span role="img" aria-label="Changed by someone else" title="Changed by someone else since you last opened it" className="size-2 shrink-0 rounded-full bg-unseen" />}
        <span title={t.triage === "pending" ? "The triage agent is looking at this task" : undefined} className={cn("truncate text-[15px] font-medium sm:text-sm", closed && "text-muted-foreground/70 line-through", t.triage === "pending" && "agent-shimmer", reveal && "triage-reveal")}>{t.title}</span>
        {t.triage === "failed" && <span title={`Triage failed: ${t.triage_error ?? ""}`} className="shrink-0 text-amber-600"><CircleAlert aria-label="Triage failed" className="size-3.5" /></span>}
        {t.priority < 2 && !closed && <Flag aria-label={t.priority === 0 ? "Urgent" : "High priority"} className={cn("size-3.5 shrink-0", t.priority === 0 ? "fill-red-500 text-red-500" : "text-orange-500")} />}
        </span>
        {t.agent_note && !closed && t.triage !== "pending" && (
          <span className="mt-0.5 flex min-w-0 items-center gap-1 text-[13px] text-muted-foreground">
            <Bot aria-hidden className="size-3 shrink-0 text-violet-500" />
            <span className="truncate">{t.agent_note.split("\n")[0]}</span>
          </span>
        )}
      </TaskLink>
      <div className="flex shrink-0 items-center gap-1.5">
        {t.claimed_by && !closed && (
          <span title={`${t.claimed_by.name} is working on it`} className="flex items-center gap-1.5 rounded-full bg-accent py-0.5 pr-1 pl-2 text-xs font-medium text-foreground/80 sm:pr-2">
            <span className="relative flex size-2"><span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-60", t.claimed_by.kind === "agent" ? "bg-violet-400" : "bg-amber-400")} /><span className={cn("relative inline-flex size-2 rounded-full", t.claimed_by.kind === "agent" ? "bg-violet-500" : "bg-amber-500")} /></span>
            <span className="hidden max-w-32 truncate sm:inline">{t.claimed_by.name}</span>
            <Avatar m={t.claimed_by} className="size-5 sm:hidden" />
          </span>
        )}
        {waiting && <span title={`Waits on ${t.blocked_by.join(", ")}`} className="flex items-center gap-1 rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700"><Lock className="size-3" /><span className="hidden sm:inline">Waiting</span></span>}
        {showAssignee && <Avatar m={t.assignee!} />}
        {t.repeat && !closed && <RepeatIcon aria-label={`Repeats: ${t.repeat}`} className="size-3.5 shrink-0 text-muted-foreground" />}
        {t.due && <DuePill due={t.due} done={closed} />}
        {action}
      </div>
    </div>
    {toggle.error && <div className="px-3"><ErrorText error={toggle.error} /></div>}
    </div>
  );
}

export function TaskList({ tasks, empty, action }: { tasks?: Task[]; empty: string; action?: (t: Task) => React.ReactNode }) {
  if (!tasks) return <TaskListSkeleton />;
  if (tasks.length === 0) return <Card><Empty>{empty}</Empty></Card>;
  return <div className="flex flex-col gap-2 md:gap-0.5">{tasks.map((t) => <TaskRow key={t.id} t={t} action={action?.(t)} />)}</div>;
}

function TaskListSkeleton() {
  return (
    <div className="flex flex-col gap-2 md:gap-0.5" aria-busy="true" aria-label="Loading tasks">
      {[62, 48, 71, 40].map((w) => (
        <div key={w} className="flex min-h-14 items-center gap-3 rounded-2xl border border-border px-4 md:min-h-11 md:border-transparent md:px-3">
          <Skeleton className="size-5 rounded-full" />
          <Skeleton className="h-3.5 rounded-full" style={{ width: `${w}%` }} />
        </div>
      ))}
    </div>
  );
}

export function NewTask({ project }: { project?: string }) {
  const quick = useQuickAdd();
  return (
    <Button variant="secondary" size="sm" className="hidden text-[13px] md:inline-flex" onClick={() => quick.open(project)}>
      <Plus className="size-4" />New task<kbd className="ml-0.5 rounded border border-border px-1 font-sans text-[10px] text-muted-foreground">N</kbd>
    </Button>
  );
}

export function MyTasksPage() {
  const [view, setView] = useView();
  const q = useQuery({ queryKey: ["tasks", "mine"], queryFn: allMyOpenTasks });
  const recent = useQuery({ queryKey: ["tasks", "recent-done"], queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { assignee: "me", status: ["done", "cancelled"], sort: "recent", limit: 20 }) });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const emojiOf = (id: string) => projects.data?.projects.find((p) => p.id === id)?.emoji;
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
      <PageHeader title="My tasks"><ViewToggle view={view} onChange={setView} /><NewTask project={active || undefined} /></PageHeader>
      {chips.length > 1 && (
        <div className="-mx-4 mb-4 flex gap-1.5 overflow-x-auto px-4 [scrollbar-width:none] md:mx-0 md:flex-wrap md:px-0" role="tablist" aria-label="Filter by project">
          <ProjectChip active={!active} onClick={() => setProject("")} name="All" n={tasks?.length ?? 0} />
          {chips.map((c) => <ProjectChip key={c.id} active={active === c.id} onClick={() => setProject(c.id)} name={projectName({ name: c.name, emoji: emojiOf(c.id) })} n={c.n} />)}
        </div>
      )}
      {view === "board" ? (
        <Board tasks={shown} done={recentShown} />
      ) : !shown || shown.length === 0 ? (
        <TaskList tasks={shown} empty="Nothing assigned to you. Agents can assign tasks to you too." />
      ) : (
        groupByDue(shown).map((s) => (
          <section key={s.key} className="mb-6" aria-label={s.title}>
            <h2 className={cn("mb-2 flex items-baseline gap-2 px-1 text-[13px] font-medium", s.key === "overdue" ? "text-red-600" : "text-muted-foreground")}>
              {s.title}<span className="text-foreground/35 tabular-nums">{s.tasks.length}</span>
            </h2>
            <TaskList tasks={s.tasks} empty="" />
          </section>
        ))
      )}
      {view === "list" && !!recentShown?.length && (
        <section className="mt-8" aria-label="Recently done">
          <h2 className="mb-2 px-1 text-[13px] font-medium text-muted-foreground">Recently done</h2>
          <TaskList tasks={recentShown.slice(0, 5)} empty="" />
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
      className={cn("flex h-8 shrink-0 select-none items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 text-[13px] font-medium transition-colors", active ? "bg-accent text-foreground" : "text-foreground/50 hover:text-foreground/80")}
    >
      {name}<span className={cn("tabular-nums", active ? "text-foreground/40" : "text-foreground/35")}>{n}</span>
    </button>
  );
}

export function ProjectsPage() {
  const q = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const [f, setF] = useState({ name: "", emoji: "", visibility: "company" });
  const invalidate = useInvalidate();
  const m = useMutation({ mutationFn: () => api.tool("create_project", f), onSuccess: () => { setF({ name: "", emoji: "", visibility: "company" }); invalidate(); } });
  return (
    <>
      <PageHeader title="Projects" />
      <Card className="mb-4 p-3">
        <form className="flex flex-col gap-2 sm:flex-row" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
          <EmojiPicker value={f.emoji} onChange={(emoji) => setF({ ...f, emoji })} label="New project emoji" />
          <Input required placeholder="New project name" value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} />
          <NativeSelect value={f.visibility} onChange={(e) => setF({ ...f, visibility: e.target.value })} aria-label="Visibility">
            <option value="company">Whole company</option>
            <option value="private">Private</option>
          </NativeSelect>
          <Button type="submit">Create</Button>
        </form>
        <ErrorText error={m.error} />
      </Card>
      <Card>
        {q.data?.projects.map((p) => (
          <Link key={p.id} to="/projects/$id" params={{ id: p.id }} className="flex items-center justify-between border-b border-border px-4 py-3 last:border-0 hover:bg-muted">
            <div>
              <div className="flex items-center gap-1.5 text-sm font-medium">{p.visibility === "private" && <Lock className="size-3.5 text-muted-foreground/70" />}{projectName(p)}</div>
              {p.members && <div className="text-xs text-muted-foreground">{p.members.join(", ")}</div>}
            </div>
            <span className="text-sm text-muted-foreground">{p.open_tasks} open</span>
          </Link>
        ))}
      </Card>
    </>
  );
}

export function ProjectPage() {
  const { id } = useParams({ strict: false }) as { id: string };
  const [showDone, setShowDone] = useState(false);
  const [view, setView] = useView();
  const done = useQuery({ queryKey: ["tasks", "project", id, "done"], enabled: view === "board", queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { project: id, status: ["done", "cancelled"], sort: "recent", limit: 20 }) });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const q = useQuery({ queryKey: ["tasks", "project", id, showDone], queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { project: id, include_done: showDone, limit: 500 }) });
  const p = projects.data?.projects.find((x) => x.id === id);
  const invalidate = useInvalidate();
  const setEmoji = useMutation({ mutationFn: (emoji: string) => api.tool("update_project", { project: id, emoji }), onSuccess: invalidate });
  return (
    <>
      <PageHeader title={p ? projectName(p) : "Project"}>
        {p && <EmojiPicker value={p.emoji ?? ""} onChange={(emoji) => setEmoji.mutate(emoji)} label="Project emoji" />}
        {view === "list" && <label className="flex items-center gap-1.5 text-[13px] text-foreground/70"><input type="checkbox" checked={showDone} onChange={(e) => setShowDone(e.target.checked)} />Show done</label>}
        <ViewToggle view={view} onChange={setView} />
        <NewTask project={id} />
      </PageHeader>
      <ErrorText error={setEmoji.error} />
      {view === "board" ? <Board tasks={q.data?.tasks.filter((t) => t.status !== "done" && t.status !== "cancelled")} done={done.data?.tasks} /> : <TaskList tasks={q.data?.tasks} empty="No tasks here yet." />}
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
  triaged: "triaged", triage_failed: "couldn't triage", repeat_ended: "ended the repeating series of",
  member_invited: "invited someone", member_changed: "changed a member", org_changed: "changed workspace settings", project_changed: "changed a project",
};

export function EventLine({ e, showTask = true, onRestore }: { e: Event; showTask?: boolean; onRestore?: (body: string) => void }) {
  const previous = e.kind === "updated" && typeof e.data.previous_body === "string" ? (e.data.previous_body as string) : undefined;
  const detail = e.kind === "closed" ? `${e.data.status}${e.data.reason ? `: ${e.data.reason}` : ""}` : e.kind === "updated" ? Object.keys(e.data).filter((k) => k !== "from_project_id" && k !== "previous_body").map((k) => (k === "body" ? "details" : k)).join(", ") : e.kind === "commented" ? String(e.data.excerpt ?? "") : e.kind === "triaged" ? ((e.data.changed as string[] | undefined) ?? []).join(", ") : e.kind === "triage_failed" ? String(e.data.error ?? "") : e.kind === "linked" || e.kind === "unlinked" ? `waits on ${e.data.blocker}` : "";
  return (
    <div className="flex gap-3 py-1.5 text-sm">
      <span className="w-14 shrink-0 text-xs leading-5 text-muted-foreground/70 sm:w-20">{ago(e.at)}</span>
      <div className="min-w-0">
        <Who m={e.actor} by={e.on_behalf_of} /> <span className="text-muted-foreground">{verbs[e.kind] ?? e.kind}</span>{" "}
        {showTask && e.task_id && <TaskLink id={e.task_id} className="font-medium hover:underline">{e.task_title || e.task_id}</TaskLink>}
        {detail && <span className="text-muted-foreground"> — {detail}</span>}
        <span className="ml-2 hidden text-xs text-muted-foreground/70 sm:inline">via {e.via}</span>
        {previous && onRestore && <button onClick={() => onRestore(previous)} className="ml-2 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline">Restore previous version</button>}
      </div>
    </div>
  );
}

export function ActivityPage() {
  const q = useQuery({ queryKey: ["activity"], queryFn: () => api.tool<{ events: Event[] }>("activity", { limit: 200 }) });
  return (
    <>
      <SettingsHeader active="activity" />
      <Card className="px-4 py-2">{q.data?.events.length === 0 ? <Empty>No activity yet.</Empty> : q.data?.events.map((e) => <EventLine key={e.id} e={e} />)}</Card>
    </>
  );
}
