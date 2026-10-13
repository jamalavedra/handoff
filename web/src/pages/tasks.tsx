import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { Lock, Plus, RotateCcw } from "lucide-react";
import { ago, api, type Event, type Member, type Project, type Task } from "../lib";
import { Badge, Button, Card, Empty, ErrorText, Input, PageHeader, Priority, Select, StatusBadge, Textarea, Who } from "../components/ui";

export function useInvalidate() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries();
}

export function TaskRow({ t, action }: { t: Task; action?: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3 border-b border-zinc-100 px-4 py-2.5 last:border-0 hover:bg-zinc-50">
      <Priority p={t.priority} />
      <Link to="/tasks/$id" params={{ id: t.id }} className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{t.title}</div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-zinc-500">
          <span className="font-mono">{t.id}</span>
          <span>{t.project}</span>
          {t.assignee && <Who m={t.assignee} />}
          {t.claimed_by && <span className="text-blue-700">working: {t.claimed_by.name}</span>}
          {t.blocked_by.length > 0 && !t.ready && t.status !== "done" && <span className="text-amber-700">waits on {t.blocked_by.join(", ")}</span>}
          {t.due && <span>due {t.due}</span>}
          {t.labels.map((l) => <Badge key={l}>{l}</Badge>)}
        </div>
      </Link>
      <StatusBadge status={t.status} />
      <span className="hidden w-16 text-right text-xs text-zinc-400 sm:block">{ago(t.updated_at)}</span>
      {action}
    </div>
  );
}

export function TaskList({ tasks, empty, action }: { tasks?: Task[]; empty: string; action?: (t: Task) => React.ReactNode }) {
  if (!tasks) return <Card><Empty>Loading…</Empty></Card>;
  return <Card>{tasks.length === 0 ? <Empty>{empty}</Empty> : tasks.map((t) => <TaskRow key={t.id} t={t} action={action?.(t)} />)}</Card>;
}

export function NewTask({ project }: { project?: string }) {
  const [open, setOpen] = useState(false);
  const [f, setF] = useState({ title: "", body: "", assignee: "", priority: "2", due: "", project: project ?? "" });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const invalidate = useInvalidate();
  const m = useMutation({
    mutationFn: () =>
      api.tool("create_tasks", { tasks: [{ title: f.title, body: f.body, assignee: f.assignee || undefined, priority: Number(f.priority), due: f.due || undefined, project: f.project || undefined }] }),
    onSuccess: () => {
      setF({ ...f, title: "", body: "" });
      setOpen(false);
      invalidate();
    },
  });
  if (!open) return <Button onClick={() => setOpen(true)}><Plus className="size-4" />New task</Button>;
  return (
    <div className="fixed inset-0 z-10 flex items-start justify-center bg-black/20 p-4 pt-24" onClick={() => setOpen(false)}>
      <Card className="w-full max-w-lg p-4" onClick={(e) => e.stopPropagation()}>
        <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
          <Input autoFocus required placeholder="What needs doing?" value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} aria-label="Title" />
          <Textarea rows={3} placeholder="Details (optional)" value={f.body} onChange={(e) => setF({ ...f, body: e.target.value })} />
          <div className="flex flex-wrap gap-2">
            <Select value={f.project} onChange={(e) => setF({ ...f, project: e.target.value })} aria-label="Project">
              <option value="">Inbox</option>
              {projects.data?.projects.filter((p) => p.name !== "Inbox").map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </Select>
            <Select value={f.assignee} onChange={(e) => setF({ ...f, assignee: e.target.value })} aria-label="Assignee">
              <option value="">Unassigned</option>
              {members.data?.members.map((x) => <option key={x.id} value={x.id}>{x.name}{x.kind === "agent" ? " (agent)" : ""}</option>)}
            </Select>
            <Select value={f.priority} onChange={(e) => setF({ ...f, priority: e.target.value })} aria-label="Priority">
              {[0, 1, 2, 3, 4].map((p) => <option key={p} value={p}>P{p}</option>)}
            </Select>
            <Input type="date" className="w-auto" value={f.due} onChange={(e) => setF({ ...f, due: e.target.value })} aria-label="Due" />
          </div>
          <ErrorText error={m.error} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" disabled={m.isPending}>Create</Button>
          </div>
        </form>
      </Card>
    </div>
  );
}

export function MyTasksPage() {
  const q = useQuery({ queryKey: ["tasks", "mine"], queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { assignee: "me" }) });
  const recent = useQuery({ queryKey: ["tasks", "recent-done"], queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { assignee: "me", status: ["done"], limit: 5 }) });
  return (
    <>
      <PageHeader title="My tasks"><NewTask /></PageHeader>
      <TaskList tasks={q.data?.tasks} empty="Nothing assigned to you. Agents can assign tasks to you too." />
      {!!recent.data?.tasks.length && (
        <>
          <h2 className="mt-8 mb-2 text-sm font-medium text-zinc-500">Recently done</h2>
          <TaskList tasks={recent.data.tasks} empty="" />
        </>
      )}
    </>
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
        <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); m.mutate(); }}>
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
      <span className="w-20 shrink-0 text-xs leading-5 text-zinc-400">{ago(e.at)}</span>
      <div className="min-w-0">
        <Who m={e.actor} by={e.on_behalf_of} /> <span className="text-zinc-500">{verbs[e.kind] ?? e.kind}</span>{" "}
        {showTask && e.task_id && <Link to="/tasks/$id" params={{ id: e.task_id }} className="font-medium hover:underline">{e.task_title || e.task_id}</Link>}
        {detail && <span className="text-zinc-500"> — {detail}</span>}
        <span className="ml-2 text-xs text-zinc-400">via {e.via}</span>
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
