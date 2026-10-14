import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouterState } from "@tanstack/react-router";
import { Bot, CalendarDays, Flag, Folder, User } from "lucide-react";
import { api, cn, type Member, type Project } from "../lib";
import { dueLabel, parseQuickAdd } from "../quickadd";
import { ErrorText } from "./app";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { Textarea } from "@/components/ui/textarea";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { Drawer, DrawerContent, DrawerTitle } from "@/components/ui/drawer";
import { useIsMobile } from "@/hooks/use-mobile";
import { inJump, typing } from "./shortcuts";

type Ctx = { open: (project?: string) => void };
const QuickAddContext = createContext<Ctx>({ open: () => {} });

export const useQuickAdd = () => useContext(QuickAddContext);

export function QuickAddProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<{ project?: string } | null>(null);
  const path = useRouterState({ select: (s) => s.location.pathname });
  const currentProject = path.match(/^\/projects\/([^/]+)/)?.[1];
  const ctx = useMemo<Ctx>(() => ({ open: (project) => setState({ project }) }), []);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || inJump()) return;
      if ((e.key === "n" || e.key === "c") && !e.metaKey && !e.ctrlKey && !e.altKey && !typing(e.target)) {
        e.preventDefault();
        setState((s) => s ?? { project: currentProject });
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
      api.tool("create_tasks", { tasks: [{ title: parsed.title, body: body || undefined, assignee: final.assignee || undefined, priority: final.priority, due: final.due || undefined, project: final.project || undefined }] }),
    onSuccess: () => {
      qc.invalidateQueries();
      onClose();
    },
  });
  return (
    <form
      className="pt-3 sm:pt-0"
      onSubmit={(e) => { e.preventDefault(); if (parsed.title) m.mutate(); }}
    >
      <input
        autoFocus
        aria-label="Title"
        className="w-full bg-transparent text-lg font-medium outline-none placeholder:text-muted-foreground/70 sm:text-base"
        placeholder="Send contract to legal tomorrow @Marta #Sales"
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      <div className="mt-3 flex flex-wrap items-center gap-1.5">
        <Chip icon={Folder}>{projectName}</Chip>
        {assignee && <Chip icon={assignee.kind === "agent" ? Bot : User} tone={assignee.kind === "agent" ? "bg-violet-50 text-violet-700" : ""}>{assignee.name}</Chip>}
        {final.due && <Chip icon={CalendarDays} tone={dueLabel(final.due).tone === "overdue" ? "bg-red-50 text-red-700" : ""}>{dueLabel(final.due).text}</Chip>}
        {final.priority < 2 && <Chip icon={Flag} tone={final.priority === 0 ? "bg-red-50 text-red-700" : "bg-orange-50 text-orange-700"}>{final.priority === 0 ? "Urgent" : "High"}</Chip>}
      </div>
      <p className="mt-2 text-xs text-muted-foreground">Type @person, #project, a date like “friday” or “Oct 14”, and !0 or !1 for urgent.</p>
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
