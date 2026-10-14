import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { AtSign, CheckCheck, ShieldQuestion, Sparkles, UserPlus } from "lucide-react";
import { ago, api, cn, type Notification } from "../lib";
import { Avatar, Card, Empty, ErrorText, PageHeader } from "../components/app";
import { Button } from "@/components/ui/button";
import { TaskLink } from "../components/tasklink";
import { useInvalidate } from "./tasks";

export type InboxData = { notifications: Notification[]; unread: number };

export const useInbox = () => useQuery({ queryKey: ["inbox"], queryFn: () => api.tool<InboxData>("inbox", { limit: 200 }) });

const rowClass = "flex items-start gap-3 px-4 py-3 hover:bg-muted";

const verb = { mention: "mentioned you in", assigned: "assigned you", approval: "needs your approval on", agent_done: "finished" } as const;
const icon = { mention: AtSign, assigned: UserPlus, approval: ShieldQuestion, agent_done: Sparkles } as const;

export function NotificationsPage() {
  const q = useInbox();
  const invalidate = useInvalidate();
  const readAll = useMutation({ mutationFn: () => api.tool("mark_read", { all: true }), onSuccess: invalidate });
  const readOne = useMutation({ mutationFn: (id: number) => api.tool("mark_read", { ids: [id] }), onSuccess: invalidate });
  const items = q.data?.notifications;
  return (
    <>
      <PageHeader title="Notifications">
        {!!q.data?.unread && (
          <Button variant="outline" size="sm" onClick={() => readAll.mutate()} disabled={readAll.isPending}>
            <CheckCheck className="size-4" />Mark all read
          </Button>
        )}
      </PageHeader>
      <ErrorText error={q.error || readAll.error} />
      <Card className="overflow-hidden">
        {!items ? (
          <Empty>Loading…</Empty>
        ) : items.length === 0 ? (
          <Empty>Nothing yet. When someone @mentions you, gives you a task, one of your agents needs approval, or the agent finishes something you asked for, it shows up here.</Empty>
        ) : (
          <ul aria-label="Notifications">
            {items.map((n) => {
              const Icon = icon[n.kind];
              const body = (<>
                    <span className="relative mt-0.5 shrink-0">
                      {n.actor && <Avatar m={n.actor} className="size-8" />}
                      <Icon className="absolute -right-1 -bottom-1 size-4 rounded-full bg-card p-0.5 text-muted-foreground shadow-sm" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block text-[15px] sm:text-sm">
                        <span className="font-medium">{n.actor?.name ?? "Someone"}</span> {verb[n.kind]} <span className="font-medium">{n.task_title}</span>
                      </span>
                      {n.excerpt && n.kind !== "assigned" && <span className="mt-0.5 block truncate text-sm text-muted-foreground">{n.excerpt}</span>}
                      <span className="mt-0.5 block text-xs text-muted-foreground/70">{ago(n.at)}</span>
                    </span>
                    {!n.read && <span role="img" aria-label="Unread" className="mt-2 size-2 shrink-0 rounded-full bg-primary" />}
                  </>);
              return (
                <li key={n.id} className={cn("border-b border-border last:border-0", !n.read && "bg-accent/50")}>
                  {n.kind === "approval" ? (
                    <Link to="/settings/agents" onClick={() => readOne.mutate(n.id)} className={rowClass}>{body}</Link>
                  ) : (
                    <TaskLink id={n.task_id} className={rowClass}>{body}</TaskLink>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </Card>
    </>
  );
}
