import * as React from "react";
import { Bot, User } from "lucide-react";
import { cn, type MemberRef, type Task, statusLabel } from "../lib";

export function Button({ className, variant = "default", size = "md", ...props }: React.ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "default" | "outline" | "ghost" | "danger"; size?: "sm" | "md" }) {
  return (
    <button
      className={cn(
        "inline-flex items-center justify-center gap-1.5 rounded-md font-medium transition-colors disabled:pointer-events-none disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-900",
        size === "sm" ? "h-8 px-3 text-sm" : "h-9 px-4 text-sm",
        variant === "default" && "bg-zinc-900 text-white hover:bg-zinc-700",
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
  <input ref={ref} className={cn("h-9 w-full rounded-md border border-zinc-300 bg-white px-3 text-sm outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900", className)} {...props} />
));

export function Textarea({ className, ...props }: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={cn("w-full rounded-md border border-zinc-300 bg-white px-3 py-2 text-sm outline-none focus:border-zinc-900 focus:ring-1 focus:ring-zinc-900", className)} {...props} />;
}

export function Select({ className, ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={cn("h-9 rounded-md border border-zinc-300 bg-white px-2 text-sm outline-none focus:border-zinc-900", className)} {...props} />;
}

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("rounded-lg border border-zinc-200 bg-white", className)} {...props} />;
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
    <div className="mb-4 flex items-center justify-between gap-4">
      <h1 className="text-xl font-semibold">{title}</h1>
      <div className="flex items-center gap-2">{children}</div>
    </div>
  );
}
