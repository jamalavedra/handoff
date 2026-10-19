import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bell, BookOpen, CalendarClock, CalendarDays, Check, CircleDot, Database, FileText, Folder, FolderGit2,
  GitPullRequest, Hash, List, Mail, MessageSquare, MessagesSquare, Plug, Search, Sheet, UserSearch, Users,
  type LucideIcon,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "../lib";
import { Card, Empty, ErrorText } from "../components/app";
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
      <p className="mb-4 max-w-2xl text-sm text-foreground/70">
        Connect your own accounts so the assistant can read them when you ask it something: your email, your calendar, your files. Only your chats use them, and it only reads; it doesn't send, change or delete anything.
      </p>
      <ErrorText error={q.error} />
      {q.data && !q.data.enabled && (
        <Card>
          <Empty>Apps aren't set up on this server yet. An admin sets HANDOFF_COMPOSIO_API_KEY to turn them on.</Empty>
        </Card>
      )}
      {q.isPending && <p className="text-sm text-muted-foreground">Loading…</p>}
      {q.data?.enabled && (
        <div className="grid gap-3 sm:grid-cols-2">
          {q.data.apps.map((a) => <AppCard key={a.slug} a={a} onChange={refresh} />)}
        </div>
      )}
    </>
  );
}

function AppCard({ a, onChange }: { a: App; onChange: () => void }) {
  const [leaving, setLeaving] = useState(false);
  const connect = useMutation({
    mutationFn: () => api.tool<{ redirect_url: string }>("connect_app", { app: a.slug, origin: window.location.origin }),
    onSuccess: ({ redirect_url }) => { setLeaving(true); window.location.assign(redirect_url); },
  });
  const disconnect = useMutation({
    mutationFn: () => api.tool("disconnect_app", { app: a.slug }),
    onSuccess: () => { toast(`${a.name} disconnected`); onChange(); },
  });
  const pending = a.status && !a.connected;
  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex items-center gap-3">
        <span className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-lg bg-accent">
          {a.logo ? <img src={a.logo} alt="" className="size-6 object-contain" /> : <Plug className="size-4" />}
        </span>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">{a.name}</div>
          <div className="text-xs text-muted-foreground">
            {a.connected ? (
              <span className="inline-flex items-center gap-1 text-green-700"><Check className="size-3" />Connected</span>
            ) : pending ? `Not finished (${a.status})` : "Not connected"}
          </div>
        </div>
        {a.connected ? (
          <Button size="sm" variant="ghost" className="text-red-600" disabled={disconnect.isPending}
            onClick={() => confirm(`Disconnect ${a.name}? The assistant stops reading it.`) && disconnect.mutate()}>
            {disconnect.isPending ? "Disconnecting…" : "Disconnect"}
          </Button>
        ) : (
          <Button size="sm" disabled={connect.isPending || leaving} onClick={() => connect.mutate()}>
            {connect.isPending || leaving ? "Opening…" : pending ? "Try again" : "Connect"}
          </Button>
        )}
      </div>
      {a.description && !a.connected && <p className="line-clamp-2 text-xs text-muted-foreground">{a.description}</p>}
      {a.connected && a.tools.length > 0 && (
        <div>
          <div className="mb-1.5 text-[11px] font-medium text-muted-foreground">The assistant can</div>
          <ul className="flex flex-col gap-1" aria-label={`What the assistant can do with ${a.name}`}>
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
      <ErrorText error={connect.error || disconnect.error} />
    </Card>
  );
}
