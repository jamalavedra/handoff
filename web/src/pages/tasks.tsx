import { createContext, useContext, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { Bot, Check, CircleAlert, Flag, ListChecks, Lock, MessageCircleQuestion, Repeat as RepeatIcon, RotateCcw } from "lucide-react";
import { ago, api, cn, projectName, type Event, type Project, type Task } from "../lib";
import { EmojiPicker } from "../components/emoji";
import { Avatar, Card, DuePill, Empty, ErrorText, PageHeader, TaskCheck, Who } from "../components/app";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Textarea } from "@/components/ui/textarea";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { InlineNewTask } from "../components/quickadd";
import { agentOnIt, isoDate } from "../quickadd";
import { arrange, emptyFilter, filterTasks, type ListPrefs, type Prop, type Rows } from "../tasklist";
import { TaskToolbar, useListPrefs } from "../components/task-toolbar";
import { useMe } from "../main";
import { SettingsHeader } from "./settings";
import { Board, useView } from "./board";
import { TaskLink } from "../components/tasklink";
import { completedText, patchTask, reopen, undoable, undoClose, useChangeFlash, useTriageReveal } from "../components/feedback";
import { Skeleton } from "@/components/ui/skeleton";
import { progressLabel } from "../todos";
import { eventDetail, verb } from "../events";
import { NewNote, ProjectNotes, useProjectNotes } from "./notes";
import { useSelection } from "../hooks/use-selection";

export function useInvalidate() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries();
}

// How rows look, from the list's view settings: compact or comfortable, and which properties show.
// Lists outside a toolbar (trash, board) get the defaults.
const RowLook = createContext<{ rows: Rows; hidden: Prop[] }>({ rows: "comfortable", hidden: [] });

export function TaskRow({ t, action }: { t: Task; action?: React.ReactNode }) {
  const me = useMe();
  const look = useContext(RowLook);
  const shows = (p: Prop) => !look.hidden.includes(p);
  const compact = look.rows === "compact";
  const invalidate = useInvalidate();
  const qc = useQueryClient();
  const closed = t.status === "done" || t.status === "cancelled";
  // The checkbox answers immediately; the server catches up, and Undo reopens.
  // `wasClosed` is the state at click time: the optimistic patch flips `closed` before onSuccess runs.
  const toggle = useMutation({
    mutationFn: (wasClosed: boolean) => (wasClosed ? reopen(t.id) : api.tool<{ next?: Task }>("close_task", { id: t.id, status: "done", today: isoDate(new Date()) })),
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
  const select = useSelection();
  const selected = select.ids.includes(t.id);
  return (
    <div>
    <div data-task-row {...select.row(t.id, !t.deleted_at)} className={cn("flex items-center gap-3 rounded-2xl border border-border bg-card px-3 transition-colors hover:bg-muted/60 md:rounded-xl md:border-transparent md:bg-transparent md:hover:bg-muted data-[kbd-focus]:bg-muted data-[kbd-focus]:ring-1 data-[kbd-focus]:ring-ring data-[selected]:bg-blue-50 data-[selected]:ring-1 data-[selected]:ring-blue-300 md:data-[selected]:bg-blue-50", compact ? "min-h-11 md:min-h-9" : "min-h-14 md:min-h-11", flash && "change-flash", agentWorking && "agent-border [--agent-border-offset:1px]")}>
      <TaskCheck t={t} disabled={!!t.deleted_at || toggle.isPending} onToggle={() => toggle.mutate(closed)} />
      <TaskLink id={t.id} className={cn("flex min-w-0 flex-1 flex-col justify-center self-stretch", compact ? "py-1" : "py-2")}>
        <span className="flex min-w-0 items-center gap-1.5">
        {t.unseen && !closed && <span role="img" aria-label="Changed by someone else" title="Changed by someone else since you last opened it" className="size-2 shrink-0 rounded-full bg-unseen" />}
        <span title={t.triage === "pending" ? "The triage agent is looking at this task" : undefined} className={cn("truncate text-[15px] font-medium sm:text-sm", closed && "text-muted-foreground/70 line-through", t.triage === "pending" && "agent-shimmer", reveal && "triage-reveal")}>{t.title}</span>
        {t.triage === "failed" && <span title={`Triage failed: ${t.triage_error ?? ""}`} className="shrink-0 text-amber-600"><CircleAlert aria-label="Triage failed" className="size-3.5" /></span>}
        {t.priority < 2 && !closed && <Flag aria-label={t.priority === 0 ? "Urgent" : "High priority"} className={cn("size-3.5 shrink-0", t.priority === 0 ? "fill-red-500 text-red-500" : "text-orange-500")} />}
        </span>
        {shows("note") && !compact && t.agent_note && !closed && t.triage !== "pending" && (
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
        {t.waiting_on && !closed && (
          <span title={`${t.assignee?.kind === "agent" ? t.assignee.name : "The agent"} needs ${t.waiting_on.id === me.data?.member?.id ? "your" : `${t.waiting_on.name}'s`} input to go on`} className="flex items-center gap-1 rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-500/15 dark:text-amber-400">
            <MessageCircleQuestion className="size-3" /><span className="hidden sm:inline">{t.waiting_on.id === me.data?.member?.id ? "Needs you" : `Needs ${t.waiting_on.name.split(" ")[0]}`}</span>
          </span>
        )}
        {waiting && <span title={`Waits on ${t.blocked_by.join(", ")}`} className="flex items-center gap-1 rounded-full bg-amber-50 px-2 py-0.5 text-xs font-medium text-amber-700"><Lock className="size-3" /><span className="hidden sm:inline">Waiting</span></span>}
        {shows("assignee") && showAssignee && <Avatar m={t.assignee!} />}
        {shows("repeat") && t.repeat && !closed && <RepeatIcon aria-label={`Repeats: ${t.repeat}`} className="size-3.5 shrink-0 text-muted-foreground" />}
        {shows("todos") && progressLabel(t.todo_progress) && (
          <span title={`${t.todo_progress!.done} of ${t.todo_progress!.total} todos done`} className={cn("flex shrink-0 items-center gap-1 text-xs tabular-nums", t.todo_progress!.done === t.todo_progress!.total ? "text-green-700 dark:text-green-400" : "text-muted-foreground")}>
            <ListChecks aria-hidden className="size-3.5" />{progressLabel(t.todo_progress)}
          </span>
        )}
        {shows("due") && t.due && <DuePill due={t.due} done={closed} />}
        {action}
        {selected && <Check aria-label="Selected" className="size-4 shrink-0 text-blue-600" />}
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

export function MyTasksPage() {
  const [view, setView] = useView();
  const [prefs, setPrefs] = useListPrefs();
  const q = useQuery({ queryKey: ["tasks", "mine"], queryFn: allMyOpenTasks });
  const recent = useQuery({ queryKey: ["tasks", "recent-done"], queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { assignee: "me", status: ["done"], sort: "recent", limit: 20 }) });
  const [query, setQuery] = useState("");
  const board = view === "board";
  const completed = recent.data && filterTasks(recent.data.tasks, prefs.filter, undefined, query);
  return (
    <RowLook.Provider value={prefs}>
      <PageHeader title="My tasks"><TaskToolbar prefs={prefs} onChange={setPrefs} query={query} onQuery={setQuery} view={view} onView={setView} link="/" /></PageHeader>
      <InlineNewTask />
      {board ? (
        <Board tasks={q.data && filterTasks(q.data.tasks, prefs.filter, undefined, query)} done={completed} />
      ) : (
        <ArrangedList tasks={q.data?.tasks} prefs={prefs} query={query} onClear={() => { setPrefs({ filter: emptyFilter }); setQuery(""); }} empty="Nothing assigned to you. Agents can assign tasks to you too." />
      )}
      {!board && prefs.showDone && !!completed?.length && (
        <section className="mt-8" aria-label="Completed">
          <h2 className="mb-2 px-1 text-[13px] font-medium text-muted-foreground">Completed</h2>
          <TaskList tasks={completed} empty="" />
        </section>
      )}
    </RowLook.Provider>
  );
}

// ArrangedList is a task list in the order, filter and grouping picked in the toolbar. Flat unless
// grouping by due date is on.
function ArrangedList({ tasks, prefs, query, empty, onClear }: { tasks?: Task[]; prefs: ListPrefs; query: string; empty: string; onClear: () => void }) {
  if (!tasks || tasks.length === 0) return <TaskList tasks={tasks} empty={empty} />;
  const sections = arrange(tasks, prefs, undefined, query);
  if (!sections.length) {
    return <Card><Empty>{query ? `No tasks match “${query}”.` : "No tasks match these filters."} <button className="underline underline-offset-2 hover:text-foreground" onClick={onClear}>{query ? "Clear search" : "Clear filters"}</button></Empty></Card>;
  }
  if (!prefs.group) return <TaskList tasks={sections[0].tasks} empty="" />;
  return sections.map((s) => (
    <section key={s.key} className="mb-6" aria-label={s.title}>
      <h2 className={cn("mb-2 flex items-baseline gap-2 px-1 text-[13px] font-medium", s.key === "overdue" ? "text-red-600" : "text-muted-foreground")}>
        {s.title}<span className="text-foreground/35 tabular-nums">{s.tasks.length}</span>
      </h2>
      <TaskList tasks={s.tasks} empty="" />
    </section>
  ));
}

// Search, sort and filter run over the whole list, so page through search_tasks at its max page size.
async function allTasks(query: Record<string, unknown>) {
  const page = 500;
  const tasks: Task[] = [];
  for (;;) {
    const r = await api.tool<{ tasks: Task[] }>("search_tasks", { ...query, limit: page, offset: tasks.length });
    tasks.push(...r.tasks);
    if (r.tasks.length < page) return { tasks };
  }
}
const allMyOpenTasks = () => allTasks({ assignee: "me", sort: "recent" });

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
  const [prefs, setPrefs] = useListPrefs();
  const showDone = prefs.showDone;
  const [view, setView] = useView();
  const done = useQuery({ queryKey: ["tasks", "project", id, "done"], enabled: view === "board", queryFn: () => api.tool<{ tasks: Task[] }>("search_tasks", { project: id, status: ["done"], sort: "recent", limit: 20 }) });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const q = useQuery({ queryKey: ["tasks", "project", id, showDone], queryFn: () => allTasks({ project: id, include_done: showDone }) });
  const p = projects.data?.projects.find((x) => x.id === id);
  const invalidate = useInvalidate();
  const setEmoji = useMutation({ mutationFn: (emoji: string) => api.tool("update_project", { project: id, emoji }), onSuccess: invalidate });
  // Tasks or the project's notes, kept in the address so Back from a note lands on the notes.
  const notesTab = (useSearch({ strict: false }) as { tab?: string }).tab === "notes";
  const navigate = useNavigate();
  const setTab = (tab?: string) => navigate({ to: ".", search: (s) => ({ ...s, tab }), replace: true });
  const notes = useProjectNotes(id);
  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState(false);
  return (
    <RowLook.Provider value={prefs}>
      <PageHeader title={p ? projectName(p) : "Project"}>
        {p && <EmojiPicker value={p.emoji ?? ""} onChange={(emoji) => setEmoji.mutate(emoji)} label="Project emoji" />}
        {notesTab ? <NewNote project={id} /> : <TaskToolbar prefs={prefs} onChange={setPrefs} query={query} onQuery={setQuery} view={view} onView={setView} link={`/projects/${id}`} onEdit={p ? () => setEditing(true) : undefined} />}
      </PageHeader>
      <div className="-mx-4 mb-4 flex gap-1.5 overflow-x-auto px-4 [scrollbar-width:none] md:mx-0 md:px-0" role="tablist" aria-label="Show">
        <ProjectChip active={!notesTab} onClick={() => setTab(undefined)} name="Tasks" n={p?.open_tasks ?? q.data?.tasks.length ?? 0} />
        <ProjectChip active={notesTab} onClick={() => setTab("notes")} name="Notes" n={notes.notes?.length ?? 0} />
      </div>
      <ErrorText error={setEmoji.error} />
      {notesTab ? <><ErrorText error={notes.error} /><ProjectNotes notes={notes.notes} more={notes.hasNextPage ? () => notes.fetchNextPage() : undefined} loadingMore={notes.isFetchingNextPage} /></> : (
        <>
          <InlineNewTask project={id} />
          {view === "board"
            ? <Board tasks={q.data && filterTasks(q.data.tasks.filter((t) => t.status !== "done" && t.status !== "cancelled"), prefs.filter, undefined, query)} done={done.data && filterTasks(done.data.tasks, prefs.filter, undefined, query)} />
            : <ArrangedList tasks={q.data?.tasks} prefs={prefs} query={query} onClear={() => { setPrefs({ filter: emptyFilter }); setQuery(""); }} empty="No tasks here yet." />}
        </>
      )}
      {p && <EditProjectDialog key={String(editing)} open={editing} project={p} onClose={() => setEditing(false)} />}
    </RowLook.Provider>
  );
}

// EditProjectDialog renames a project and edits its description (Edit list in View settings).
function EditProjectDialog({ open, project, onClose }: { open: boolean; project: Project; onClose: () => void }) {
  const invalidate = useInvalidate();
  const [name, setName] = useState(project.name);
  const [description, setDescription] = useState(project.description ?? "");
  const save = useMutation({
    mutationFn: () => api.tool("update_project", { project: project.id, name: name.trim(), description: description.trim() }),
    onSuccess: () => { invalidate(); onClose(); },
  });
  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="rounded-2xl sm:max-w-md">
        <DialogTitle>Edit list</DialogTitle>
        <DialogDescription className="sr-only">Rename the project or change its description</DialogDescription>
        <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); if (name.trim()) save.mutate(); }}>
          <Input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Name" aria-label="Name" maxLength={100} />
          <Textarea rows={3} value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What this project is about (agents read it too)" aria-label="Description" />
          <ErrorText error={save.error} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={!name.trim() || save.isPending}>Save</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
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

// EventLine is one change in a history. restoreKind names the events whose previous_body it offers to
// restore (a task's updates, or a note's).
export function EventLine({ e, showTask = true, showNote = true, restoreKind = "updated", onRestore }: { e: Event; showTask?: boolean; showNote?: boolean; restoreKind?: string; onRestore?: (body: string) => void }) {
  const previous = e.kind === restoreKind && typeof e.data.previous_body === "string" ? (e.data.previous_body as string) : undefined;
  const detail = eventDetail(e);
  const note = showNote && typeof e.data.note_id === "string" ? { id: e.data.note_id, title: String(e.data.title ?? "a note") } : undefined;
  return (
    <div className="flex gap-3 py-1.5 text-sm">
      <span className="w-14 shrink-0 text-xs leading-5 text-muted-foreground/70 sm:w-20">{ago(e.at)}</span>
      <div className="min-w-0">
        <Who m={e.actor} by={e.on_behalf_of} /> <span className="text-muted-foreground">{verb(e)}</span>{" "}
        {note && <Link to="/notes/$id" params={{ id: note.id }} className="font-medium hover:underline">{note.title}</Link>}
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
