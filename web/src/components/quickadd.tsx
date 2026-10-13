import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRouterState } from "@tanstack/react-router";
import { Bot, CalendarDays, Flag, Folder, User } from "lucide-react";
import { api, cn, type Member, type Project } from "../lib";
import { dueLabel, parseQuickAdd } from "../quickadd";
import { Button, ErrorText, Input, Select, Textarea } from "./ui";

type Ctx = { open: (project?: string) => void };
const QuickAddContext = createContext<Ctx>({ open: () => {} });

export const useQuickAdd = () => useContext(QuickAddContext);

function typing(t: EventTarget | null) {
  const el = t as HTMLElement | null;
  return !!el && (el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName));
}

export function QuickAddProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<{ project?: string } | null>(null);
  const path = useRouterState({ select: (s) => s.location.pathname });
  const currentProject = path.match(/^\/projects\/([^/]+)/)?.[1];
  const ctx = useMemo<Ctx>(() => ({ open: (project) => setState({ project }) }), []);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setState(null);
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
      {state && <QuickAddSheet project={state.project} onClose={() => setState(null)} />}
    </QuickAddContext.Provider>
  );
}

type Overrides = { project?: string; assignee?: string; priority?: string; due?: string };

function Chip({ icon: Icon, children, tone }: { icon: typeof Flag; children: React.ReactNode; tone?: string }) {
  return <span className={cn("inline-flex items-center gap-1 rounded-full bg-zinc-100 px-2 py-0.5 text-xs font-medium text-zinc-700", tone)}><Icon className="size-3" />{children}</span>;
}

function QuickAddSheet({ project, onClose }: { project?: string; onClose: () => void }) {
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const qc = useQueryClient();
  const [text, setText] = useState("");
  const [body, setBody] = useState("");
  const [details, setDetails] = useState(false);
  const [o, setO] = useState<Overrides>({});
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
    <div className="fixed inset-0 z-40 flex items-end bg-black/30 sm:items-start sm:justify-center sm:p-4 sm:pt-24" onClick={onClose}>
      <form
        role="dialog"
        aria-label="New task"
        className="w-full rounded-t-2xl bg-white p-4 pb-[calc(1rem+env(safe-area-inset-bottom))] shadow-xl sm:max-w-lg sm:rounded-2xl sm:pb-4"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => { e.preventDefault(); if (parsed.title) m.mutate(); }}
      >
        <div className="mx-auto mb-3 h-1 w-10 rounded-full bg-zinc-200 sm:hidden" />
        <input
          autoFocus
          aria-label="Title"
          className="w-full bg-transparent text-lg font-medium outline-none placeholder:text-zinc-400 sm:text-base"
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
        <p className="mt-2 text-xs text-zinc-500">Type @person, #project, a date like “friday” or “Oct 14”, and !0 or !1 for urgent.</p>
        {details && (
          <div className="mt-3 flex flex-col gap-2">
            <Textarea rows={3} placeholder="Details (optional)" value={body} onChange={(e) => setBody(e.target.value)} aria-label="Details" />
            <div className="grid grid-cols-2 gap-2 sm:flex sm:flex-wrap">
              <Select value={final.project} onChange={(e) => setO({ ...o, project: e.target.value })} aria-label="Project">
                <option value="">Inbox</option>
                {projects.data?.projects.filter((p) => p.name !== "Inbox").map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
              </Select>
              <Select value={final.assignee} onChange={(e) => setO({ ...o, assignee: e.target.value })} aria-label="Assignee">
                <option value="">Unassigned</option>
                {people.map((x) => <option key={x.id} value={x.id}>{x.name}{x.kind === "agent" ? " (agent)" : ""}</option>)}
              </Select>
              <Select value={final.priority} onChange={(e) => setO({ ...o, priority: e.target.value })} aria-label="Priority">
                {[0, 1, 2, 3, 4].map((p) => <option key={p} value={p}>P{p}</option>)}
              </Select>
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
    </div>
  );
}
