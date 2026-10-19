import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bell, BookOpen, CalendarClock, CalendarDays, CircleDot, Database, FileText, Folder, FolderGit2,
  GitPullRequest, Hash, List, Mail, MessageSquare, MessagesSquare, Plug, Search, Sheet, Unplug, UserSearch, Users,
  type LucideIcon,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "../lib";
import { Card, Empty, ErrorText } from "../components/app";
import { ConfirmButton, IconTile, SettingsSection, StatusDot } from "../components/settings";
import { Button } from "@/components/ui/button";
import { SettingsHeader } from "./settings";
import { toolLabel, type ToolIcon } from "../app-tools";

const icons: Record<ToolIcon, LucideIcon> = {
  search: Search, mail: Mail, thread: MessagesSquare, contacts: UserSearch, calendar: CalendarDays, event: CalendarClock,
  free: CalendarClock, list: List, file: FileText, folder: Folder, sheet: Sheet, message: MessageSquare, channel: Hash,
  issue: CircleDot, pr: GitPullRequest, bell: Bell, repo: FolderGit2, page: BookOpen, database: Database, team: Users,
};

type App = { slug: string; name: string; logo?: string; description?: string; connected: boolean; status?: string; tools: string[] };

// AppsPage is where each person connects their own accounts (through Composio). Only their own chat
// with the assistant reads them; nobody else's turns do.
export function AppsPage() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["apps"], queryFn: () => api.tool<{ enabled: boolean; apps: App[] }>("list_apps") });
  const refresh = () => qc.invalidateQueries({ queryKey: ["apps"] });
  const connected = q.data?.apps.filter((a) => a.connected) ?? [];
  const rest = q.data?.apps.filter((a) => !a.connected) ?? [];
  // Coming back from Composio's sign-in page.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const app = params.get("app");
    if (!app) return;
    if (params.get("status") === "failed") toast.error(`${app} wasn't connected`);
    else toast(`${app} connected`);
    window.history.replaceState(null, "", window.location.pathname);
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return (
    <>
      <SettingsHeader active="apps" />
      <SettingsSection title="Your apps" description="Connect your own accounts so the assistant can use them when you ask: your email, calendar, files and messages. Only your requests use them. It reads on its own, but anything it would send, post or create waits as a card until you tap Send.">
        <ErrorText error={q.error} />
        {q.data && !q.data.enabled && (
          <Card>
            <Empty>Apps aren't set up on this server yet. An admin sets HANDOFF_COMPOSIO_API_KEY to turn them on.</Empty>
          </Card>
        )}
        {q.isPending && <div className="grid gap-3 sm:grid-cols-2">{[0, 1, 2, 3].map((i) => <Card key={i} className="h-[76px] animate-pulse bg-muted" />)}</div>}
        {q.data?.enabled && (
          <>
            {connected.length > 0 && <div className="mb-3 grid items-start gap-3 sm:grid-cols-2">{connected.map((a) => <AppCard key={a.slug} a={a} onChange={refresh} />)}</div>}
            {rest.length > 0 && (
              <>
                {connected.length > 0 && <h3 className="mt-6 mb-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">More apps</h3>}
                <div className="grid items-start gap-3 sm:grid-cols-2">{rest.map((a) => <AppCard key={a.slug} a={a} onChange={refresh} />)}</div>
              </>
            )}
          </>
        )}
      </SettingsSection>
    </>
  );
}

function AppCard({ a, onChange }: { a: App; onChange: () => void }) {
  const [leaving, setLeaving] = useState(false);
  const connect = useMutation({
    mutationFn: () => api.tool<{ redirect_url: string }>("connect_app", { app: a.slug, origin: window.location.origin }),
    onSuccess: ({ redirect_url }) => { setLeaving(true); window.location.assign(redirect_url); },
  });
  const disconnect = async () => { await api.tool("disconnect_app", { app: a.slug }); toast(`${a.name} disconnected`); onChange(); };
  const pending = a.status && !a.connected;
  return (
    <Card className="flex flex-col">
      <div className="flex items-center gap-3 p-4">
        <IconTile src={a.logo} icon={Plug} />
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">{a.name}</div>
          {a.connected ? <StatusDot tone="on">Connected</StatusDot> : pending ? <StatusDot tone="warn">Not finished</StatusDot> : <StatusDot tone="off">Not connected</StatusDot>}
        </div>
        {a.connected ? (
          <ConfirmButton icon={Unplug} label="Disconnect" title={`Disconnect ${a.name}?`} pendingLabel="Disconnecting…"
            description={<>The assistant stops reading your {a.name}, and Composio forgets the sign-in. You can connect it again any time.</>}
            onConfirm={disconnect} />
        ) : (
          <Button size="sm" variant={pending ? "outline" : "default"} disabled={connect.isPending || leaving} onClick={() => connect.mutate()}>
            {connect.isPending || leaving ? "Opening…" : pending ? "Try again" : "Connect"}
          </Button>
        )}
      </div>
      {a.description && !a.connected && <div className="-mt-1 px-4 pb-4"><p className="line-clamp-2 text-xs text-muted-foreground">{a.description}</p></div>}
      {a.connected && a.tools.length > 0 && (
        <div className="border-t border-border px-4 py-3">
          <div className="mb-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">The assistant can</div>
          <ul className="grid gap-x-4 gap-y-1.5 sm:grid-cols-2" aria-label={`What the assistant can do with ${a.name}`}>
            {a.tools.map((slug) => {
              const { text, icon } = toolLabel(slug, a.slug);
              const Icon = icons[icon];
              return (
                <li key={slug} className="flex items-center gap-2 text-xs text-foreground/80" title={slug}>
                  <Icon className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
                  {text}
                </li>
              );
            })}
          </ul>
        </div>
      )}
      {connect.error && <div className="px-4 pb-3"><ErrorText error={connect.error} /></div>}
    </Card>
  );
}
