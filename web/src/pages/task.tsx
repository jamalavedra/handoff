import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useCanGoBack, useNavigate, useParams, useRouter } from "@tanstack/react-router";
import { AppWindow, ArrowUp, Bot, History, Link2, Repeat as RepeatIcon, ShieldQuestion, SmilePlus, CalendarDays, ChevronDown, ChevronLeft, Flag, Folder, Lock, Maximize2, MoreHorizontal, RotateCcw, Trash2, User, X, XCircle } from "lucide-react";
import { TaskLink } from "../components/tasklink";
import { useTabs } from "../components/tabs";
import { taskHref } from "../tabs";
import { Markdown } from "../components/markdown";
import { BodyEditor } from "../components/editor";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { useMe } from "../main";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ago, api, cn, type Comment, type Event, type Member, type MemberRef, type Project, type Task, statusLabel } from "../lib";
import { agentOnIt, dueLabel, mentionQuery, repeatLabel } from "../quickadd";
import { Avatar, ErrorText, TaskCheck, Who } from "../components/app";
import { Button } from "@/components/ui/button";
import { EventLine, useInvalidate } from "./tasks";
import { completedText, markLocal, undoable, undoClose } from "../components/feedback";
import { toast } from "sonner";
import { useDictation } from "@/hooks/use-dictation";
import { MicButton } from "../components/dictation";

type Here = { member: MemberRef; typing?: boolean };
type Detail = { task: Task; comments: Comment[]; events: Event[]; here?: Here[] };

const names = (h: Here[]) => (h.length <= 2 ? h.map((x) => x.member.name).join(" and ") : `${h[0].member.name} and ${h.length - 1} others`);

const statusTone: Record<Task["status"], string> = {
  open: "bg-accent text-foreground/80",
  in_progress: "bg-amber-50 text-amber-800",
  blocked: "bg-red-50 text-red-700",
  done: "bg-green-50 text-green-700",
  cancelled: "bg-accent text-muted-foreground",
};
const priorityText = ["Urgent", "High", "Normal", "Low", "Someday"];

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

// TaskView is the task page body; with `panel` it renders inside the desktop side panel.
export function TaskView({ id, panel }: { id: string; panel?: boolean }) {
  const navigate = useNavigate();
  const router = useRouter();
  const canGoBack = useCanGoBack();
  const tabs = useTabs();
  const closePanel = () => navigate({ to: ".", search: (p) => ({ ...p, task: undefined }) });
  const q = useQuery({ queryKey: ["task", id], queryFn: () => api.tool<Detail>("get_task", { id }), refetchInterval: 10000 });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const invalidate = useInvalidate();
  const [comment, setComment] = useState("");
  const [caret, setCaret] = useState(0);
  const [pick, setPick] = useState(0);
  const commentRef = useRef<HTMLTextAreaElement>(null);
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
  const [activityOpen, setActivityOpen] = useState(false);
  const meId = useMe().data?.member?.id;
  // markLocal: our own edits shouldn't flash the row as "changed elsewhere".
  // A patch can name its task: a body draft saved as the view switches tasks still belongs to the old one.
  const update = useMutation({ mutationFn: ({ id: on = id, ...patch }: { id?: string } & Record<string, unknown>) => { markLocal(on); return api.tool("update_task", { id: on, ...patch }); }, onSuccess: invalidate });
  const close = useMutation({
    mutationFn: (status: string) => { markLocal(id); return api.tool<{ next?: Task }>("close_task", { id, status }); },
    onSuccess: (r, status) => { invalidate(); undoable(qc, completedText(status === "done" ? "Task completed" : "Task cancelled", r.next), () => undoClose(id, r.next)); },
  });
  const trash = useMutation({
    mutationFn: () => { markLocal(id); return api.tool("trash_task", { id }); },
    onSuccess: () => { invalidate(); undoable(qc, "Moved to trash", () => api.tool("restore_task", { id })); },
  });
  const restore = useMutation({ mutationFn: () => { markLocal(id); return api.tool("restore_task", { id }); }, onSuccess: invalidate });
  const lastTyping = useRef(0);
  const watch = (extra?: { typing?: boolean; leave?: boolean }) => api.tool("watch_task", { id, ...extra }).catch(() => {});
  const typing = () => {
    if (Date.now() - lastTyping.current < 3000) return;
    lastTyping.current = Date.now();
    watch({ typing: true });
  };
  useEffect(() => {
    watch();
    const beat = setInterval(watch, 15000);
    return () => { clearInterval(beat); watch({ leave: true }); };
  }, [id]);
  const dictation = useDictation((text) => { setComment(text); setCaret(text.length); typing(); });
  // The view is reused when the panel or route switches tasks: drop the dictation and draft with the old one.
  const { cancel: cancelDictation } = dictation;
  useEffect(() => () => { cancelDictation(); setComment(""); setCaret(0); }, [id, cancelDictation]);
  const addComment = useMutation({ mutationFn: () => api.tool("comment", { id, body: comment }), onSuccess: () => { setComment(""); lastTyping.current = 0; watch(); invalidate(); } });
  const link = useMutation({ mutationFn: (v: { blocker: string; remove?: boolean }) => api.tool("link_tasks", { blocker: v.blocker, blocked: id, remove: v.remove }), onSuccess: invalidate });

  // Back within the app when there is somewhere to go; a deep link (fresh tab) lands on My tasks.
  const goBack = () => (canGoBack ? router.history.back() : navigate({ to: "/" }));
  // On phones the full page has no app bars, so loading and errors keep a way back.
  const back = (
    <button onClick={() => goBack()} className="-ml-2 mb-3 flex h-9 items-center gap-0.5 rounded-full pr-2 pl-1 text-sm text-muted-foreground hover:bg-accent">
      <ChevronLeft className="size-5" />Back
    </button>
  );
  if (q.error) {
    if (!panel) return <div>{back}<ErrorText error={q.error} /></div>;
    return (
      <div className="px-5 pt-4">
        <button onClick={closePanel} className="-ml-2 mb-3 flex size-9 items-center justify-center rounded-full text-muted-foreground hover:bg-accent" aria-label="Close panel" title="Close (Esc)"><X className="size-5" /></button>
        <ErrorText error={q.error} />
      </div>
    );
  }
  if (!q.data) return panel ? null : back;
  const { task: t, comments, events } = q.data;
  const closed = t.status === "done" || t.status === "cancelled";
  const locked = !!t.deleted_at;
  const err = update.error || close.error || trash.error || restore.error || addComment.error || link.error;
  const setStatus = (s: string) => (s === "done" || s === "cancelled" ? close.mutate(s) : update.mutate({ status: s }));
  const people = members.data?.members.filter((m) => m.status !== "removed") ?? [];
  const due = t.due ? dueLabel(t.due) : undefined;
  const activity = events.filter((e) => e.kind !== "commented");
  const memberNames = people.map((m) => m.name);
  const mq = mentionQuery(comment, caret);
  const suggestions = mq ? people.filter((m) => m.name.toLowerCase().includes(mq.query.toLowerCase())).slice(0, 5) : [];
  const picked = suggestions.length ? pick % suggestions.length : 0;
  const insertMention = (name: string) => {
    if (!mq) return;
    const pos = mq.start + name.length + 2;
    setComment(comment.slice(0, mq.start) + "@" + name + " " + comment.slice(caret));
    setCaret(pos);
    requestAnimationFrame(() => {
      commentRef.current?.focus();
      commentRef.current?.setSelectionRange(pos, pos);
    });
  };
  const here = q.data.here ?? [];
  const typers = here.filter((h) => h.typing);

  return (
    <div className={cn("mx-auto flex max-w-3xl flex-col", panel ? "min-h-full px-5 pt-4" : "min-h-[calc(100dvh-1rem-env(safe-area-inset-top)-env(safe-area-inset-bottom))] md:min-h-[calc(100dvh-4rem)] motion-safe:animate-in motion-safe:fade-in-0 motion-safe:slide-in-from-right-8 motion-safe:duration-300 md:animate-none")}>
      <div className="mb-3 flex items-center gap-1">
        {panel ? (
          <button onClick={closePanel} className="-ml-2 flex size-9 items-center justify-center rounded-full text-muted-foreground hover:bg-accent" aria-label="Close panel" title="Close (Esc)">
            <X className="size-5" />
          </button>
        ) : (
          <button onClick={() => goBack()} className="-ml-2 flex h-9 items-center gap-0.5 rounded-full pr-2 pl-1 text-sm text-muted-foreground hover:bg-accent" aria-label="Back">
            <ChevronLeft className="size-5" /><span className="hidden sm:inline">Back</span>
          </button>
        )}
        <span className="min-w-0 flex-1 truncate text-sm text-muted-foreground/70">
          <Link to="/projects/$id" params={{ id: t.project_id }} className="hover:text-foreground/80 hover:underline">{t.project}</Link>
          {t.deleted_at && <span className="ml-2 font-medium text-red-600">In trash</span>}
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
        {panel && (
          <Link to="/tasks/$id" params={{ id }} className="flex size-9 items-center justify-center rounded-full text-muted-foreground hover:bg-accent" aria-label="Open full page" title="Open full page">
            <Maximize2 className="size-4" />
          </Link>
        )}
        <Menu>
          {(done) => (
            <>
              {panel && tabs && <MenuItem icon={AppWindow} onClick={() => { tabs.openInTab(taskHref(t.id)); done(); }}>Open in new tab</MenuItem>}
              <MenuItem icon={History} onClick={() => { setActivityOpen(true); done(); }}>Activity <span className="ml-auto text-xs text-muted-foreground/70 tabular-nums">{activity.length}</span></MenuItem>
              <MenuItem icon={Link2} onClick={() => { copyTaskLink(t.id); done(); }}>Copy link</MenuItem>
              {!closed && !locked && <MenuItem icon={XCircle} onClick={() => { close.mutate("cancelled"); done(); }}>Cancel task</MenuItem>}
              {locked ? <MenuItem icon={RotateCcw} onClick={() => { restore.mutate(); done(); }}>Restore</MenuItem> : <MenuItem icon={Trash2} danger onClick={() => { trash.mutate(); done(); }}>Move to trash</MenuItem>}
            </>
          )}
        </Menu>
      </div>

      {/* One scrollable row on phones so the title stays near the top; wrapped on wider screens and in the panel. */}
      <div className={cn("mb-4 flex gap-2", panel ? "flex-wrap" : "-mx-4 overflow-x-auto px-4 [scrollbar-width:none] md:mx-0 md:flex-wrap md:overflow-visible md:px-0")}>
        <Pill label="Status" className={statusTone[t.status]} select={!locked && (
          <select className={overlay} value={t.status} onChange={(e) => setStatus(e.target.value)} aria-label="Status">
            {(["open", "in_progress", "blocked", "done", "cancelled"] as const).map((s) => <option key={s} value={s}>{statusLabel[s]}</option>)}
          </select>
        )}>{statusLabel[t.status]}</Pill>
        <Pill label="Due" icon={CalendarDays} className={cn(due?.tone === "overdue" && !closed && "bg-red-50 text-red-700", due?.tone === "today" && !closed && "bg-blue-50 text-blue-700")} select={!locked && (
          <input ref={dueRef} type="date" className={overlay} value={t.due ?? ""} onClick={() => { try { dueRef.current?.showPicker(); } catch { /* not supported */ } }} onChange={(e) => update.mutate({ due: e.target.value })} aria-label="Due" />
        )}>{due ? due.text : "No date"}</Pill>
        {t.due && !locked && <button onClick={() => update.mutate({ due: "" })} aria-label="Clear due date" className="-ml-1.5 flex size-7 items-center justify-center rounded-full text-muted-foreground/70 hover:bg-accent"><X className="size-3.5" /></button>}
        <Pill label="Assignee" icon={t.assignee?.kind === "agent" ? Bot : User} className={t.assignee?.kind === "agent" ? "bg-violet-50 text-violet-700" : ""} select={!locked && (
          <select className={overlay} value={t.assignee?.id ?? ""} onChange={(e) => update.mutate({ assignee: e.target.value })} aria-label="Assignee">
            <option value="">Unassigned</option>
            {people.map((m) => <option key={m.id} value={m.id}>{m.name}{m.kind === "agent" ? " (agent)" : ""}</option>)}
          </select>
        )}>{t.assignee?.name ?? "Unassigned"}</Pill>
        <Pill label="Priority" icon={Flag} className={cn(t.priority === 0 && "bg-red-50 text-red-700", t.priority === 1 && "bg-orange-50 text-orange-700")} select={!locked && (
          <select className={overlay} value={t.priority} onChange={(e) => update.mutate({ priority: Number(e.target.value) })} aria-label="Priority">
            {priorityText.map((p, i) => <option key={i} value={i}>{p}</option>)}
          </select>
        )}>{priorityText[t.priority]}</Pill>
        <Pill label="Repeat" icon={RepeatIcon} select={!locked && (
          <select className={overlay} value={t.repeat ?? ""} onChange={(e) => update.mutate({ repeat: e.target.value })} aria-label="Repeat">
            <option value="">Doesn't repeat</option>
            {(Object.keys(repeatLabel) as (keyof typeof repeatLabel)[]).map((r) => <option key={r} value={r}>{repeatLabel[r]}</option>)}
          </select>
        )}>{t.repeat ? repeatLabel[t.repeat] : "Doesn't repeat"}</Pill>
        <Pill label="Project" icon={Folder} select={!locked && (
          <select className={overlay} value={t.project_id} onChange={(e) => update.mutate({ project: e.target.value })} aria-label="Project">
            {projects.data?.projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
        )}>{t.project}</Pill>
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

      <PendingApprovals id={t.id} />
      <ErrorText error={err} />

      {/* -mx-2/px-2 cancel out: the frame lines up with the details' own hover area. */}
      <div className={cn("-mx-2 mb-6 rounded-lg px-2", agentOnIt(t, here) && "agent-border")}><BodyEditor key={t.id} t={t} disabled={locked} onSave={(body) => update.mutateAsync({ id: t.id, body, body_mode: "replace", confirm: true })} /></div>

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

      {/* The task's chat: talking with the team and its agents about the work above. */}
      {comments.length > 0 && (
        <section className="mb-4 flex flex-col gap-3" aria-label="Comments">
          <h2 className="text-[13px] font-medium text-muted-foreground">Chat</h2>
          {comments.map((c) => {
            const mine = c.author?.id === meId;
            return (
            <div key={c.id} className={cn("flex gap-2.5", mine && "flex-row-reverse")}>
              {!mine && <Avatar m={c.author} className="mt-0.5" />}
              <div className={cn("min-w-0 max-w-[88%] rounded-2xl px-3 py-2 text-[15px] sm:text-sm", mine ? "rounded-tr-md bg-accent" : "rounded-tl-md bg-card shadow-[0_0_0_1px_rgba(0,0,0,0.06)]")}>
                {!mine && <div className="mb-0.5 text-xs text-muted-foreground"><Who m={c.author} by={c.on_behalf_of} /> · {ago(c.created_at)}</div>}
                <Markdown text={c.body} mentions={memberNames} />
                <Reactions c={c} disabled={locked} />
                {mine && <div className="mt-0.5 text-right text-[11px] text-muted-foreground/70">{ago(c.created_at)}</div>}
              </div>
            </div>
            );
          })}
        </section>
      )}

      <Dialog open={activityOpen} onOpenChange={setActivityOpen}>
        <DialogContent className="max-h-[80dvh] overflow-y-auto rounded-2xl sm:max-w-lg">
          <DialogTitle>Activity</DialogTitle>
          <DialogDescription>Created by {t.created_by?.name ?? "someone"}{t.on_behalf_of ? ` for ${t.on_behalf_of.name}` : ""} · {ago(t.created_at)}</DialogDescription>
          <div>{activity.map((e) => <EventLine key={e.id} e={e} showTask={false} onRestore={locked ? undefined : (body) => update.mutate({ body, body_mode: "replace", confirm: true }, { onSuccess: () => { toast("Previous version restored"); setActivityOpen(false); } })} />)}</div>
        </DialogContent>
      </Dialog>

      {!locked && (
        <form
          className="sticky bottom-[calc(0.75rem+env(safe-area-inset-bottom))] z-10 mt-auto flex items-end gap-2 rounded-2xl border border-border bg-card p-1.5 pl-3 shadow-lg md:bottom-4"
          onSubmit={(e) => { e.preventDefault(); dictation.cancel(); if (comment.trim()) addComment.mutate(); }}
        >
          {suggestions.length > 0 && (
            <ul role="listbox" aria-label="Mention someone" className="absolute bottom-full left-0 mb-2 w-64 overflow-hidden rounded-xl border border-border bg-card py-1 shadow-lg">
              {suggestions.map((m, i) => (
                <li key={m.id} role="option" aria-selected={i === picked}>
                  <button type="button" onMouseDown={(e) => { e.preventDefault(); insertMention(m.name); }} className={cn("flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-muted", i === picked && "bg-accent")}>
                    <Avatar m={{ id: m.id, name: m.name, kind: m.kind }} />
                    <span className="truncate">{m.name}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
          {typers.length > 0 && <span aria-live="polite" className="absolute -top-6 left-3 animate-pulse text-xs text-muted-foreground">{names(typers)} {typers.length === 1 ? "is" : "are"} typing…</span>}
          <textarea
            rows={1}
            ref={commentRef}
            placeholder={t.assignee?.kind === "agent" ? `Message ${t.assignee.name}…` : "Message… type @ to mention"}
            value={comment}
            onChange={(e) => { dictation.cancel(); setComment(e.target.value); setCaret(e.target.selectionStart ?? 0); setPick(0); typing(); }}
            onSelect={(e) => setCaret(e.currentTarget.selectionStart ?? 0)}
            onKeyDown={(e) => {
              if (suggestions.length) {
                if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); setPick(picked + (e.key === "ArrowDown" ? 1 : suggestions.length - 1)); return; }
                if (e.key === "Enter" || e.key === "Tab") { e.preventDefault(); insertMention(suggestions[picked].name); return; }
                if (e.key === "Escape") { e.preventDefault(); setCaret(0); return; }
              }
              if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); e.currentTarget.form?.requestSubmit(); } }}
            aria-label="Comment"
            className="max-h-40 min-h-9 flex-1 resize-none bg-transparent py-1.5 text-base outline-none [field-sizing:content] placeholder:text-muted-foreground/70 sm:text-sm"
          />
          {dictation.supported && <MicButton listening={dictation.listening} onToggle={() => dictation.toggle(comment)} wave={dictation} />}
          <button disabled={!comment.trim() || addComment.isPending} aria-label="Send comment" className="flex size-9 shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground disabled:bg-foreground/10 disabled:text-muted-foreground/70">
            <ArrowUp className="size-4" />
          </button>
        </form>
      )}
    </div>
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

const quickReactions = ["👍", "✅", "👀", "🎉", "❤️", "🙏", "😄", "🚀"];

// Reactions are the light way to answer in a thread: ✅ "done", 👀 "looking", 👍 "agreed". Agents use them too.
function Reactions({ c, disabled }: { c: Comment; disabled: boolean }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const react = useMutation({
    mutationFn: (emoji: string) => api.tool("react", { comment: c.id, emoji }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["task"] }),
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  });
  const reactions = c.reactions ?? [];
  if (disabled && !reactions.length) return null;
  return (
    <div className="mt-1.5 flex flex-wrap items-center gap-1">
      {reactions.map((r) => (
        <button
          key={r.emoji}
          type="button"
          disabled={disabled || react.isPending}
          title={r.by.join(", ")}
          aria-pressed={!!r.mine}
          aria-label={`${r.emoji} ${r.count}, ${r.mine ? "remove yours" : "react"}`}
          onClick={() => react.mutate(r.emoji)}
          className={cn("flex h-6 items-center gap-1 rounded-full border px-1.5 text-xs tabular-nums transition-colors", r.mine ? "border-ring bg-accent text-foreground" : "border-border text-muted-foreground hover:bg-muted")}
        >
          <span className="text-sm leading-none">{r.emoji}</span>{r.count}
        </button>
      ))}
      {!disabled && (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <button type="button" aria-label="Add reaction" className="flex size-6 items-center justify-center rounded-full text-muted-foreground/70 transition-colors hover:bg-muted hover:text-foreground">
              <SmilePlus className="size-3.5" />
            </button>
          </PopoverTrigger>
          <PopoverContent align="start" className="flex w-auto gap-0.5 rounded-full p-1">
            {quickReactions.map((e) => (
              <button key={e} type="button" disabled={react.isPending} aria-label={`React ${e}`} onClick={() => { setOpen(false); react.mutate(e); }} className="flex size-8 items-center justify-center rounded-full text-lg transition-transform hover:scale-110 hover:bg-muted">
                {e}
              </button>
            ))}
          </PopoverContent>
        </Popover>
      )}
    </div>
  );
}

// copyTaskLink puts the task's address on the clipboard, to paste anywhere.
function copyTaskLink(id: string) {
  const url = `${window.location.origin}/tasks/${id}`;
  navigator.clipboard?.writeText(url).then(() => toast("Link copied", { description: url }), () => toast.error("Couldn't copy the link"));
}
