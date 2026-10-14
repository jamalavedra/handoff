import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Check, CheckCircle2, FileText, FolderKanban, Globe, Lightbulb, MessageSquare, Pencil, Plus, Search, Sparkles, Users } from "lucide-react";
import { api, cn, type MemberRef } from "../lib";
import { line, summary, type ActivityStep } from "../activity";
import { useAgentActivity } from "../agent-activity";
import { TaskLink } from "./tasklink";
import { Avatar } from "./app";
import { Trace, TraceCollapsible, TraceCollapsibleContent, TraceCollapsibleTrigger, TraceDescription, TraceIcon, TraceLine, TraceTitle } from "@/components/ui/trace";

const toolIcon: Record<string, typeof Search> = {
  search_tasks: Search, get_task: FileText, list_projects: FolderKanban, list_members: Users,
  create_tasks: Plus, update_task: Pencil, close_task: CheckCircle2, comment: MessageSquare,
};

function icon(s: ActivityStep) {
  if (s.error) return AlertCircle;
  if (s.type === "thought") return Lightbulb;
  if (s.type === "lookup") return Globe;
  return (s.tool && toolIcon[s.tool]) || (s.type === "write" ? Pencil : Check);
}

// ActivityTrail is what the agent did for a message, as a trace under it: folded to one line ("Looked
// up Centaur · 3 steps") that opens to every step. Live, each step shows as it comes, the latest
// shimmering.
export function ActivityTrail({ steps, live, onNavigate, className }: { steps: ActivityStep[]; live?: boolean; onNavigate?: () => void; className?: string }) {
  if (!steps.length) return null;
  const rows = <Rows steps={steps} live={live} onNavigate={onNavigate} />;
  if (live) return <Trace className={className} aria-live="polite">{rows}</Trace>;
  const s = summary(steps);
  return (
    <TraceCollapsible className={className}>
      <TraceCollapsibleTrigger>
        <TraceLine status={s.failed ? "error" : undefined}>
          <TraceIcon><Sparkles /></TraceIcon>
          <TraceTitle>{s.headline}</TraceTitle>
          <TraceDescription>{s.label}{s.failed > 0 && ` · ${s.failed} failed`}</TraceDescription>
        </TraceLine>
      </TraceCollapsibleTrigger>
      <TraceCollapsibleContent>
        <Trace>{rows}</Trace>
      </TraceCollapsibleContent>
    </TraceCollapsible>
  );
}

function Rows({ steps, live, onNavigate }: { steps: ActivityStep[]; live?: boolean; onNavigate?: () => void }) {
  return <>{steps.map((s, i) => {
    const Icon = icon(s);
    const running = live && i === steps.length - 1;
    const links = s.tasks && s.tasks.length > 0 && s.tool !== "search_tasks";
    return (
      <div key={i} className="flex min-w-0 flex-col gap-1">
        <TraceLine status={s.error ? "error" : undefined} className={cn(s.type === "thought" && "italic")} title={line(s)}>
          <TraceIcon><Icon /></TraceIcon>
          <TraceTitle running={running}>{line(s)}</TraceTitle>
        </TraceLine>
        {s.error && <p className="ml-5 text-xs break-words text-amber-700 dark:text-amber-500">{s.error}</p>}
        {links && (
          <div className="ml-5 flex flex-wrap gap-1" onClickCapture={onNavigate}>
            {s.tasks!.map((t) => (
              <TaskLink key={t.id} id={t.id} className="max-w-full truncate rounded-full border border-border bg-card px-2 py-0.5 text-xs text-foreground/80 hover:bg-accent hover:text-foreground">
                {t.title}
              </TaskLink>
            ))}
          </div>
        )}
      </div>
    );
  })}</>;
}

// LiveTrail shows the workspace's agent at work on a task, at the end of its chat, until it replies.
export function LiveTrail({ task }: { task: string }) {
  const live = useAgentActivity({ task });
  const status = useQuery({ queryKey: ["assistant"], queryFn: () => api.tool<{ enabled: boolean; agent?: MemberRef }>("get_assistant"), refetchInterval: false });
  const ref = useRef<HTMLDivElement>(null);
  const n = live.steps.length;
  useEffect(() => { if (n) ref.current?.scrollIntoView({ block: "end" }); }, [n]);
  if (!live.working) return null;
  const agent = status.data?.agent ?? { id: "agent", name: "Agent", kind: "agent" as const };
  return (
    <div ref={ref} className="mt-3 flex gap-2.5" role="status">
      <Avatar m={agent} className="agent-working mt-0.5" />
      <div className="min-w-0 flex-1">
        <div className="mb-1 text-xs text-muted-foreground">{agent.name} is working on it…</div>
        <ActivityTrail steps={live.steps} live />
      </div>
    </div>
  );
}
