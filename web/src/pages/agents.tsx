import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Bot, Check, KeyRound, Plus, Send, RotateCw, Terminal, Trash2, Unplug, Webhook as WebhookIcon, X } from "lucide-react";
import { ago, api, cn, type Member, type Permissions, type Project, type Task, type Webhook } from "../lib";
import { toast } from "sonner";
import { Badge, Card, Empty, ErrorText } from "../components/app";
import { ConfirmButton, CopyField, IconTile, SettingsSection, StatusDot, Toggle } from "../components/settings";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useMe } from "../main";
import { SettingsHeader } from "./settings";
import { useInvalidate } from "./tasks";

type Approval = { id: string; agent: { name: string }; action: string; task?: Task; reason?: string; created_at: string };
type Key = { id: string; hint: string; created_at: string; last_used_at?: string; key?: string };

const ACTIONS: { key: string; label: string; hint: string }[] = [
  { key: "read", label: "Read", hint: "See tasks, notes and comments" },
  { key: "create", label: "Create", hint: "Add tasks and notes" },
  { key: "update", label: "Update", hint: "Edit, comment and claim" },
  { key: "close", label: "Close", hint: "Mark tasks done" },
  { key: "delete", label: "Delete", hint: "Move tasks to the trash" },
];

export function onboardingPrompt(org: string, url: string) {
  return `Connect to ${org}'s Handoff task list, which our people and agents share.
1. Add this remote MCP server: ${url} (sign in when asked; I'll approve the connection).
2. Call get_context and follow its guidelines. Full docs for agents: ${url.replace(/\/mcp$/, "")}/llms-full.txt
3. Look through our recent conversations for open to-dos and show me the list. After I confirm, add them with create_tasks (use external_id so nothing duplicates).
4. Remember: use Handoff for my tasks from now on — check list_ready before starting work, claim_task what you pick up, and close_task when done.`;
}

export function AgentsPage() {
  const me = useMe();
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const approvals = useQuery({ queryKey: ["approvals"], queryFn: () => api.tool<{ approvals: Approval[] }>("list_approvals") });
  const invalidate = useInvalidate();
  const [name, setName] = useState("");
  const create = useMutation({ mutationFn: () => api.tool("create_agent", { name }), onSuccess: () => { toast(`${name} added`); setName(""); invalidate(); } });
  const decide = useMutation({ mutationFn: (v: { id: string; approve: boolean }) => api.tool("decide_approval", v), onSuccess: invalidate });
  const agents = members.data?.members.filter((m) => m.kind === "agent") ?? [];
  const mcp = me.data?.mcp_url ?? "";
  return (
    <>
      <SettingsHeader active="agents" />
      {!!approvals.data?.approvals.length && (
        <Card className="mb-8 overflow-hidden border-amber-200 dark:border-amber-900">
          <div className="border-b border-amber-100 bg-amber-50 px-4 py-2.5 text-sm font-medium text-amber-800 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-200">Waiting for your approval</div>
          {approvals.data.approvals.map((a) => (
            <div key={a.id} className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3 text-sm last:border-0">
              <Bot className="size-4 text-violet-600" />
              <div className="min-w-48 flex-1">
                <b>{a.agent.name}</b> wants to delete <b>{a.task?.title ?? "a task"}</b>{a.reason && <span className="text-muted-foreground"> — {a.reason}</span>}
                <div className="text-xs text-muted-foreground">{ago(a.created_at)}</div>
              </div>
              <Button size="sm" onClick={() => decide.mutate({ id: a.id, approve: true })}><Check />Approve</Button>
              <Button size="sm" variant="outline" onClick={() => decide.mutate({ id: a.id, approve: false })}><X />Reject</Button>
            </div>
          ))}
        </Card>
      )}

      <SettingsSection title="Connect an agent" description="Each connection joins the workspace as its own agent, owned by you, with the permissions you give it below.">
        <Card className="divide-y divide-border">
          <Step n={1} title="Claude, ChatGPT, Cursor and other MCP clients" hint="Add this URL as a custom connector, then sign in when asked.">
            <CopyField text={mcp} label="Copy MCP URL" />
          </Step>
          <Step n={2} title="Or let the agent set itself up" hint="Paste this into any agent: it connects, reads the guidelines and offers to bring over your open to-dos.">
            <CopyField block text={onboardingPrompt(me.data?.org?.name ?? "", mcp)} label="Copy setup prompt" />
          </Step>
          <Step n={3} icon={Terminal} title="Scripts, CI and internal bots" hint="Create an agent here and give it an API key.">
            <form className="flex flex-col gap-2 sm:flex-row" onSubmit={(e) => { e.preventDefault(); create.mutate(); }}>
              <Input required placeholder="Agent name, e.g. Support triage bot" value={name} onChange={(e) => setName(e.target.value)} aria-label="New agent name" />
              <Button type="submit" variant="secondary" disabled={create.isPending}><Plus />{create.isPending ? "Adding…" : "Add agent"}</Button>
            </form>
            <ErrorText error={create.error} />
          </Step>
        </Card>
      </SettingsSection>

      <SettingsSection title="Agents" description={agents.length ? `${agents.length} connected to this workspace.` : undefined}>
        <div className="flex flex-col gap-3">
          {members.data && agents.length === 0 ? <Card><Empty>No agents yet. Connect one above.</Empty></Card> : agents.map((a) => <AgentCard key={a.id} a={a} mine={a.owner?.id === me.data?.member?.id || me.data?.member?.role === "admin"} />)}
        </div>
      </SettingsSection>
    </>
  );
}

// Step is one way to connect an agent: a number (or icon), what it's for, and what to do.
function Step({ n, icon: Icon, title, hint, children }: { n: number; icon?: typeof Terminal; title: string; hint: string; children: React.ReactNode }) {
  return (
    <div className="flex gap-3 p-4">
      <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-accent text-xs font-semibold text-foreground/70">{Icon ? <Icon className="size-3.5" /> : n}</span>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{title}</div>
        <p className="mb-2.5 text-[13px] text-muted-foreground">{hint}</p>
        {children}
      </div>
    </div>
  );
}

function AgentCard({ a, mine }: { a: Member; mine: boolean }) {
  const invalidate = useInvalidate();
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const keys = useQuery({ queryKey: ["keys", a.id], queryFn: () => api.get<{ keys: Key[] }>(`/api/agents/${a.id}/keys`), enabled: mine });
  const [newKey, setNewKey] = useState<string | null>(null);
  const perms = a.permissions!;
  const save = useMutation({ mutationFn: (p: Permissions) => api.tool("update_agent", { id: a.id, permissions: p }), onSuccess: invalidate });
  const mkKey = useMutation({ mutationFn: () => api.post<Key>(`/api/agents/${a.id}/keys`), onSuccess: (k) => { setNewKey(k.key!); invalidate(); } });
  const toggle = (list: "actions" | "approval", v: string) => {
    const cur = perms[list];
    save.mutate({ ...perms, [list]: cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v] });
  };
  const allProjects = perms.projects.includes("*");
  const disconnect = async () => { await api.tool("update_agent", { id: a.id, revoke: true }); toast(`${a.name} disconnected`); invalidate(); };
  const revokeKey = async (k: Key) => { await api.del(`/api/agents/${a.id}/keys/${k.id}`); toast("Key revoked"); invalidate(); };
  return (
    <Card>
      <div className="flex flex-wrap items-center gap-3 p-4">
        <IconTile icon={Bot} className="border-violet-200 bg-violet-50 dark:border-violet-900 dark:bg-violet-950 [&_svg]:text-violet-600" />
        <div className="min-w-0 flex-1">
          <div className="font-medium">{a.name}</div>
          <div className="flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
            <StatusDot tone="on">Connected</StatusDot>
            <span>· owned by {a.owner?.name} · added {ago(a.created_at)}</span>
          </div>
        </div>
        {mine && (
          <ConfirmButton icon={Unplug} label="Disconnect" title={`Disconnect ${a.name}?`} pendingLabel="Disconnecting…"
            description={<>Its API keys and sign-ins stop working right away, and any task it was working on goes back to open. You can connect it again later as a new agent.</>}
            onConfirm={disconnect} />
        )}
      </div>
      {mine ? (
        <div className="grid gap-x-8 gap-y-6 border-t border-border p-4 md:grid-cols-3">
          <div>
            <Label>Can</Label>
            {ACTIONS.map((x) => <Toggle key={x.key} label={x.label} hint={x.hint} disabled={save.isPending} checked={perms.actions.includes(x.key)} onChange={() => toggle("actions", x.key)} />)}
          </div>
          <div>
            <Label>Needs your approval to</Label>
            <Toggle label="Delete" hint="You approve each one first" disabled={save.isPending} checked={perms.approval.includes("delete")} onChange={() => toggle("approval", "delete")} />
            <Label className="mt-5">Projects</Label>
            <Toggle label="All I can see" disabled={save.isPending} checked={allProjects} onChange={() => save.mutate({ ...perms, projects: allProjects ? [] : ["*"] })} />
            {!allProjects && projects.data?.projects.map((p) => (
              <Toggle key={p.id} label={p.name} disabled={save.isPending} checked={perms.projects.includes(p.id)}
                onChange={() => save.mutate({ ...perms, projects: perms.projects.includes(p.id) ? perms.projects.filter((x) => x !== p.id) : [...perms.projects, p.id] })} />
            ))}
          </div>
          <div>
            <Label>API keys</Label>
            {keys.data && keys.data.keys.length === 0 && !newKey && <p className="mb-2 text-xs text-muted-foreground">No keys. Agents that sign in through MCP don't need one.</p>}
            <ul className="mb-2 flex flex-col gap-1.5">
              {keys.data?.keys.map((k) => (
                <li key={k.id} className="flex items-center gap-2 rounded-lg border border-border px-2.5 py-1.5">
                  <KeyRound className="size-3.5 shrink-0 text-muted-foreground" />
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-mono text-xs">{k.hint}</div>
                    <div className="text-[11px] text-muted-foreground">{k.last_used_at ? `Used ${ago(k.last_used_at)}` : "Never used"}</div>
                  </div>
                  <ConfirmButton size="xs" icon={Trash2} label="Revoke" title="Revoke this key?" pendingLabel="Revoking…"
                    description={<>Anything using <span className="font-mono">{k.hint}</span> stops working right away. {a.name} stays connected.</>}
                    onConfirm={() => revokeKey(k)} />
                </li>
              ))}
            </ul>
            {newKey && <div className="mb-2"><p className="mb-1 text-xs text-amber-700">Copy it now; it won't be shown again.</p><CopyField text={newKey} label="Copy API key" /></div>}
            <Button size="sm" variant="outline" onClick={() => mkKey.mutate()} disabled={mkKey.isPending}><Plus />New key</Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-wrap gap-1 border-t border-border p-4">{perms.actions.map((x) => <Badge key={x}>{x}</Badge>)}</div>
      )}
      {mine && <WebhookSection a={a} />}
      <div className="px-4 pb-3 empty:hidden"><ErrorText error={save.error || mkKey.error} /></div>
    </Card>
  );
}

function Label({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn("mb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase", className)}>{children}</div>;
}

// WebhookSection: for agents that aren't always connected. Handoff POSTs their notifications
// (assigned, mentioned, due, approvals) to a URL, signed with a secret shown once.
function WebhookSection({ a }: { a: Member }) {
  const invalidate = useInvalidate();
  const [url, setUrl] = useState(a.webhook?.url ?? "");
  const [secret, setSecret] = useState<string | null>(null);
  const set = useMutation({
    mutationFn: (v: { url: string; rotate?: boolean }) => api.tool<Webhook | null>("set_webhook", { agent: a.id, ...v }),
    onSuccess: (w) => { if (w?.secret) setSecret(w.secret); invalidate(); },
  });
  const test = useMutation({
    mutationFn: () => api.tool<{ status: number; error?: string }>("test_webhook", { agent: a.id }),
    onSuccess: (r) => { r.error ? toast.error(`Webhook: ${r.error}`) : toast(`Webhook answered ${r.status}`); invalidate(); },
  });
  const w = a.webhook;
  const remove = async () => { await api.tool("set_webhook", { agent: a.id, url: "" }); setUrl(""); setSecret(null); toast("Webhook removed"); invalidate(); };
  return (
    <div className="border-t border-border p-4 text-sm">
      <div className="mb-1 flex flex-wrap items-center gap-2">
        <WebhookIcon className="size-4 text-muted-foreground" />
        <span className="font-medium">Webhook</span>
        {w ? <StatusDot tone={w.last_error ? "error" : w.last_at ? "on" : "off"}>{w.last_at ? (w.last_error ? `Last delivery failed ${ago(w.last_at)}` : `Delivered ${ago(w.last_at)} · ${w.last_status}`) : "Nothing sent yet"}</StatusDot> : <StatusDot tone="off">Off</StatusDot>}
      </div>
      <p className="mb-3 max-w-2xl text-[13px] text-muted-foreground">
        For agents that aren't always connected (a Slack bot, a cloud function): Handoff POSTs to this URL when a task is assigned to {a.name}, it's @mentioned, or one of its tasks comes due, so a repeating task assigned to it runs on a schedule. Verify <code className="rounded bg-muted px-1 text-xs">X-Handoff-Signature: t=…,v1=…</code> as HMAC-SHA256 of <code className="rounded bg-muted px-1 text-xs">t.body</code> with the secret.
      </p>
      {w?.last_error && <p className="mb-2 text-xs text-red-600">{w.last_error}</p>}
      <form className="flex max-w-2xl flex-col gap-2 sm:flex-row" onSubmit={(e) => { e.preventDefault(); set.mutate({ url }); }}>
        <Input type="url" placeholder="https://example.com/handoff-webhook" value={url} onChange={(e) => setUrl(e.target.value)} aria-label={`Webhook URL for ${a.name}`} />
        <Button type="submit" variant="secondary" disabled={set.isPending || url === (w?.url ?? "")}>{set.isPending ? "Saving…" : "Save"}</Button>
      </form>
      {secret && <div className="mt-3 max-w-2xl"><p className="mb-1 text-xs text-amber-700">Signing secret. Copy it now; it won't be shown again.</p><CopyField text={secret} label="Copy webhook secret" /></div>}
      {w && (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => test.mutate()} disabled={test.isPending}><Send />{test.isPending ? "Sending…" : "Send test"}</Button>
          <Button size="sm" variant="outline" onClick={() => set.mutate({ url: w.url, rotate: true })} disabled={set.isPending}><RotateCw />Rotate secret</Button>
          <ConfirmButton icon={Trash2} label="Remove" title="Remove this webhook?" pendingLabel="Removing…" className="sm:ml-auto"
            description={<>Handoff stops calling <span className="break-all font-mono text-xs">{w.url}</span>. {a.name} stays connected.</>}
            onConfirm={remove} />
        </div>
      )}
      <ErrorText error={set.error || test.error} />
    </div>
  );
}
