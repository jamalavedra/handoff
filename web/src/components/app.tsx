import * as React from "react";
import { Bot, Check, User, X } from "lucide-react";
import { cn, type MemberRef, type Task, statusLabel } from "../lib";
import { dueLabel } from "../quickadd";

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("rounded-2xl border border-border bg-card", className)} {...props} />;
}

export function Badge({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) {
  return <span className={cn("inline-flex items-center gap-1 rounded-full border border-border bg-muted px-2 py-0.5 text-xs text-foreground/70", className)} {...props} />;
}

const statusColor: Record<Task["status"], string> = {
  open: "border-input bg-card text-foreground/80",
  in_progress: "border-blue-200 bg-blue-50 text-blue-700",
  blocked: "border-amber-200 bg-amber-50 text-amber-700",
  done: "border-green-200 bg-green-50 text-green-700",
  cancelled: "border-border bg-accent text-muted-foreground",
};

export function StatusBadge({ status }: { status: Task["status"] }) {
  return <Badge className={statusColor[status]}>{statusLabel[status]}</Badge>;
}

export function Priority({ p }: { p: number }) {
  const c = ["text-red-600", "text-orange-600", "text-foreground/70", "text-muted-foreground/70", "text-muted-foreground/50"][p];
  return <span className={cn("font-mono text-xs font-semibold", c)}>P{p}</span>;
}

export function Who({ m, by }: { m?: MemberRef; by?: MemberRef }) {
  if (!m) return null;
  const Icon = m.kind === "agent" ? Bot : User;
  return (
    <span className="inline-flex items-center gap-1 text-foreground/80">
      <Icon className={cn("size-3.5", m.kind === "agent" ? "text-violet-600" : "text-muted-foreground")} />
      {m.name}
      {by && <span className="text-muted-foreground/70">for {by.name}</span>}
    </span>
  );
}

export function ErrorText({ error }: { error: unknown }) {
  if (!error) return null;
  return <p className="text-sm text-red-600">{error instanceof Error ? error.message : String(error)}</p>;
}

export function Empty({ children }: { children: React.ReactNode }) {
  return <div className="px-4 py-10 text-center text-sm text-muted-foreground">{children}</div>;
}

export function PageHeader({ title, children }: { title: string; children?: React.ReactNode }) {
  return (
    <div className="mb-5 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 pt-1">
      <h1 className="text-[26px] leading-tight font-semibold tracking-tight md:text-[28px]">{title}</h1>
      <div className="flex flex-wrap items-center gap-2">{children}</div>
    </div>
  );
}

export function Avatar({ m, className }: { m: MemberRef; className?: string }) {
  const initials = m.name.split(/\s+/).map((w) => w[0]).join("").slice(0, 2).toUpperCase();
  return (
    <span title={m.name} className={cn("inline-flex size-6 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold", m.kind === "agent" ? "bg-violet-100 text-violet-700" : "bg-foreground/10 text-foreground/80", className)}>
      {m.kind === "agent" ? <Bot className="size-3.5" /> : initials}
    </span>
  );
}

const dueTone = {
  overdue: "bg-red-50 text-red-700",
  today: "bg-today/10 text-today",
  soon: "bg-accent text-foreground/80",
  later: "bg-accent text-muted-foreground",
};

export function DuePill({ due, done }: { due: string; done?: boolean }) {
  const l = dueLabel(due);
  return <span className={cn("shrink-0 rounded-full px-2 py-0.5 text-xs font-medium tabular-nums", done ? "bg-accent text-muted-foreground/70" : dueTone[l.tone])}>{l.text}</span>;
}

// TaskCheck is the round checkbox: empty when open, half-filled while someone works on it.
export function TaskCheck({ t, onToggle, disabled }: { t: Task; onToggle: () => void; disabled?: boolean }) {
  const label = t.status === "done" || t.status === "cancelled" ? `Reopen ${t.title}` : `Mark ${t.title} done`;
  return (
    <button
      type="button"
      data-task-check
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onToggle}
      className="-m-2 flex size-10 shrink-0 items-center justify-center rounded-full disabled:cursor-default sm:size-9"
    >
      <StatusCircle t={t} interactive />
    </button>
  );
}

// StatusCircle is the checkbox's face on its own, for places that show a task's state without toggling it.
export function StatusCircle({ t, small, interactive }: { t: Pick<Task, "status" | "claimed_by" | "ready" | "blocked_by">; small?: boolean; interactive?: boolean }) {
  const working = t.status === "in_progress" || !!t.claimed_by;
  const size = small ? "size-3.5" : "size-5";
  const ring = small ? "border-[1.25px]" : "border-[1.5px]";
  if (t.status === "done") return <span className={cn("flex shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground", size)}><Check className={small ? "size-2.5" : "size-3.5"} strokeWidth={3} /></span>;
  if (t.status === "cancelled") return <span className={cn("flex shrink-0 items-center justify-center rounded-full border-input text-muted-foreground/70", size, ring)}><X className={small ? "size-2" : "size-3"} strokeWidth={3} /></span>;
  if (working) return <svg viewBox="0 0 20 20" className={cn("shrink-0 text-amber-500", size)} aria-hidden><circle cx="10" cy="10" r="8.25" fill="none" stroke="currentColor" strokeWidth="1.5" /><path d="M10 3.5a6.5 6.5 0 0 0 0 13z" fill="currentColor" /></svg>;
  const waiting = t.status === "blocked" || (!t.ready && t.blocked_by.length > 0);
  return <span className={cn("shrink-0 rounded-full transition-colors", size, ring, waiting ? "border-dashed border-input" : "border-foreground/40", !waiting && interactive && "hover:border-foreground hover:bg-accent")} />;
}
