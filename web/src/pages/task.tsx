import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCanGoBack, useNavigate, useParams, useRouter } from "@tanstack/react-router";
import { Archive, ArchiveRestore, Bot, Check, MessageCircleQuestion, History, Link2, Repeat as RepeatIcon, ShieldQuestion, CalendarDays, ChevronDown, ChevronLeft, Flag, Folder, Lock, MoreHorizontal, RotateCcw, Trash2, User, X } from "lucide-react";
import { TaskLink } from "../components/tasklink";
import { TaskText, Todos } from "../components/todos";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { ago, api, cn, type Member, type MemberRef, type Project, type Task, statusLabel } from "../lib";
import { agentOnIt, dueLabel, repeatLabel } from "../quickadd";
import { Avatar, ErrorText, TaskCheck } from "../components/app";
import { Button } from "@/components/ui/button";
import { EventLine, useInvalidate } from "./tasks";
import { completedText, markLocal, undoable, undoClose } from "../components/feedback";
import { toast } from "sonner";
import { ChatPreview, names, taskQuery } from "../components/task-chat";
import { useMe } from "../main";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useAssistant, useTaskChatShown } from "../components/assistant";


const statusTone: Record<Task["status"], string> = {
  open: "bg-accent text-foreground/80",
  in_progress: "bg-amber-50 text-amber-800",
  done: "bg-green-50 text-green-700",
  cancelled: "bg-accent text-muted-foreground",
};
const priorityText = ["Urgent", "High", "Normal", "Low", "Someday"];
// Every workspace starts with this project (core.DefaultProject); tasks land there unless filed elsewhere.
const defaultProject = "Inbox";
type Extra = "assignee" | "priority" | "repeat" | "project";

function Pill({ icon: Icon, children, className, label, select }: { icon?: typeof Flag; children: React.ReactNode; className?: string; label: string; select?: React.ReactNode }) {
  return (
    <span className={cn("relative inline-flex h-7 max-w-full shrink-0 items-center gap-1.5 rounded-full bg-accent px-2.5 text-[13px] font-medium text-foreground/80 focus-within:ring-2 focus-within:ring-foreground/40 hover:bg-foreground/10", className)}>
      {Icon && <Icon className="size-3 shrink-0" />}
      <span className="truncate">{children}</span>
      {select && <ChevronDown className="size-3 shrink-0 opacity-50" />}
      {select && <span className="sr-only">{label}</span>}
      {select}
    </span>
  );
}

// StatusSwitch is the status button: one click opens Open · In progress · Done, one more moves it.
// Archived isn't one of them (it's in the ⋯ menu); picking one here brings an archived task back.
function StatusSwitch({ status, disabled, onPick }: { status: Task["status"]; disabled: boolean; onPick: (s: string) => void }) {
  const [open, setOpen] = useState(false);
  const pill = (
    <span className={cn("inline-flex h-7 shrink-0 items-center gap-1.5 rounded-full px-2.5 text-[13px] font-medium", statusTone[status])}>
      {statusLabel[status]}{!disabled && <ChevronDown className="size-3 opacity-50" />}
    </span>
  );
  if (disabled) return pill;
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" aria-label={`Status: ${statusLabel[status]}`} className="rounded-full outline-none hover:brightness-95 focus-visible:ring-2 focus-visible:ring-foreground/40">{pill}</button>
      </PopoverTrigger>
      <PopoverContent align="start" className="flex w-auto gap-1 rounded-full p-1">
        {(["open", "in_progress", "done"] as const).map((s) => (
          <button
            key={s}
            type="button"
            aria-pressed={status === s}
            onClick={() => { setOpen(false); if (s !== status) onPick(s); }}
            className={cn("h-8 rounded-full px-3 text-[13px] font-medium transition-colors", status === s ? statusTone[s] + " ring-1 ring-foreground/15" : "text-muted-foreground hover:bg-accent hover:text-foreground")}
          >
            {statusLabel[s]}
          </button>
        ))}
      </PopoverContent>
    </Popover>
  );
}

const overlay = "absolute inset-0 size-full cursor-pointer appearance-none opacity-0";

function Menu({ children }: { children: (close: () => void) => React.ReactNode }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="relative">
      <button onClick={() => setOpen(!open)} aria-label="More actions" aria-expanded={open} className="flex size-9 items-center justify-center rounded-full text-muted-foreground hover:bg-accent">
        <MoreHorizontal className="size-5" />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-30" onClick={() => setOpen(false)} />
          <div role="menu" className="absolute right-0 z-40 mt-1 w-48 overflow-hidden rounded-xl border border-border bg-card py-1 shadow-lg">{children(() => setOpen(false))}</div>
        </>
      )}
    </div>
  );
}

function MenuItem({ icon: Icon, onClick, children, danger }: { icon: typeof Flag; onClick: () => void; children: React.ReactNode; danger?: boolean }) {
  return (
    <button role="menuitem" onClick={onClick} className={cn("flex w-full items-center gap-2.5 px-3 py-2.5 text-left text-sm hover:bg-muted", danger ? "text-red-600" : "text-foreground/80")}>
      <Icon className="size-4" />{children}
    </button>
  );
}

function TitleEditor({ t, onSave, disabled }: { t: Task; onSave: (title: string) => void; disabled: boolean }) {
  const [v, setV] = useState(t.title);
  const discard = useRef(false);
  useEffect(() => setV(t.title), [t.title]);
  const commit = () => {
    if (discard.current) { discard.current = false; return; }
    const next = v.replace(/\s+/g, " ").trim();
    if (!next) setV(t.title);
    else if (next !== t.title) onSave(next);
  };
  return (
    <textarea
      aria-label="Title"
      rows={1}
      value={v}
      readOnly={disabled}
      onChange={(e) => setV(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") { e.preventDefault(); e.currentTarget.blur(); }
        if (e.key === "Escape") { discard.current = true; setV(t.title); e.currentTarget.blur(); }
      }}
      className={cn("-mx-1 min-w-0 flex-1 resize-none rounded-md bg-transparent px-1 text-2xl leading-tight font-semibold outline-none [field-sizing:content] focus:bg-card focus:ring-2 focus:ring-input", (t.status === "done" || t.status === "cancelled") && "text-muted-foreground/70", t.triage === "pending" && "agent-shimmer")}
    />
  );
}

export function TaskPage() {
  const { id } = useParams({ strict: false }) as { id: string };
  return <TaskView id={id} />;
}

function TaskView({ id }: { id: string }) {
  const navigate = useNavigate();
  const router = useRouter();
  const canGoBack = useCanGoBack();
  const q = useQuery({ ...taskQuery(id), refetchInterval: 10000 });
  const assistant = useAssistant();
  const chatShown = useTaskChatShown();
  const meId = useMe().data?.member?.id;
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const invalidate = useInvalidate();
  const qc = useQueryClient();
  const loaded = !!q.data;
  const commentCount = q.data?.comments.length ?? 0;
  // Opening a task reads its notifications; polling in a background tab doesn't.
  useEffect(() => {
    const read = () => {
      if (!loaded || document.hidden) return;
      api.tool("mark_read", { task: id }).then(() => qc.invalidateQueries({ queryKey: ["inbox"] }), () => {});
    };
    read();
    document.addEventListener("visibilitychange", read);
    return () => document.removeEventListener("visibilitychange", read);
  }, [id, loaded, commentCount, qc]);
  const dueRef = useRef<HTMLInputElement>(null);
  // Assignee, priority, repeat and project show as pills only when set; the ⋯ menu sets them from a picker.
  const [picking, setPicking] = useState<Extra | null>(null);
  const [activityOpen, setActivityOpen] = useState(false);
  // markLocal: our own edits shouldn't flash the row as "changed elsewhere".
  // A patch can name its task: a body draft saved as the view switches tasks still belongs to the old one.
  const update = useMutation({ mutationFn: ({ id: on = id, ...patch }: { id?: string } & Record<string, unknown>) => { markLocal(on); return api.tool("update_task", { id: on, ...patch }); }, onSuccess: invalidate });
  const close = useMutation({
    mutationFn: (status: string) => { markLocal(id); return api.tool<{ next?: Task }>("close_task", { id, status }); },
    onSuccess: (r, status) => { invalidate(); undoable(qc, completedText(status === "done" ? "Task completed" : "Task archived", r.next), () => undoClose(id, r.next)); },
  });
  const trash = useMutation({
    mutationFn: () => { markLocal(id); return api.tool("trash_task", { id }); },
    onSuccess: () => { invalidate(); undoable(qc, "Moved to trash", () => api.tool("restore_task", { id })); },
  });
  const restore = useMutation({ mutationFn: () => { markLocal(id); return api.tool("restore_task", { id }); }, onSuccess: invalidate });
  const watch = (extra?: { leave?: boolean }) => api.tool("watch_task", { id, ...extra }).catch(() => {});
  useEffect(() => {
    watch();
    const beat = setInterval(watch, 15000);
    return () => { clearInterval(beat); watch({ leave: true }); };
  }, [id]);
  const link = useMutation({ mutationFn: (v: { blocker: string; remove?: boolean }) => api.tool("link_tasks", { blocker: v.blocker, blocked: id, remove: v.remove }), onSuccess: invalidate });

  // Back within the app when there is somewhere to go; a deep link (fresh tab) lands on My tasks.
  const goBack = () => (canGoBack ? router.history.back() : navigate({ to: "/" }));
  // On phones the full page has no app bars, so loading and errors keep a way back.
  const back = (
    <button onClick={() => goBack()} className="-ml-2 mb-3 flex h-9 items-center gap-0.5 rounded-full pr-2 pl-1 text-sm text-muted-foreground hover:bg-accent">
      <ChevronLeft className="size-5" />Back
    </button>
  );
  if (q.error) return <div>{back}<ErrorText error={q.error} /></div>;
  if (!q.data) return back;
  const { task: t, comments, events } = q.data;
  const closed = t.status === "done" || t.status === "cancelled";
  const locked = !!t.deleted_at;
  const err = update.error || close.error || trash.error || restore.error || link.error;
  const setStatus = (s: string) => (s === "done" || s === "cancelled" ? close.mutate(s) : update.mutate({ status: s }));
  const people = members.data?.members.filter((m) => m.status !== "removed") ?? [];
  const due = t.due ? dueLabel(t.due) : undefined;
  const activity = events.filter((e) => e.kind !== "commented");
  const here = q.data.here ?? [];
  const typers = here.filter((h) => h.typing);
  const configured: Record<Extra, boolean> = {
    assignee: !!t.assignee && t.assignee.id !== meId,
    priority: t.priority !== 2,
    repeat: !!t.repeat,
    project: t.project !== defaultProject,
  };
  const pickers: Record<Extra, Picker> = {
    assignee: {
      title: "Assign to",
      value: t.assignee?.id ?? "",
      options: [{ value: "", label: "Unassigned" }, ...people.map((m) => ({ value: m.id, label: m.kind === "agent" ? `${m.name} (agent)` : m.name }))],
      set: (v) => update.mutate({ assignee: v }),
    },
    priority: { title: "Priority", value: String(t.priority), options: priorityText.map((p, i) => ({ value: String(i), label: p })), set: (v) => update.mutate({ priority: Number(v) }) },
    repeat: {
      title: "Repeat",
      value: t.repeat ?? "",
      options: [{ value: "", label: "Doesn't repeat" }, ...(Object.keys(repeatLabel) as (keyof typeof repeatLabel)[]).map((r) => ({ value: r, label: repeatLabel[r] }))],
      set: (v) => update.mutate({ repeat: v }),
    },
    project: { title: "Move to project", value: t.project_id, options: projects.data?.projects.map((p) => ({ value: p.id, label: p.name })) ?? [], set: (v) => update.mutate({ project: v }) },
  };

  return (
    <div className="mx-auto flex min-h-[calc(100dvh-1rem-env(safe-area-inset-top)-env(safe-area-inset-bottom))] max-w-3xl flex-col motion-safe:animate-in motion-safe:fade-in-0 motion-safe:slide-in-from-right-8 motion-safe:duration-300 md:min-h-[calc(100dvh-4rem)] md:animate-none">
      <div className="mb-3 flex items-center gap-1">
        {/* Phones have no app bars on a task; wider screens go back from the tab strip. */}
        <button onClick={() => goBack()} className="-ml-2 flex size-9 items-center justify-center rounded-full text-muted-foreground hover:bg-accent md:hidden" aria-label="Back">
          <ChevronLeft className="size-5" />
        </button>
        <span className="min-w-0 flex-1 truncate text-sm">
          {t.deleted_at && <span className="font-medium text-red-600">In trash</span>}
        </span>
        {here.length > 0 && (
          <div role="group" aria-label={`${names(here)} ${here.length === 1 ? "is" : "are"} here`} title={`${names(here)} ${here.length === 1 ? "is" : "are"} looking at this`} className="mr-1 flex items-center -space-x-1.5">
            {here.slice(0, 3).map((h) => <Avatar key={h.member.id} m={h.member} className="ring-2 ring-background" />)}
            {here.length > 3 && <span className="inline-flex size-6 items-center justify-center rounded-full bg-accent text-[10px] font-semibold text-foreground/70 ring-2 ring-background">+{here.length - 3}</span>}
          </div>
        )}
        <button onClick={() => copyTaskLink(id)} className="flex size-9 items-center justify-center rounded-full text-muted-foreground hover:bg-accent" aria-label="Copy link" title="Copy link">
          <Link2 className="size-4" />
        </button>
        <Menu>
          {(done) => (
            <>
              <MenuItem icon={History} onClick={() => { setActivityOpen(true); done(); }}>Activity <span className="ml-auto text-xs text-muted-foreground/70 tabular-nums">{activity.length}</span></MenuItem>
              <MenuItem icon={Link2} onClick={() => { copyTaskLink(t.id); done(); }}>Copy link</MenuItem>
              {!locked && (
                <>
                  <div role="separator" className="my-1 h-px bg-border" />
                  <MenuItem icon={User} onClick={() => { done(); setPicking("assignee"); }}>Assign…</MenuItem>
                  <MenuItem icon={Flag} onClick={() => { done(); setPicking("priority"); }}>Priority…</MenuItem>
                  <MenuItem icon={RepeatIcon} onClick={() => { done(); setPicking("repeat"); }}>Repeat…</MenuItem>
                  <MenuItem icon={Folder} onClick={() => { done(); setPicking("project"); }}>Move to project…</MenuItem>
                  <div role="separator" className="my-1 h-px bg-border" />
                </>
              )}
              {!locked && (t.status === "cancelled"
                ? <MenuItem icon={ArchiveRestore} onClick={() => { setStatus("open"); done(); }}>Unarchive</MenuItem>
                : <MenuItem icon={Archive} onClick={() => { close.mutate("cancelled"); done(); }}>Archive</MenuItem>)}
              {locked ? <MenuItem icon={RotateCcw} onClick={() => { restore.mutate(); done(); }}>Restore</MenuItem> : <MenuItem icon={Trash2} danger onClick={() => { trash.mutate(); done(); }}>Move to trash</MenuItem>}
            </>
          )}
        </Menu>
      </div>

      {/* One scrollable row on phones so the title stays near the top; wrapped on wider screens. */}
      <div className="-mx-4 mb-4 flex gap-2 overflow-x-auto px-4 [scrollbar-width:none] md:mx-0 md:flex-wrap md:overflow-visible md:px-0">
        <StatusSwitch status={t.status} disabled={locked} onPick={setStatus} />
        <Pill label="Due" icon={CalendarDays} className={cn(due?.tone === "overdue" && !closed && "bg-red-50 text-red-700", due?.tone === "today" && !closed && "bg-blue-50 text-blue-700")} select={!locked && (
          <input ref={dueRef} type="date" className={overlay} value={t.due ?? ""} onClick={() => { try { dueRef.current?.showPicker(); } catch { /* not supported */ } }} onChange={(e) => update.mutate({ due: e.target.value })} aria-label="Due" />
        )}>{due ? due.text : "No date"}</Pill>
        {t.due && !locked && <button onClick={() => update.mutate({ due: "" })} aria-label="Clear due date" className="-ml-1.5 flex size-7 items-center justify-center rounded-full text-muted-foreground/70 hover:bg-accent"><X className="size-3.5" /></button>}
        {configured.assignee && <Pill label="Assignee" icon={t.assignee?.kind === "agent" ? Bot : User} className={t.assignee?.kind === "agent" ? "bg-violet-50 text-violet-700" : ""} select={!locked && (
          <select className={overlay} value={t.assignee?.id ?? ""} onChange={(e) => update.mutate({ assignee: e.target.value })} aria-label="Assignee">
            <option value="">Unassigned</option>
            {people.map((m) => <option key={m.id} value={m.id}>{m.name}{m.kind === "agent" ? " (agent)" : ""}</option>)}
          </select>
        )}>{t.assignee?.name ?? "Unassigned"}</Pill>}
        {configured.priority && <Pill label="Priority" icon={Flag} className={cn(t.priority === 0 && "bg-red-50 text-red-700", t.priority === 1 && "bg-orange-50 text-orange-700")} select={!locked && (
          <select className={overlay} value={t.priority} onChange={(e) => update.mutate({ priority: Number(e.target.value) })} aria-label="Priority">
            {priorityText.map((p, i) => <option key={i} value={i}>{p}</option>)}
          </select>
        )}>{priorityText[t.priority]}</Pill>}
        {configured.repeat && <Pill label="Repeat" icon={RepeatIcon} select={!locked && (
          <select className={overlay} value={t.repeat ?? ""} onChange={(e) => update.mutate({ repeat: e.target.value })} aria-label="Repeat">
            <option value="">Doesn't repeat</option>
            {(Object.keys(repeatLabel) as (keyof typeof repeatLabel)[]).map((r) => <option key={r} value={r}>{repeatLabel[r]}</option>)}
          </select>
        )}>{t.repeat ? repeatLabel[t.repeat] : "Doesn't repeat"}</Pill>}
        {configured.project && <Pill label="Project" icon={Folder} select={!locked && (
          <select className={overlay} value={t.project_id} onChange={(e) => update.mutate({ project: e.target.value })} aria-label="Project">
            {projects.data?.projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
        )}>{t.project}</Pill>}
      </div>

      <div className="mb-4 flex items-start gap-3">
        <div className="pt-1.5 pl-1"><TaskCheck t={t} disabled={locked || close.isPending || update.isPending} onToggle={() => (closed ? update.mutate({ status: "open" }) : close.mutate("done"))} /></div>
        <TitleEditor t={t} disabled={locked} onSave={(title) => update.mutate({ title })} />
      </div>


      {t.claimed_by && !closed && (
        <div className={cn("mb-5 flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm", t.claimed_by.kind === "agent" ? "bg-violet-50 text-violet-900" : "bg-amber-50 text-amber-900")}>
          <span className="relative flex size-2"><span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-60", t.claimed_by.kind === "agent" ? "bg-violet-400" : "bg-amber-400")} /><span className={cn("relative inline-flex size-2 rounded-full", t.claimed_by.kind === "agent" ? "bg-violet-500" : "bg-amber-500")} /></span>
          <span className="min-w-0 flex-1"><b className="font-medium">{t.claimed_by.name}</b> is working on this{t.claim_expires_at && <span className="opacity-60"> · until {new Date(t.claim_expires_at).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })}</span>}</span>
        </div>
      )}

      {t.waiting_on && !closed && (
        <button
          type="button"
          onClick={() => assistant.openTaskChat()}
          className="mb-4 flex w-full items-center gap-2 rounded-xl bg-amber-50 px-3 py-2 text-left text-sm text-amber-800 hover:bg-amber-100 dark:bg-amber-500/10 dark:text-amber-300 dark:hover:bg-amber-500/15"
        >
          <MessageCircleQuestion className="size-4 shrink-0" />
          <span className="min-w-0 flex-1">{t.waiting_on.id === meId ? "Your input is needed to go on" : `Waiting on ${t.waiting_on.name}`}: answer in the chat</span>
        </button>
      )}
      <PendingApprovals id={t.id} />
      <ErrorText error={err} />

      {/* A task is its text and its todos. -mx-2/px-2 line the frame up with the rows' hover areas. */}
      <div className={cn("-mx-2 mb-2 rounded-lg px-2", agentOnIt(t, here) && "agent-border")}>
        <div className="mb-3"><TaskText key={t.id} id={t.id} body={t.body ?? ""} disabled={locked} onSave={(body) => update.mutateAsync({ id: t.id, body, body_mode: "replace", confirm: true })} /></div>
        <Todos key={`todos-${t.id}`} task={t.id} todos={t.todos ?? []} disabled={locked} />
      </div>

      {/* Dependencies are usually set by agents over the API; people see them and can drop them. */}
      {(t.blocked_by.length > 0 || t.blocks.length > 0) && (
      <section className="mb-6" aria-label="Dependencies">
          <div className="mb-2 flex flex-col gap-1.5">
            {t.blocked_by.map((b) => (
              <div key={b} className="flex items-center gap-2 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900">
                <Lock className="size-3.5" />Waits on <TaskLink id={b} className="font-mono text-xs underline-offset-2 hover:underline">{b}</TaskLink>
                {!locked && <button className="ml-auto text-xs text-amber-700/70 hover:text-red-600" onClick={() => link.mutate({ blocker: b, remove: true })}>Remove</button>}
              </div>
            ))}
            {t.blocks.map((b) => (
              <div key={b} className="flex items-center gap-2 rounded-lg bg-accent px-3 py-2 text-sm text-foreground/70">
                Blocks <TaskLink id={b} className="font-mono text-xs underline-offset-2 hover:underline">{b}</TaskLink>
              </div>
            ))}
          </div>
      </section>
      )}


      {/* The chat lives in the assistant panel, beside the task; this opens it there. */}
      {!chatShown && <ChatPreview
        task={id}
        comments={comments}
        agent={t.assignee?.kind === "agent" ? t.assignee : undefined}
        typing={typers.length ? `${names(typers)} ${typers.length === 1 ? "is" : "are"} typing…` : undefined}
        onOpen={() => assistant.openTaskChat()}
      />}

      <PickerDialog picker={picking ? pickers[picking] : undefined} onClose={() => setPicking(null)} />

      <Dialog open={activityOpen} onOpenChange={setActivityOpen}>
        <DialogContent className="max-h-[80dvh] overflow-y-auto rounded-2xl sm:max-w-lg">
          <DialogTitle>Activity</DialogTitle>
          <DialogDescription>Created by {t.created_by?.name ?? "someone"}{t.on_behalf_of ? ` for ${t.on_behalf_of.name}` : ""} · {ago(t.created_at)}</DialogDescription>
          <div>{activity.map((e) => <EventLine key={e.id} e={e} showTask={false} restoreKind="updated" onRestore={locked ? undefined : (body) => update.mutate({ body, body_mode: "replace", confirm: true }, { onSuccess: () => { toast("Previous version restored"); setActivityOpen(false); } })} />)}</div>
        </DialogContent>
      </Dialog>

    </div>
  );
}

type Picker = { title: string; value: string; options: { value: string; label: string }[]; set: (value: string) => void };

// PickerDialog lists a field's options as buttons: a picker every browser shows, unlike opening a
// native select from script.
function PickerDialog({ picker, onClose }: { picker?: Picker; onClose: () => void }) {
  return (
    <Dialog open={!!picker} onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="max-h-[80dvh] overflow-y-auto rounded-2xl sm:max-w-xs">
        <DialogTitle>{picker?.title}</DialogTitle>
        <DialogDescription className="sr-only">Choose an option</DialogDescription>
        <div className="-mx-2 flex flex-col">
          {picker?.options.map((o) => (
            <button
              key={o.value}
              aria-pressed={o.value === picker.value}
              autoFocus={o.value === picker.value}
              onClick={() => { if (o.value !== picker.value) picker.set(o.value); onClose(); }}
              className="flex items-center gap-2 rounded-lg px-2 py-2.5 text-left text-sm hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
            >
              <span className="min-w-0 flex-1 truncate">{o.label}</span>
              {o.value === picker.value && <Check className="size-4 shrink-0 text-muted-foreground" />}
            </button>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}

type Approval = { id: string; agent: MemberRef; action: string; task?: Task; reason?: string };

// PendingApprovals shows an agent's request on the task it concerns, so it can be decided in place.
function PendingApprovals({ id }: { id: string }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["approvals"], queryFn: () => api.tool<{ approvals: Approval[] }>("list_approvals") });
  const decide = useMutation({
    mutationFn: (v: { id: string; approve: boolean }) => api.tool("decide_approval", v),
    onSuccess: (_r, v) => { qc.invalidateQueries(); toast(v.approve ? "Approved" : "Request declined"); },
  });
  const mine = q.data?.approvals.filter((a) => a.task?.id === id) ?? [];
  if (!mine.length) return null;
  return (
    <div className="mb-5 flex flex-col gap-2">
      {mine.map((a) => (
        <div key={a.id} className="flex flex-wrap items-center gap-2 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2.5 text-sm text-amber-900">
          <ShieldQuestion className="size-4 shrink-0" />
          <span className="min-w-0 flex-1"><b className="font-medium">{a.agent.name}</b> wants to {a.action === "trash" || a.action === "delete" ? "move this task to the trash" : a.action}{a.reason ? `: ${a.reason}` : "."}</span>
          <Button size="sm" variant="ghost" onClick={() => decide.mutate({ id: a.id, approve: false })} disabled={decide.isPending}>Deny</Button>
          <Button size="sm" onClick={() => decide.mutate({ id: a.id, approve: true })} disabled={decide.isPending}>Approve</Button>
        </div>
      ))}
      <ErrorText error={decide.error} />
    </div>
  );
}

// copyTaskLink puts the task's address on the clipboard, to paste anywhere.
function copyTaskLink(id: string) {
  const url = `${window.location.origin}/tasks/${id}`;
  navigator.clipboard?.writeText(url).then(() => toast("Link copied", { description: url }), () => toast.error("Couldn't copy the link"));
}
