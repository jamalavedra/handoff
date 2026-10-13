import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { AtSign, CheckCheck, ShieldQuestion, UserPlus } from "lucide-react";
import { ago, api, cn, type Notification } from "../lib";
import { Avatar, Button, Card, Empty, ErrorText, PageHeader } from "../components/ui";
import { TaskLink } from "../components/tasklink";
import { useInvalidate } from "./tasks";

export type InboxData = { notifications: Notification[]; unread: number };

export const useInbox = () => useQuery({ queryKey: ["inbox"], queryFn: () => api.tool<InboxData>("inbox", { limit: 200 }) });

const rowClass = "flex items-start gap-3 px-4 py-3 hover:bg-zinc-50";

const verb = { mention: "mentioned you in", assigned: "assigned you", approval: "needs your approval on" } as const;
const icon = { mention: AtSign, assigned: UserPlus, approval: ShieldQuestion } as const;

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
          <Empty>Nothing yet. When someone @mentions you, gives you a task, or one of your agents needs approval, it shows up here.</Empty>
        ) : (
          <ul aria-label="Notifications">
            {items.map((n) => {
              const Icon = icon[n.kind];
              const body = (<>
                    <span className="relative mt-0.5 shrink-0">
                      {n.actor && <Avatar m={n.actor} className="size-8" />}
                      <Icon className="absolute -right-1 -bottom-1 size-4 rounded-full bg-white p-0.5 text-zinc-500 shadow-sm" />
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block text-[15px] sm:text-sm">
                        <span className="font-medium">{n.actor?.name ?? "Someone"}</span> {verb[n.kind]} <span className="font-medium">{n.task_title}</span>
                      </span>
                      {n.excerpt && n.kind !== "assigned" && <span className="mt-0.5 block truncate text-sm text-zinc-500">{n.excerpt}</span>}
                      <span className="mt-0.5 block text-xs text-zinc-400">{ago(n.at)}</span>
                    </span>
                    {!n.read && <span role="img" aria-label="Unread" className="mt-2 size-2 shrink-0 rounded-full bg-accent" />}
                  </>);
              return (
                <li key={n.id} className={cn("border-b border-zinc-100 last:border-0", !n.read && "bg-accent-soft/50")}>
                  {n.kind === "approval" ? (
                    <Link to="/agents" onClick={() => readOne.mutate(n.id)} className={rowClass}>{body}</Link>
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
