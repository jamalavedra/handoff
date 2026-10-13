import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { ArrowUp, Bot, CalendarDays, ChevronDown, ChevronLeft, Copy, Flag, Folder, Lock, MoreHorizontal, RotateCcw, Trash2, User, X, XCircle } from "lucide-react";
import { ago, api, cn, type Comment, type Event, type Member, type Project, type Task, statusLabel } from "../lib";
import { dueLabel } from "../quickadd";
import { Avatar, Button, ErrorText, Input, TaskCheck, Textarea, Who } from "../components/ui";
import { EventLine, useInvalidate } from "./tasks";

type Detail = { task: Task; comments: Comment[]; events: Event[] };

const statusTone: Record<Task["status"], string> = {
  open: "bg-zinc-100 text-zinc-700",
  in_progress: "bg-amber-50 text-amber-800",
  blocked: "bg-red-50 text-red-700",
  done: "bg-green-50 text-green-700",
  cancelled: "bg-zinc-100 text-zinc-500",
};
const priorityText = ["Urgent", "High", "Normal", "Low", "Someday"];

function Pill({ icon: Icon, children, className, label, select }: { icon?: typeof Flag; children: React.ReactNode; className?: string; label: string; select?: React.ReactNode }) {
  return (
    <span className={cn("relative inline-flex h-8 max-w-full items-center gap-1.5 rounded-full bg-zinc-100 px-3 text-sm font-medium text-zinc-700 focus-within:ring-2 focus-within:ring-zinc-400 hover:bg-zinc-200/70", className)}>
      {Icon && <Icon className="size-3.5 shrink-0" />}
      <span className="truncate">{children}</span>
      {select && <ChevronDown className="size-3.5 shrink-0 opacity-50" />}
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
      <button onClick={() => setOpen(!open)} aria-label="More actions" aria-expanded={open} className="flex size-9 items-center justify-center rounded-full text-zinc-500 hover:bg-zinc-100">
        <MoreHorizontal className="size-5" />
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-30" onClick={() => setOpen(false)} />
          <div role="menu" className="absolute right-0 z-40 mt-1 w-48 overflow-hidden rounded-xl border border-zinc-200 bg-white py-1 shadow-lg">{children(() => setOpen(false))}</div>
        </>
      )}
    </div>
  );
}

function MenuItem({ icon: Icon, onClick, children, danger }: { icon: typeof Flag; onClick: () => void; children: React.ReactNode; danger?: boolean }) {
  return (
    <button role="menuitem" onClick={onClick} className={cn("flex w-full items-center gap-2.5 px-3 py-2.5 text-left text-sm hover:bg-zinc-50", danger ? "text-red-600" : "text-zinc-700")}>
      <Icon className="size-4" />{children}
    </button>
  );
}

function TitleEditor({ t, onSave, disabled }: { t: Task; onSave: (title: string) => void; disabled: boolean }) {
  const [v, setV] = useState(t.title);
  useEffect(() => setV(t.title), [t.title]);
  const commit = () => {
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
        if (e.key === "Escape") { setV(t.title); e.currentTarget.blur(); }
      }}
      className={cn("-mx-1 min-w-0 flex-1 resize-none rounded-md bg-transparent px-1 text-2xl leading-tight font-semibold outline-none [field-sizing:content] focus:bg-white focus:ring-2 focus:ring-zinc-300", (t.status === "done" || t.status === "cancelled") && "text-zinc-400")}
    />
  );
}

function BodyEditor({ t, onSave, disabled }: { t: Task; onSave: (body: string) => void; disabled: boolean }) {
  const [editing, setEditing] = useState(false);
  const [v, setV] = useState(t.body ?? "");
  if (editing)
    return (
      <form className="flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); onSave(v); setEditing(false); }}>
        <Textarea autoFocus rows={5} value={v} onChange={(e) => setV(e.target.value)} aria-label="Details" />
        <div className="flex justify-end gap-2">
          <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>Cancel</Button>
          <Button size="sm">Save</Button>
        </div>
      </form>
    );
  if (!t.body) return disabled ? null : <button className="text-sm text-zinc-400 hover:text-zinc-700" onClick={() => { setV(""); setEditing(true); }}>Add details…</button>;
  return (
    <button disabled={disabled} className="block w-full rounded-lg text-left text-[15px] leading-relaxed whitespace-pre-wrap text-zinc-700 enabled:hover:bg-zinc-100/60 sm:text-sm" onClick={() => { setV(t.body ?? ""); setEditing(true); }}>
      {t.body}
    </button>
  );
}

export function TaskPage() {
  const { id } = useParams({ strict: false }) as { id: string };
  const navigate = useNavigate();
  const q = useQuery({ queryKey: ["task", id], queryFn: () => api.tool<Detail>("get_task", { id }) });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const invalidate = useInvalidate();
  const [comment, setComment] = useState("");
  const [blocker, setBlocker] = useState("");
  const dueRef = useRef<HTMLInputElement>(null);
  const update = useMutation({ mutationFn: (patch: Record<string, unknown>) => api.tool("update_task", { id, ...patch }), onSuccess: invalidate });
  const close = useMutation({ mutationFn: (status: string) => api.tool("close_task", { id, status }), onSuccess: invalidate });
  const trash = useMutation({ mutationFn: () => api.tool("trash_task", { id }), onSuccess: invalidate });
  const restore = useMutation({ mutationFn: () => api.tool("restore_task", { id }), onSuccess: invalidate });
  const claim = useMutation({ mutationFn: () => api.tool("claim_task", { id }), onSuccess: invalidate });
  const release = useMutation({ mutationFn: () => api.tool("release_task", { id }), onSuccess: invalidate });
  const addComment = useMutation({ mutationFn: () => api.tool("comment", { id, body: comment }), onSuccess: () => { setComment(""); invalidate(); } });
  const link = useMutation({ mutationFn: (v: { blocker: string; remove?: boolean }) => api.tool("link_tasks", { blocker: v.blocker, blocked: id, remove: v.remove }), onSuccess: () => { setBlocker(""); invalidate(); } });

  if (q.error) return <ErrorText error={q.error} />;
  if (!q.data) return null;
  const { task: t, comments, events } = q.data;
  const closed = t.status === "done" || t.status === "cancelled";
  const locked = !!t.deleted_at;
  const err = update.error || close.error || trash.error || restore.error || claim.error || release.error || addComment.error || link.error;
  const setStatus = (s: string) => (s === "done" || s === "cancelled" ? close.mutate(s) : update.mutate({ status: s }));
  const people = members.data?.members.filter((m) => m.status !== "removed") ?? [];
  const due = t.due ? dueLabel(t.due) : undefined;
  const activity = events.filter((e) => e.kind !== "commented");

  return (
    <div className="mx-auto flex min-h-[calc(100dvh-13rem-env(safe-area-inset-bottom))] max-w-3xl flex-col md:min-h-[calc(100dvh-4rem)]">
      <div className="mb-3 flex items-center gap-1">
        <button onClick={() => window.history.length > 1 ? window.history.back() : navigate({ to: "/" })} className="-ml-2 flex h-9 items-center gap-0.5 rounded-full pr-2 pl-1 text-sm text-zinc-500 hover:bg-zinc-100" aria-label="Back">
          <ChevronLeft className="size-5" /><span className="hidden sm:inline">Back</span>
        </button>
        <span className="min-w-0 flex-1 truncate text-sm text-zinc-400">
          <Link to="/projects/$id" params={{ id: t.project_id }} className="hover:text-zinc-700 hover:underline">{t.project}</Link>
          {t.deleted_at && <span className="ml-2 font-medium text-red-600">In trash</span>}
        </span>
        <Menu>
          {(done) => (
            <>
              <MenuItem icon={Copy} onClick={() => { navigator.clipboard?.writeText(t.id); done(); }}>Copy ID <span className="ml-auto font-mono text-xs text-zinc-400">{t.id}</span></MenuItem>
              {!closed && !locked && <MenuItem icon={XCircle} onClick={() => { close.mutate("cancelled"); done(); }}>Cancel task</MenuItem>}
              {locked ? <MenuItem icon={RotateCcw} onClick={() => { restore.mutate(); done(); }}>Restore</MenuItem> : <MenuItem icon={Trash2} danger onClick={() => { trash.mutate(); done(); }}>Move to trash</MenuItem>}
            </>
          )}
        </Menu>
      </div>

      <div className="mb-4 flex items-start gap-3">
        <div className="pt-1.5 pl-1"><TaskCheck t={t} disabled={locked || close.isPending || update.isPending} onToggle={() => (closed ? update.mutate({ status: "open" }) : close.mutate("done"))} /></div>
        <TitleEditor t={t} disabled={locked} onSave={(title) => update.mutate({ title })} />
      </div>

      <div className="mb-5 flex flex-wrap gap-2">
        <Pill label="Status" className={statusTone[t.status]} select={!locked && (
          <select className={overlay} value={t.status} onChange={(e) => setStatus(e.target.value)} aria-label="Status">
            {(["open", "in_progress", "blocked", "done", "cancelled"] as const).map((s) => <option key={s} value={s}>{statusLabel[s]}</option>)}
          </select>
        )}>{statusLabel[t.status]}</Pill>
        <Pill label="Due" icon={CalendarDays} className={cn(due?.tone === "overdue" && !closed && "bg-red-50 text-red-700", due?.tone === "today" && !closed && "bg-blue-50 text-blue-700")} select={!locked && (
          <input ref={dueRef} type="date" className={overlay} value={t.due ?? ""} onClick={() => { try { dueRef.current?.showPicker(); } catch { /* not supported */ } }} onChange={(e) => update.mutate({ due: e.target.value })} aria-label="Due" />
        )}>{due ? due.text : "No date"}</Pill>
        {t.due && !locked && <button onClick={() => update.mutate({ due: "" })} aria-label="Clear due date" className="-ml-1.5 flex size-8 items-center justify-center rounded-full text-zinc-400 hover:bg-zinc-100"><X className="size-3.5" /></button>}
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
        <Pill label="Project" icon={Folder} select={!locked && (
          <select className={overlay} value={t.project_id} onChange={(e) => update.mutate({ project: e.target.value })} aria-label="Project">
            {projects.data?.projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
        )}>{t.project}</Pill>
      </div>

      {t.claimed_by && !closed && (
        <div className={cn("mb-5 flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm", t.claimed_by.kind === "agent" ? "bg-violet-50 text-violet-900" : "bg-amber-50 text-amber-900")}>
          <span className="relative flex size-2"><span className={cn("absolute inline-flex size-full animate-ping rounded-full opacity-60", t.claimed_by.kind === "agent" ? "bg-violet-400" : "bg-amber-400")} /><span className={cn("relative inline-flex size-2 rounded-full", t.claimed_by.kind === "agent" ? "bg-violet-500" : "bg-amber-500")} /></span>
          <span className="min-w-0 flex-1"><b className="font-medium">{t.claimed_by.name}</b> is working on this{t.claim_expires_at && <span className="opacity-60"> · until {new Date(t.claim_expires_at).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })}</span>}</span>
          {!locked && <Button size="sm" variant="outline" onClick={() => release.mutate()}>Release</Button>}
        </div>
      )}
      {!t.claimed_by && t.ready && !closed && !locked && (
        <button onClick={() => claim.mutate()} className="mb-5 flex w-full items-center gap-2 rounded-xl border border-dashed border-zinc-300 px-3 py-2.5 text-left text-sm text-zinc-600 hover:border-zinc-400 hover:bg-white">
          Ready to start — <span className="font-medium text-zinc-900">I'm on it</span>
        </button>
      )}

      <ErrorText error={err} />

      <div className="mb-6"><BodyEditor key={t.body ?? ""} t={t} disabled={locked} onSave={(body) => update.mutate({ body, body_mode: "replace", confirm: true })} /></div>

      <section className="mb-6" aria-label="Dependencies">
        {(t.blocked_by.length > 0 || t.blocks.length > 0) && (
          <div className="mb-2 flex flex-col gap-1.5">
            {t.blocked_by.map((b) => (
              <div key={b} className="flex items-center gap-2 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900">
                <Lock className="size-3.5" />Waits on <Link to="/tasks/$id" params={{ id: b }} className="font-mono text-xs underline-offset-2 hover:underline">{b}</Link>
                {!locked && <button className="ml-auto text-xs text-amber-700/70 hover:text-red-600" onClick={() => link.mutate({ blocker: b, remove: true })}>Remove</button>}
              </div>
            ))}
            {t.blocks.map((b) => (
              <div key={b} className="flex items-center gap-2 rounded-lg bg-zinc-100 px-3 py-2 text-sm text-zinc-600">
                Blocks <Link to="/tasks/$id" params={{ id: b }} className="font-mono text-xs underline-offset-2 hover:underline">{b}</Link>
              </div>
            ))}
          </div>
        )}
        {!locked && (
          <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); link.mutate({ blocker }); }}>
            <Input placeholder="Waits on task ID…" value={blocker} onChange={(e) => setBlocker(e.target.value)} className="h-9 max-w-56 sm:h-8" aria-label="Blocker id" />
            {blocker && <Button size="sm" variant="outline">Add</Button>}
          </form>
        )}
      </section>

      {comments.length > 0 && (
        <section className="mb-4 flex flex-col gap-2" aria-label="Comments">
          {comments.map((c) => (
            <div key={c.id} className="flex gap-2.5">
              <Avatar m={c.author} className="mt-0.5" />
              <div className="min-w-0 flex-1 rounded-2xl rounded-tl-md bg-white px-3 py-2 text-[15px] shadow-[0_0_0_1px_rgba(0,0,0,0.06)] sm:text-sm">
                <div className="mb-0.5 text-xs text-zinc-500"><Who m={c.author} by={c.on_behalf_of} /> · {ago(c.created_at)}</div>
                <div className="whitespace-pre-wrap">{c.body}</div>
              </div>
            </div>
          ))}
        </section>
      )}

      <details className="group mb-4 rounded-xl border border-zinc-200 bg-white">
        <summary className="flex cursor-pointer list-none items-center gap-2 px-4 py-3 text-sm text-zinc-500 [&::-webkit-details-marker]:hidden">
          <ChevronDown className="size-4 -rotate-90 transition-transform group-open:rotate-0" />Activity<span className="text-zinc-400">{activity.length}</span>
          <span className="ml-auto truncate text-xs text-zinc-400">Created by {t.created_by?.name ?? "someone"}{t.on_behalf_of ? ` for ${t.on_behalf_of.name}` : ""} · {ago(t.created_at)}</span>
        </summary>
        <div className="border-t border-zinc-100 px-4 py-2">
          {activity.map((e) => <EventLine key={e.id} e={e} showTask={false} />)}
        </div>
      </details>

      {!locked && (
        <form
          className="sticky bottom-[calc(5.5rem+env(safe-area-inset-bottom))] z-10 mt-auto flex items-end gap-2 rounded-2xl border border-zinc-200 bg-white p-1.5 pl-3 shadow-lg md:bottom-4"
          onSubmit={(e) => { e.preventDefault(); if (comment.trim()) addComment.mutate(); }}
        >
          <textarea
            rows={1}
            placeholder="Add a comment…"
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); e.currentTarget.form?.requestSubmit(); } }}
            aria-label="Comment"
            className="max-h-40 min-h-9 flex-1 resize-none bg-transparent py-1.5 text-base outline-none [field-sizing:content] placeholder:text-zinc-400 sm:text-sm"
          />
          <button disabled={!comment.trim() || addComment.isPending} aria-label="Send comment" className="flex size-9 shrink-0 items-center justify-center rounded-full bg-zinc-900 text-white disabled:bg-zinc-200 disabled:text-zinc-400">
            <ArrowUp className="size-4" />
          </button>
        </form>
      )}
    </div>
  );
}
