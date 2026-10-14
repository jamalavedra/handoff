import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, Check, CheckCircle2, ChevronRight, FileText, FolderKanban, Globe, Lightbulb, MessageSquare, Pencil, Plus, Search, Users } from "lucide-react";
import { api, cn, type MemberRef } from "../lib";
import { line, summary, wrote, type ActivityStep } from "../activity";
import { useAgentActivity } from "../agent-activity";
import { TaskLink } from "./tasklink";
import { Avatar } from "./app";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";

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

// ActivityTrail is what the agent did for a message, one line per step, newest last. Live, it shows
// every step as it comes, the latest shimmering; done, it folds into "N steps" (open when it changed
// something, so new tasks are one click away).
export function ActivityTrail({ steps, live, onNavigate, className }: { steps: ActivityStep[]; live?: boolean; onNavigate?: () => void; className?: string }) {
  const [open, setOpen] = useState(() => wrote(steps));
  if (!steps.length) return null;
  const list = <Steps steps={steps} live={live} onNavigate={onNavigate} />;
  if (live) return <div className={cn("flex flex-col gap-1", className)} aria-live="polite">{list}</div>;
  const s = summary(steps);
  return (
    <Collapsible open={open} onOpenChange={setOpen} className={cn("mb-1.5", className)}>
      <CollapsibleTrigger className="flex items-center gap-1 rounded-full py-0.5 pr-2 pl-1 text-xs text-muted-foreground hover:bg-accent hover:text-foreground">
        <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        {s.label}
        {s.failed > 0 && <span className="text-amber-600">· {s.failed} failed</span>}
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-1 flex flex-col gap-1">{list}</CollapsibleContent>
    </Collapsible>
  );
}

function Steps({ steps, live, onNavigate }: { steps: ActivityStep[]; live?: boolean; onNavigate?: () => void }) {
  return <>{steps.map((s, i) => {
    const Icon = icon(s);
    const latest = live && i === steps.length - 1;
    const thought = s.type === "thought";
    return (
      <div key={i} className={cn("rounded-lg px-2 py-1.5 text-xs", thought ? "bg-transparent" : "bg-muted")}>
        <div className={cn("flex items-start gap-1.5", s.error ? "text-amber-700" : thought ? "text-muted-foreground italic" : "text-foreground/70")}>
          <Icon className="mt-px size-3.5 shrink-0" />
          <span className={cn("min-w-0 break-words", latest && "agent-shimmer")}>
            {line(s)}{s.error && <span className="text-muted-foreground"> · {s.error}</span>}
          </span>
        </div>
        {s.tasks && s.tasks.length > 0 && s.tool !== "search_tasks" && (
          <div className="mt-1 ml-5 flex flex-wrap gap-1" onClickCapture={onNavigate}>
            {s.tasks.map((t) => (
              <TaskLink key={t.id} id={t.id} className="max-w-full truncate rounded-full border border-border bg-card px-2 py-0.5 text-foreground/80 hover:bg-accent hover:text-foreground">
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
