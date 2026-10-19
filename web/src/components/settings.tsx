import * as React from "react";
import { useState } from "react";
import { Check, Copy, type LucideIcon } from "lucide-react";
import { cn } from "../lib";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

// Pieces every settings page shares, so Agents, Apps and Connections read as one place.

// SettingsSection is a titled block: a heading, a line on what it's for, and an optional action.
export function SettingsSection({ title, description, action, children, className }: {
  title: string; description?: React.ReactNode; action?: React.ReactNode; children: React.ReactNode; className?: string;
}) {
  return (
    <section className={cn("mb-10", className)}>
      <div className="mb-3 flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
        <div className="min-w-0 max-w-2xl">
          <h2 className="text-[15px] font-semibold tracking-tight">{title}</h2>
          {description && <p className="mt-0.5 text-sm text-muted-foreground">{description}</p>}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

// IconTile is the square a card leads with: an app's logo, or an icon.
export function IconTile({ icon: Icon, src, className }: { icon?: LucideIcon; src?: string; className?: string }) {
  return (
    <span className={cn("flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-xl border border-border bg-background", className)}>
      {src ? <img src={src} alt="" className="size-5 object-contain" /> : Icon && <Icon className="size-4 text-foreground/70" />}
    </span>
  );
}

// StatusDot says whether something is live, at a glance.
export function StatusDot({ tone, children }: { tone: "on" | "off" | "warn" | "error"; children: React.ReactNode }) {
  const dot = { on: "bg-green-500", off: "bg-foreground/25", warn: "bg-amber-500", error: "bg-red-500" }[tone];
  const text = { on: "text-green-700 dark:text-green-400", off: "text-muted-foreground", warn: "text-amber-700", error: "text-red-600" }[tone];
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-xs", text)}>
      <span className={cn("size-1.5 rounded-full", dot)} aria-hidden />
      {children}
    </span>
  );
}

// CopyField is a value to copy: an address on one line, or a prompt as a block.
export function CopyField({ text, label, block }: { text: string; label: string; block?: boolean }) {
  const [ok, setOk] = useState(false);
  const copy = () => { navigator.clipboard.writeText(text); setOk(true); setTimeout(() => setOk(false), 1500); };
  return (
    <div className={cn("flex gap-2 rounded-xl border border-border bg-muted pl-3", block ? "items-start py-2.5 pr-2" : "items-center py-1 pr-1")}>
      <pre className={cn("min-w-0 flex-1 font-mono text-xs leading-relaxed text-foreground/80", block ? "max-h-56 overflow-y-auto whitespace-pre-wrap break-words" : "truncate")}>{text}</pre>
      <Button type="button" size="xs" variant="ghost" onClick={copy} aria-label={label} className="shrink-0">
        {ok ? <><Check className="text-green-600" />Copied</> : <><Copy />Copy</>}
      </Button>
    </div>
  );
}

// Toggle is a switch for one setting, with its label.
export function Toggle({ checked, onChange, disabled, label, hint }: { checked: boolean; onChange: () => void; disabled?: boolean; label: string; hint?: string }) {
  return (
    <label className={cn("flex items-start justify-between gap-3 py-1.5 text-sm", disabled ? "opacity-60" : "cursor-pointer")}>
      <span className="min-w-0">
        <span className="block">{label}</span>
        {hint && <span className="block text-xs text-muted-foreground">{hint}</span>}
      </span>
      <Switch checked={checked} disabled={disabled} onChange={onChange} />
    </label>
  );
}

// Switch is the bare toggle, for rows that lay out their own label (wrap it in a <label>, or pass one).
export function Switch({ checked, disabled, onChange, label }: { checked: boolean; disabled?: boolean; onChange: () => void; label?: string }) {
  return (
    <span className="relative mt-0.5 inline-flex shrink-0">
      <input type="checkbox" role="switch" aria-label={label} className="peer sr-only" checked={checked} disabled={disabled} onChange={onChange} />
      <span className="h-5 w-9 rounded-full bg-foreground/15 transition-colors peer-checked:bg-primary peer-focus-visible:ring-[3px] peer-focus-visible:ring-ring/50" />
      <span className="pointer-events-none absolute top-0.5 left-0.5 size-4 rounded-full bg-background shadow-sm transition-transform peer-checked:translate-x-4" />
    </span>
  );
}

// ConfirmButton is every "disconnect", "remove" and "revoke": a quiet outline button that asks first,
// in a dialog that says what happens, and shows the action running until it's done.
export function ConfirmButton({ icon: Icon, label, title, description, confirmLabel, pendingLabel, onConfirm, size = "sm", className }: {
  icon?: LucideIcon;
  label: string;
  title: string;
  description: React.ReactNode;
  confirmLabel?: string;
  pendingLabel?: string;
  onConfirm: () => Promise<unknown>;
  size?: "sm" | "xs";
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const run = async () => {
    setBusy(true);
    setError(null);
    try {
      await onConfirm();
      setOpen(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Button
        type="button"
        size={size}
        variant="outline"
        onClick={() => { setError(null); setOpen(true); }}
        className={cn("text-foreground/70 hover:border-red-200 hover:bg-red-50 hover:text-red-600 dark:hover:border-red-900 dark:hover:bg-red-950", className)}
      >
        {Icon && <Icon />}{label}
      </Button>
      <Dialog open={open} onOpenChange={(o) => !busy && setOpen(o)}>
        <DialogContent className="rounded-2xl sm:max-w-md" showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
            <DialogDescription>{description}</DialogDescription>
          </DialogHeader>
          {error && <p className="text-sm text-red-600">{error}</p>}
          <DialogFooter>
            <Button type="button" variant="ghost" disabled={busy} onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="button" variant="destructive" disabled={busy} onClick={run} autoFocus>
              {busy ? pendingLabel ?? "Working…" : confirmLabel ?? label}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
