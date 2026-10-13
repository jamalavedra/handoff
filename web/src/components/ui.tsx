import * as React from "react";
import { Bot, Check, User, X } from "lucide-react";
import { cn, type MemberRef, type Task, statusLabel } from "../lib";
import { dueLabel } from "../quickadd";

export function Button({ className, variant = "default", size = "md", ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "default" | "outline" | "ghost" | "danger"; size?: "sm" | "md" }) {
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors disabled:pointer-events-none disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent",
        size === "sm" ? "h-8 px-3 text-sm" : "h-9 px-4 text-sm",
        variant === "default" && "bg-accent text-on-accent hover:opacity-90",
        variant === "outline" && "border border-zinc-300 bg-white hover:bg-zinc-100",
        variant === "ghost" && "hover:bg-zinc-100",
        variant === "danger" && "bg-red-600 text-white hover:bg-red-500",
        className,
      )}
      {...props}
    />
  );
}

export const Input = React.forwardRef<HTMLInputElement, React.InputHTMLAttributes<HTMLInputElement>>(({ className, ...props }, ref) => (
  <input ref={ref} className={cn("h-10 w-full rounded-md border border-zinc-300 bg-white px-3 text-base outline-none sm:h-9 sm:text-sm focus:border-accent focus:ring-1 focus:ring-accent", className)} {...props} />
));

export function Textarea({ className, ...props }: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={cn("w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-base outline-none sm:text-sm focus:border-accent focus:ring-1 focus:ring-accent", className)} {...props} />;
}

export function Select({ className, ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={cn("h-10 rounded-md border border-zinc-300 bg-white px-2 text-base outline-none sm:h-9 sm:text-sm focus:border-accent", className)} {...props} />;
}

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("rounded-2xl border border-zinc-200/70 bg-white shadow-[0_1px_3px_rgba(0,0,0,0.04)]", className)} {...props} />;
}

export function Badge({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) {
  return <span className={cn("inline-flex items-center gap-1 rounded-full border border-zinc-200 bg-zinc-50 px-2 py-0.5 text-xs text-zinc-600", className)} {...props} />;
}

const statusColor: Record<Task["status"], string> = {
  open: "border-zinc-300 bg-white text-zinc-700",
  in_progress: "border-blue-200 bg-blue-50 text-blue-700",
  blocked: "border-amber-200 bg-amber-50 text-amber-700",
  done: "border-green-200 bg-green-50 text-green-700",
  cancelled: "border-zinc-200 bg-zinc-100 text-zinc-500",
};

export function StatusBadge({ status }: { status: Task["status"] }) {
  return <Badge className={statusColor[status]}>{statusLabel[status]}</Badge>;
}

export function Priority({ p }: { p: number }) {
  const c = ["text-red-600", "text-orange-600", "text-zinc-600", "text-zinc-400", "text-zinc-300"][p];
  return <span className={cn("font-mono text-xs font-semibold", c)}>P{p}</span>;
}

export function Who({ m, by }: { m?: MemberRef; by?: MemberRef }) {
  if (!m) return null;
  const Icon = m.kind === "agent" ? Bot : User;
  return (
    <span className="inline-flex items-center gap-1 text-zinc-700">
      <Icon className={cn("size-3.5", m.kind === "agent" ? "text-violet-600" : "text-zinc-500")} />
      {m.name}
      {by && <span className="text-zinc-400">for {by.name}</span>}
    </span>
  );
}

export function ErrorText({ error }: { error: unknown }) {
  if (!error) return null;
  return <p className="text-sm text-red-600">{error instanceof Error ? error.message : String(error)}</p>;
}

export function Empty({ children }: { children: React.ReactNode }) {
  return <div className="px-4 py-10 text-center text-sm text-zinc-500">{children}</div>;
}

export function PageHeader({ title, children }: { title: string; children?: React.ReactNode }) {
  return (
    <div className="mb-5 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 pt-1">
      <h1 className="text-[28px] leading-none font-extrabold tracking-tight uppercase md:text-3xl">{title}</h1>
      <div className="flex flex-wrap items-center gap-2">{children}</div>
    </div>
  );
}

export function Avatar({ m, className }: { m: MemberRef; className?: string }) {
  const initials = m.name.split(/\s+/).map((w) => w[0]).join("").slice(0, 2).toUpperCase();
  return (
    <span title={m.name} className={cn("inline-flex size-6 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold", m.kind === "agent" ? "bg-violet-100 text-violet-700" : "bg-zinc-200 text-zinc-700", className)}>
      {m.kind === "agent" ? <Bot className="size-3.5" /> : initials}
    </span>
  );
}

const dueTone = {
  overdue: "bg-red-50 text-red-700",
  today: "bg-accent-soft text-accent",
  soon: "bg-zinc-100 text-zinc-700",
  later: "bg-zinc-100 text-zinc-500",
};

export function DuePill({ due, done }: { due: string; done?: boolean }) {
  const l = dueLabel(due);
  return <span className={cn("shrink-0 rounded-full px-2 py-0.5 text-xs font-medium tabular-nums", done ? "bg-zinc-100 text-zinc-400" : dueTone[l.tone])}>{l.text}</span>;
}

// TaskCheck is the round checkbox: empty when open, half-filled while someone works on it.
export function TaskCheck({ t, onToggle, disabled }: { t: Task; onToggle: () => void; disabled?: boolean }) {
  const working = t.status === "in_progress" || !!t.claimed_by;
  const label = t.status === "done" || t.status === "cancelled" ? `Reopen ${t.title}` : `Mark ${t.title} done`;
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onToggle}
      className="-m-2 flex size-10 shrink-0 items-center justify-center rounded-full disabled:cursor-default sm:size-9"
    >
      {t.status === "done" ? (
        <span className="flex size-5 items-center justify-center rounded-full bg-zinc-900 text-white"><Check className="size-3.5" strokeWidth={3} /></span>
      ) : t.status === "cancelled" ? (
        <span className="flex size-5 items-center justify-center rounded-full border-[1.5px] border-zinc-300 text-zinc-400"><X className="size-3" strokeWidth={3} /></span>
      ) : working ? (
        <svg viewBox="0 0 20 20" className="size-5 text-amber-500" aria-hidden><circle cx="10" cy="10" r="8.25" fill="none" stroke="currentColor" strokeWidth="1.5" /><path d="M10 3.5a6.5 6.5 0 0 0 0 13z" fill="currentColor" /></svg>
      ) : (
        <span className={cn("size-5 rounded-full border-[1.5px] transition-colors", t.status === "blocked" || (!t.ready && t.blocked_by.length > 0) ? "border-dashed border-zinc-300" : "border-zinc-400 hover:border-zinc-900 hover:bg-zinc-100")} />
      )}
    </button>
  );
}
