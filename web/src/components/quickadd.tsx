import { createContext, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouterState } from "@tanstack/react-router";
import { Bot, CalendarDays, Flag, Folder, Repeat as RepeatIcon, User, X } from "lucide-react";
import { api, cn, type Member, type Project } from "../lib";
import { dueLabel, parseQuickAdd, repeatLabel } from "../quickadd";
import { ErrorText, StatusCircle } from "./app";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Drawer, DrawerContent, DrawerTitle } from "@/components/ui/drawer";
import { useIsMobile } from "@/hooks/use-mobile";
import { useDictation } from "@/hooks/use-dictation";
import { MicButton, Waveform, privacyNote } from "./dictation";
import { inJump, typing } from "./shortcuts";

// open() starts a new task: in the page's inline composer when it has one (task lists do), in the
// dialog otherwise. The composer registers itself while it's on screen.
type Ctx = { open: (project?: string) => void; register: (focus: () => void) => () => void };
const QuickAddContext = createContext<Ctx>({ open: () => {}, register: () => () => {} });

export const useQuickAdd = () => useContext(QuickAddContext);

export function QuickAddProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<{ project?: string } | null>(null);
  const path = useRouterState({ select: (s) => s.location.pathname });
  const currentProject = path.match(/^\/projects\/([^/]+)/)?.[1];
  const inline = useRef<(() => void) | null>(null);
  const ctx = useMemo<Ctx>(() => ({
    open: (project) => (inline.current ? inline.current() : setState({ project })),
    register: (focus) => { inline.current = focus; return () => { if (inline.current === focus) inline.current = null; }; },
  }), []);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || inJump()) return;
      if ((e.key === "n" || e.key === "c") && !e.metaKey && !e.ctrlKey && !e.altKey && !typing(e.target)) {
        e.preventDefault();
        if (inline.current) inline.current();
        else setState((s) => s ?? { project: currentProject });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [currentProject]);
  return (
    <QuickAddContext.Provider value={ctx}>
      {children}
      <QuickAddOverlay state={state} onClose={() => setState(null)} />
    </QuickAddContext.Provider>
  );
}

type Overrides = { project?: string; assignee?: string; priority?: string; due?: string };
type Draft = { text: string; body: string; details: boolean; o: Overrides };
const emptyDraft: Draft = { text: "", body: "", details: false, o: {} };

function Chip({ icon: Icon, children, tone }: { icon: typeof Flag; children: React.ReactNode; tone?: string }) {
  return <span className={cn("inline-flex items-center gap-1 rounded-full bg-accent px-2 py-0.5 text-xs font-medium text-foreground/80", tone)}><Icon className="size-3" />{children}</span>;
}

// QuickAddOverlay is a centred dialog on desktop and a bottom drawer on phones; both wrap the same form.
// The draft lives here, above the Dialog/Drawer switch, so crossing the breakpoint keeps what was typed.
function QuickAddOverlay({ state, onClose }: { state: { project?: string } | null; onClose: () => void }) {
  const isMobile = useIsMobile();
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const close = () => { setDraft(emptyDraft); onClose(); };
  const onOpenChange = (open: boolean) => { if (!open) close(); };
  const form = state && <QuickAddForm project={state.project} draft={draft} onDraft={(d) => setDraft((cur) => ({ ...cur, ...d }))} onClose={close} />;
  if (isMobile) {
    return (
      <Drawer open={!!state} onOpenChange={onOpenChange}>
        <DrawerContent className="px-4 pb-[calc(1rem+env(safe-area-inset-bottom))]">
          <DrawerTitle className="sr-only">New task</DrawerTitle>
          <div className="min-h-0 overflow-y-auto">{form}</div>
        </DrawerContent>
      </Drawer>
    );
  }
  return (
    <Dialog open={!!state} onOpenChange={onOpenChange}>
      <DialogContent showCloseButton={false} aria-describedby={undefined} className="top-24 translate-y-0 gap-0 rounded-2xl p-4 sm:max-w-lg">
        <DialogTitle className="sr-only">New task</DialogTitle>
        {form}
      </DialogContent>
    </Dialog>
  );
}

function QuickAddForm({ project, draft, onDraft, onClose }: { project?: string; draft: Draft; onDraft: (d: Partial<Draft>) => void; onClose: () => void }) {
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const qc = useQueryClient();
  const { text, body, details, o } = draft;
  const setText = (text: string) => onDraft({ text });
  const setBody = (body: string) => onDraft({ body });
  const setDetails = (details: boolean) => onDraft({ details });
  const setO = (o: Overrides) => onDraft({ o });
  // Dictation writes into the same title text, so the parser's chips update as the words stream in.
  const dictation = useDictation(setText);
  const people = members.data?.members.filter((m) => m.status === "active" || m.status === "invited") ?? [];
  const parsed = parseQuickAdd(text, { members: people, projects: projects.data?.projects ?? [] });
  const final = {
    project: o.project ?? parsed.project?.id ?? project ?? "",
    assignee: o.assignee ?? parsed.assignee?.id ?? "",
    priority: Number(o.priority ?? parsed.priority ?? 2),
    due: o.due ?? parsed.due ?? "",
  };
  const projectName = projects.data?.projects.find((p) => p.id === final.project)?.name ?? "Inbox";
  const assignee = people.find((m) => m.id === final.assignee);
  const m = useMutation({
    mutationFn: () =>
      api.tool("create_tasks", { tasks: [{ title: parsed.title, body: body || undefined, assignee: final.assignee || undefined, unassigned: o.assignee === "" ? true : undefined, repeat: parsed.repeat, priority: o.priority !== undefined || parsed.priority !== undefined ? final.priority : undefined, due: final.due || undefined, project: final.project || undefined }] }),
    onSuccess: () => {
      qc.invalidateQueries();
      onClose();
    },
  });
  return (
    <form
      className="pt-3 sm:pt-0"
      onSubmit={(e) => { e.preventDefault(); dictation.cancel(); if (parsed.title) m.mutate(); }}
    >
      <div className="flex items-center gap-2">
        <input
          autoFocus
          aria-label="Title"
          className="min-w-0 flex-1 bg-transparent text-lg font-medium outline-none placeholder:text-muted-foreground/70 sm:text-base"
          placeholder="Send contract to legal tomorrow @Marta #Sales"
          value={text}
          onChange={(e) => { dictation.cancel(); setText(e.target.value); }}
        />
        {dictation.supported && <MicButton listening={dictation.listening} onToggle={() => dictation.toggle(text)} className="-my-1.5" />}
      </div>
      {dictation.listening && (
        <button
          type="button"
          onClick={dictation.stop}
          aria-label="Listening. Tap to stop"
          className="mt-3 flex w-full items-center gap-2.5 rounded-full bg-red-50 py-1.5 pr-3 pl-2.5 text-sm text-red-700 motion-safe:animate-in motion-safe:fade-in-0"
        >
          <span className="size-2 shrink-0 rounded-full bg-red-500 motion-safe:animate-pulse" />
          <Waveform analyser={dictation.analyser} bars={20} />
          <span className="font-medium">Listening…</span>
          <span className="ml-auto text-xs text-red-700/70">Tap to stop</span>
        </button>
      )}
      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        <Chip icon={Folder}>{projectName}</Chip>
        {assignee && <Chip icon={assignee.kind === "agent" ? Bot : User} tone={assignee.kind === "agent" ? "bg-violet-50 text-violet-700" : ""}>{assignee.name}</Chip>}
        {final.due && <Chip icon={CalendarDays} tone={dueLabel(final.due).tone === "overdue" ? "bg-red-50 text-red-700" : ""}>{dueLabel(final.due).text}</Chip>}
        {parsed.repeat && <Chip icon={RepeatIcon}>{repeatLabel[parsed.repeat]}</Chip>}
        {final.priority < 2 && <Chip icon={Flag} tone={final.priority === 0 ? "bg-red-50 text-red-700" : "bg-orange-50 text-orange-700"}>{final.priority === 0 ? "Urgent" : "High"}</Chip>}
      </div>
      <p className="mt-2 text-xs text-muted-foreground">
        {dictation.listening ? privacyNote : "Type @person, #project, a date like “friday” or “Oct 14”, and !0 or !1 for urgent."}
      </p>
      {details && (
        <div className="mt-3 flex flex-col gap-2">
          <Textarea rows={3} placeholder="Details (optional)" value={body} onChange={(e) => setBody(e.target.value)} aria-label="Details" />
          <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
            <NativeSelect value={final.project} onChange={(e) => setO({ ...o, project: e.target.value })} aria-label="Project">
              <option value="">Inbox</option>
              {projects.data?.projects.filter((p) => p.name !== "Inbox").map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
            </NativeSelect>
            <NativeSelect value={final.assignee} onChange={(e) => setO({ ...o, assignee: e.target.value })} aria-label="Assignee">
              <option value="">Unassigned</option>
              {people.map((x) => <option key={x.id} value={x.id}>{x.name}{x.kind === "agent" ? " (agent)" : ""}</option>)}
            </NativeSelect>
            <NativeSelect value={final.priority} onChange={(e) => setO({ ...o, priority: e.target.value })} aria-label="Priority">
              {[0, 1, 2, 3, 4].map((p) => <option key={p} value={p}>P{p}</option>)}
            </NativeSelect>
            <Input type="date" className="sm:w-auto" value={final.due} onChange={(e) => setO({ ...o, due: e.target.value })} aria-label="Due" />
          </div>
        </div>
      )}
      <ErrorText error={m.error} />
      <div className="mt-4 flex items-center gap-2">
        {!details && <Button type="button" variant="ghost" size="sm" onClick={() => setDetails(true)}>Details</Button>}
        <Button type="button" variant="ghost" className="ml-auto" onClick={onClose}>Cancel</Button>
        <Button type="submit" disabled={!parsed.title || m.isPending}>Add task</Button>
      </div>
    </form>
  );
}

const pill = "relative inline-flex h-8 shrink-0 items-center gap-1.5 rounded-full bg-accent px-3 text-[13px] font-medium text-foreground/80 transition-colors hover:bg-foreground/10 focus-within:ring-2 focus-within:ring-ring/40";
const overlay = "absolute inset-0 size-full cursor-pointer appearance-none opacity-0";

// InlineNewTask is the composer at the top of a task list: type a title and press Enter, and the task
// is added right there; the field stays ready for the next one. Pills set its status, due date and
// project; @person, #project, "friday" and !1 in the title work as in the dialog. Esc clears it.
// While it's on screen, N (and every New task button) comes here instead of opening the dialog.
export function InlineNewTask({ project }: { project?: string }) {
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const qc = useQueryClient();
  const quick = useQuickAdd();
  const input = useRef<HTMLInputElement>(null);
  const dueRef = useRef<HTMLInputElement>(null);
  const [text, setText] = useState("");
  const [started, setStarted] = useState(false);
  const [o, setO] = useState<Overrides>({});
  const [focused, setFocused] = useState(false);
  useEffect(() => quick.register(() => { input.current?.focus(); input.current?.scrollIntoView({ block: "nearest" }); }), [quick]);

  const people = members.data?.members.filter((m) => m.status === "active" || m.status === "invited") ?? [];
  const parsed = parseQuickAdd(text, { members: people, projects: projects.data?.projects ?? [] });
  const final = { project: o.project ?? parsed.project?.id ?? project ?? "", due: o.due ?? parsed.due ?? "" };
  const projectLabel = projects.data?.projects.find((p) => p.id === final.project)?.name ?? "Inbox";
  const due = final.due ? dueLabel(final.due) : undefined;
  const assignee = parsed.assignee && people.find((m) => m.id === parsed.assignee!.id);
  const reset = () => { setText(""); setStarted(false); setO({}); };

  const m = useMutation({
    mutationFn: (v: { title: string; started: boolean }) =>
      api.tool<{ tasks: { id: string }[] }>("create_tasks", { tasks: [{ title: v.title, assignee: parsed.assignee?.id, repeat: parsed.repeat, priority: parsed.priority, due: final.due || undefined, project: final.project || undefined }] })
        .then(async (r) => { if (v.started && r.tasks[0]) await api.tool("update_task", { id: r.tasks[0].id, status: "in_progress" }); }),
    onSuccess: () => qc.invalidateQueries(),
  });
  const submit = () => {
    if (!parsed.title || m.isPending) return;
    m.mutate({ title: parsed.title, started });
    reset();
    input.current?.focus();
  };
  const open = focused || !!text;

  return (
    <form
      onSubmit={(e) => { e.preventDefault(); submit(); }}
      onFocus={() => setFocused(true)}
      onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setFocused(false); }}
      className={cn("mb-4 rounded-2xl border bg-card px-3.5 py-2.5 transition-[border-color,box-shadow]", open ? "border-foreground/25 shadow-sm" : "border-border hover:border-foreground/15")}
    >
      <input
        ref={input}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); if (text) reset(); else e.currentTarget.blur(); } }}
        placeholder="New task"
        aria-label="New task"
        maxLength={500}
        className="w-full bg-transparent py-1 text-[17px] outline-none placeholder:text-muted-foreground/60 sm:text-base"
      />
      <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
        <button type="button" onClick={() => setStarted((s) => !s)} aria-pressed={started} title="Status" className={pill}>
          <StatusCircle t={{ status: started ? "in_progress" : "open", ready: true, blocked_by: [] }} small />{started ? "In progress" : "Todo"}
        </button>
        <span className={cn(pill, !due && "text-muted-foreground", due?.tone === "overdue" && "bg-red-50 text-red-700")}>
          <CalendarDays className="size-3.5" />{due ? due.text : "Due date"}
          <input ref={dueRef} type="date" aria-label="Due date" value={final.due} onChange={(e) => setO({ ...o, due: e.target.value })} onClick={() => { try { dueRef.current?.showPicker(); } catch { /* not supported */ } }} className={overlay} />
        </span>
        {final.due && <button type="button" onClick={() => setO({ ...o, due: "" })} aria-label="Clear due date" className="-ml-1 flex size-6 items-center justify-center rounded-full text-muted-foreground/70 hover:bg-accent"><X className="size-3" /></button>}
        <span className={pill}>
          <Folder className="size-3.5" />{projectLabel}
          <select aria-label="Project" value={final.project} onChange={(e) => setO({ ...o, project: e.target.value })} className={overlay}>
            <option value="">Inbox</option>
            {projects.data?.projects.filter((p) => p.name !== "Inbox").map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
        </span>
        {assignee && <span className={cn(pill, assignee.kind === "agent" && "bg-violet-50 text-violet-700")}>{assignee.kind === "agent" ? <Bot className="size-3.5" /> : <User className="size-3.5" />}{assignee.name}</span>}
        {parsed.repeat && <span className={pill}><RepeatIcon className="size-3.5" />{repeatLabel[parsed.repeat]}</span>}
        {parsed.priority !== undefined && parsed.priority < 2 && <span className={cn(pill, parsed.priority === 0 ? "bg-red-50 text-red-700" : "bg-orange-50 text-orange-700")}><Flag className="size-3.5" />{parsed.priority === 0 ? "Urgent" : "High"}</span>}
        {text && <span className="ml-auto hidden text-xs text-muted-foreground [@media(pointer:fine)]:inline">↵ to add · Esc to clear</span>}
      </div>
      <ErrorText error={m.error} />
    </form>
  );
}
