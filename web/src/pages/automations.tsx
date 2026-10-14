import { useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pause, Plus, Repeat as RepeatIcon, Webhook, Zap } from "lucide-react";
import { toast } from "sonner";
import { api, cn, projectName, type Member, type MemberRef, type Project, type Task } from "../lib";
import { isoDate, repeatLabel, scheduleLabel, type Repeat } from "../quickadd";
import { Avatar, Card, DuePill, ErrorText, PageHeader } from "../components/app";
import { TaskLink, usePanelTask } from "../components/tasklink";
import { patchTask, undoable } from "../components/feedback";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { Skeleton } from "@/components/ui/skeleton";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useMe } from "../main";
import { useInvalidate } from "./tasks";

// An automation is a repeating task: completing one creates the next, and an agent with a
// webhook is called when it comes due. This page lists every open one the viewer can see.

const repeats = Object.keys(repeatLabel) as Repeat[];

export function AutomationsPage() {
  const q = useQuery({ queryKey: ["tasks", "automations"], queryFn: allAutomations });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const [creating, setCreating] = useState(false);
  // Paused rows drop out at once (the optimistic patch clears repeat); soonest first.
  const tasks = q.data?.tasks.filter((t) => t.repeat).sort((a, b) => (a.due ?? "9999").localeCompare(b.due ?? "9999"));
  const byId = new Map(members.data?.members.map((m) => [m.id, m]));
  return (
    <>
      <PageHeader title="Automations">
        <Button size="sm" onClick={() => setCreating(true)}><Plus className="size-4" />New automation</Button>
      </PageHeader>
      <ErrorText error={q.error} />
      {!tasks ? (
        <AutomationsSkeleton />
      ) : tasks.length === 0 ? (
        <Card className="flex flex-col items-center px-6 py-12 text-center">
          <span className="mb-3 flex size-10 items-center justify-center rounded-full bg-violet-100 text-violet-700"><Zap className="size-5" /></span>
          <p className="max-w-md text-sm text-foreground/80">Automations are tasks that repeat on a schedule, usually handed to an agent. Each time one comes due its assignee picks it up, and finishing it schedules the next.</p>
          <p className="mt-2 text-sm text-muted-foreground">For example: every Friday, Report bot writes the weekly summary.</p>
          <Button size="sm" variant="secondary" className="mt-5" onClick={() => setCreating(true)}><Plus className="size-4" />New automation</Button>
        </Card>
      ) : (
        <Card className="divide-y divide-border overflow-hidden">
          {tasks.map((t) => <AutomationRow key={t.id} t={t} agent={t.assignee ? byId.get(t.assignee.id) : undefined} />)}
        </Card>
      )}
      <NewAutomation open={creating} onOpenChange={setCreating} />
    </>
  );
}

function AutomationsSkeleton() {
  return (
    <Card className="divide-y divide-border" aria-busy="true" aria-label="Loading automations">
      {[55, 40, 65].map((w) => (
        <div key={w} className="flex flex-col gap-2 px-4 py-3.5">
          <Skeleton className="h-3.5 rounded-full" style={{ width: `${w}%` }} />
          <Skeleton className="h-3 w-1/3 rounded-full" />
        </div>
      ))}
    </Card>
  );
}

function AutomationRow({ t, agent }: { t: Task; agent?: Member }) {
  const qc = useQueryClient();
  const invalidate = useInvalidate();
  const selected = usePanelTask() === t.id;
  const was = t.repeat!;
  const pause = useMutation({
    mutationFn: () => api.tool("update_task", { id: t.id, repeat: "" }),
    onMutate: () => patchTask(qc, t.id, { repeat: undefined }),
    onError: (e, _v, rollback) => { rollback?.(); toast.error(e instanceof Error ? e.message : String(e)); },
    onSuccess: () => undoable(qc, `Paused “${t.title}”`, () => api.tool("update_task", { id: t.id, repeat: was })),
    onSettled: invalidate,
  });
  return (
    // Sized by the row, not the screen: the list narrows when the task panel and assistant are open.
    <div className="@container">
    <div className={cn("flex items-center gap-3 px-4 py-3 transition-colors hover:bg-muted/60", selected && "bg-accent hover:bg-accent")}>
      <TaskLink id={t.id} className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="truncate text-[15px] font-medium sm:text-sm">{t.title}</span>
        {/* Narrow rows: schedule and project, then who runs it. Wide rows: one line. */}
        <span className="flex min-w-0 flex-col gap-1 text-[13px] text-muted-foreground @2xl:flex-row @2xl:items-center @2xl:gap-2">
          <span className="flex min-w-0 items-center gap-2">
            <span className="flex min-w-0 items-center gap-1 font-medium text-foreground/80"><RepeatIcon aria-hidden className="size-3.5 shrink-0" /><span className="truncate">{scheduleLabel(was, t.due)}</span></span>
            <span aria-hidden>·</span>
            <span className="truncate">{t.project}</span>
          </span>
          <span aria-hidden className="hidden @2xl:inline">·</span>
          <Runner m={t.assignee} agent={agent} />
        </span>
      </TaskLink>
      <div className="flex shrink-0 items-center gap-2">
        {t.due && <span className="flex items-center gap-1.5"><span className="hidden text-xs text-muted-foreground @lg:inline">Next</span><DuePill due={t.due} /></span>}
        <Button size="sm" variant="ghost" className="text-muted-foreground" disabled={pause.isPending} onClick={() => pause.mutate()} aria-label={`Pause ${t.title}`} title="Stop repeating; the current task stays open">
          <Pause className="size-3.5" /><span className="hidden @lg:inline">Pause</span>
        </Button>
      </div>
    </div>
    </div>
  );
}

// Runner is who does the work. For agents, list_members includes the webhook only for the agent's
// owner and admins, so "No webhook" shows only to people who could add one.
function Runner({ m, agent }: { m?: MemberRef; agent?: Member }) {
  const me = useMe();
  if (!m) return <span>Unassigned</span>;
  const canSee = !!agent && (me.data?.member?.role === "admin" || agent.owner?.id === me.data?.member?.id);
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <Avatar m={m} className="size-5" />
      <span className="truncate text-foreground/80">{m.name}</span>
      {m.kind === "agent" && <span className="shrink-0 rounded-full bg-violet-100 px-1.5 text-[11px] leading-4 font-medium text-violet-700">Agent</span>}
      {m.kind === "agent" && agent?.webhook?.url && (
        <span title="Its webhook is called when this comes due" className="flex shrink-0 items-center gap-0.5 whitespace-nowrap text-[11px] text-green-700"><Webhook className="size-3" />Webhook</span>
      )}
      {m.kind === "agent" && canSee && !agent?.webhook?.url && (
        <span title="No webhook: the agent sees it next time it checks Handoff. Add one in Settings → Agents." className="shrink-0 whitespace-nowrap text-[11px] text-muted-foreground/80">No webhook</span>
      )}
    </span>
  );
}

const blank = () => ({ title: "", repeat: "weekly" as Repeat, due: isoDate(new Date()), assignee: "", project: "" });

function NewAutomation({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const me = useMe();
  const qc = useQueryClient();
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  // The workspace's triage agent (the one behind the assistant) is the natural default runner.
  const assistant = useQuery({ queryKey: ["assistant"], queryFn: () => api.tool<{ enabled: boolean; agent?: MemberRef }>("get_assistant"), refetchInterval: false });
  const [f, setF] = useState(blank);
  const people = members.data?.members.filter((m) => m.status === "active" || m.status === "invited") ?? [];
  const agents = people.filter((m) => m.kind === "agent");
  const humans = people.filter((m) => m.kind === "human");
  const fallback = assistant.data?.agent && people.some((m) => m.id === assistant.data.agent!.id) ? assistant.data.agent.id : me.data?.member?.id ?? "";
  const assignee = f.assignee || fallback;
  const list = projects.data?.projects ?? [];
  const project = f.project || list.find((p) => p.name === "Inbox")?.id || list[0]?.id || "";
  const close = (o: boolean) => { if (!o) setF(blank()); onOpenChange(o); };
  // One id per submission: a repeated submit or retry returns the same automation instead of a second one.
  const submission = useRef<string | null>(null);
  const create = useMutation({
    mutationFn: () => {
      submission.current ??= `automation-${crypto.randomUUID()}`;
      const due = f.repeat === "weekdays" && f.due ? nextWeekday(f.due) : f.due;
      return api.tool("create_tasks", { tasks: [{ title: f.title.trim(), repeat: f.repeat, due: due || undefined, assignee: assignee || undefined, project: project || undefined, external_id: submission.current }] });
    },
    onSuccess: () => {
      submission.current = null;
      toast(`Scheduled “${f.title.trim()}”`, { description: scheduleLabel(f.repeat, f.due) });
      qc.invalidateQueries();
      close(false);
    },
  });
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="rounded-2xl sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New automation</DialogTitle>
          <DialogDescription>A task that repeats. Finishing one schedules the next.</DialogDescription>
        </DialogHeader>
        <form id="new-automation" className="flex flex-col gap-3 text-sm" onSubmit={(e) => { e.preventDefault(); if (f.title.trim() && !create.isPending) create.mutate(); }}>
          <label className="flex flex-col gap-1.5 font-medium">What
            <Input required autoFocus placeholder="Write the weekly summary" value={f.title} onChange={(e) => setF({ ...f, title: e.target.value })} />
          </label>
          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1.5 font-medium *:data-[slot=native-select-wrapper]:w-full">How often
              <NativeSelect className="w-full" value={f.repeat} onChange={(e) => setF({ ...f, repeat: e.target.value as Repeat })}>
                {repeats.map((r) => <option key={r} value={r}>{repeatLabel[r]}</option>)}
              </NativeSelect>
            </label>
            <label className="flex flex-col gap-1.5 font-medium">First run
              <Input type="date" required value={f.due} onChange={(e) => setF({ ...f, due: e.target.value })} />
            </label>
          </div>
          <p className="-mt-1 text-xs text-muted-foreground">{scheduleLabel(f.repeat, f.due)}</p>
          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1.5 font-medium *:data-[slot=native-select-wrapper]:w-full">Who
              <NativeSelect className="w-full" value={assignee} onChange={(e) => setF({ ...f, assignee: e.target.value })}>
                {agents.length > 0 && <optgroup label="Agents">{agents.map((m) => <option key={m.id} value={m.id}>{m.name}</option>)}</optgroup>}
                <optgroup label="People">{humans.map((m) => <option key={m.id} value={m.id}>{m.id === me.data?.member?.id ? `${m.name} (me)` : m.name}</option>)}</optgroup>
              </NativeSelect>
            </label>
            <label className="flex flex-col gap-1.5 font-medium *:data-[slot=native-select-wrapper]:w-full">Project
              <NativeSelect className="w-full" value={project} onChange={(e) => setF({ ...f, project: e.target.value })}>
                {list.map((p) => <option key={p.id} value={p.id}>{projectName(p)}</option>)}
              </NativeSelect>
            </label>
          </div>
          <ErrorText error={create.error} />
        </form>
        <DialogFooter>
          <Button variant="outline" onClick={() => close(false)}>Cancel</Button>
          <Button type="submit" form="new-automation" disabled={create.isPending}>Create</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// Every repeating task the person can see, a page at a time.
async function allAutomations() {
  const page = 500;
  const tasks: Task[] = [];
  for (;;) {
    const r = await api.tool<{ tasks: Task[] }>("search_tasks", { repeating: true, limit: page, offset: tasks.length });
    tasks.push(...r.tasks);
    if (r.tasks.length < page) return { tasks };
  }
}

// A weekday series that would start on a weekend starts on the Monday after.
function nextWeekday(due: string) {
  const [y, m, d] = due.split("-").map(Number);
  const at = new Date(y, m - 1, d);
  while (at.getDay() === 0 || at.getDay() === 6) at.setDate(at.getDate() + 1);
  return isoDate(at);
}
