import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Bot, Check, Copy, KeyRound, Webhook as WebhookIcon, X } from "lucide-react";
import { ago, api, cn, type Member, type Permissions, type Project, type Task, type Webhook } from "../lib";
import { toast } from "sonner";
import { Badge, Card, Empty, ErrorText, PageHeader } from "../components/app";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useMe } from "../main";
import { useInvalidate } from "./tasks";

type Approval = { id: string; agent: { name: string }; action: string; task?: Task; reason?: string; created_at: string };
type Key = { id: string; hint: string; created_at: string; last_used_at?: string; key?: string };

const ACTIONS = ["read", "create", "update", "close", "delete"];

function CopyBox({ text, label }: { text: string; label: string }) {
  const [ok, setOk] = useState(false);
  return (
    <div className="flex items-start gap-2 rounded-md border border-border bg-muted p-2">
      <pre className="min-w-0 flex-1 whitespace-pre-wrap break-all font-mono text-xs">{text}</pre>
      <button aria-label={label} className="rounded p-1 hover:bg-foreground/10" onClick={() => { navigator.clipboard.writeText(text); setOk(true); setTimeout(() => setOk(false), 1500); }}>
        {ok ? <Check className="size-4 text-green-600" /> : <Copy className="size-4" />}
      </button>
    </div>
  );
}

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
  const create = useMutation({ mutationFn: () => api.tool("create_agent", { name }), onSuccess: () => { setName(""); invalidate(); } });
  const decide = useMutation({ mutationFn: (v: { id: string; approve: boolean }) => api.tool("decide_approval", v), onSuccess: invalidate });
  const agents = members.data?.members.filter((m) => m.kind === "agent") ?? [];
  const mcp = me.data?.mcp_url ?? "";
  return (
    <>
      <PageHeader title="Agents" />
      {!!approvals.data?.approvals.length && (
        <Card className="mb-6 border-amber-200">
          <div className="border-b border-amber-100 bg-amber-50 px-4 py-2 text-sm font-medium text-amber-800">Waiting for your approval</div>
          {approvals.data.approvals.map((a) => (
            <div key={a.id} className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-2.5 text-sm last:border-0">
              <Bot className="size-4 text-violet-600" />
              <div className="min-w-48 flex-1">
                <b>{a.agent.name}</b> wants to delete <b>{a.task?.title ?? "a task"}</b>{a.reason && <span className="text-muted-foreground"> — {a.reason}</span>}
                <div className="text-xs text-muted-foreground">{ago(a.created_at)}</div>
              </div>
              <Button size="sm" onClick={() => decide.mutate({ id: a.id, approve: true })}><Check className="size-3.5" />Approve</Button>
              <Button size="sm" variant="outline" onClick={() => decide.mutate({ id: a.id, approve: false })}><X className="size-3.5" />Reject</Button>
            </div>
          ))}
        </Card>
      )}
      <Card className="mb-6 p-4">
        <h2 className="mb-1 text-sm font-semibold">Connect an agent</h2>
        <p className="mb-3 text-sm text-foreground/70">Claude, ChatGPT, Cursor and other MCP clients: add this URL as a custom connector and sign in. Each connection becomes its own agent below, owned by you.</p>
        <CopyBox text={mcp} label="Copy MCP URL" />
        <p className="mb-2 mt-4 text-sm text-foreground/70">Or paste this into any agent and it will set itself up:</p>
        <CopyBox text={onboardingPrompt(me.data?.org?.name ?? "", mcp)} label="Copy onboarding prompt" />
        <p className="mb-2 mt-4 text-sm text-foreground/70">Headless agents (scripts, CI, internal bots): create one here and give it an API key.</p>
        <form className="flex flex-col gap-2 sm:flex-row" onSubmit={(e) => { e.preventDefault(); create.mutate(); }}>
          <Input required placeholder="Agent name, e.g. Support triage bot" value={name} onChange={(e) => setName(e.target.value)} />
          <Button type="submit">Add agent</Button>
        </form>
        <ErrorText error={create.error} />
      </Card>
      <div className="flex flex-col gap-3">
        {agents.length === 0 ? <Card><Empty>No agents yet.</Empty></Card> : agents.map((a) => <AgentCard key={a.id} a={a} mine={a.owner?.id === me.data?.member?.id || me.data?.member?.role === "admin"} />)}
      </div>
    </>
  );
}

function AgentCard({ a, mine }: { a: Member; mine: boolean }) {
  const invalidate = useInvalidate();
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const keys = useQuery({ queryKey: ["keys", a.id], queryFn: () => api.get<{ keys: Key[] }>(`/api/agents/${a.id}/keys`), enabled: mine });
  const [newKey, setNewKey] = useState<string | null>(null);
  const perms = a.permissions!;
  const save = useMutation({ mutationFn: (p: Permissions) => api.tool("update_agent", { id: a.id, permissions: p }), onSuccess: invalidate });
  const revoke = useMutation({ mutationFn: () => api.tool("update_agent", { id: a.id, revoke: true }), onSuccess: invalidate });
  const mkKey = useMutation({ mutationFn: () => api.post<Key>(`/api/agents/${a.id}/keys`), onSuccess: (k) => { setNewKey(k.key!); invalidate(); } });
  const rmKey = useMutation({ mutationFn: (k: string) => api.del(`/api/agents/${a.id}/keys/${k}`), onSuccess: invalidate });
  const toggle = (list: "actions" | "approval", v: string) => {
    const cur = perms[list];
    save.mutate({ ...perms, [list]: cur.includes(v) ? cur.filter((x) => x !== v) : [...cur, v] });
  };
  const allProjects = perms.projects.includes("*");
  return (
    <Card className="p-4">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <Bot className="size-4 text-violet-600" />
        <span className="font-medium">{a.name}</span>
        <span className="text-sm text-muted-foreground">owned by {a.owner?.name} · added {ago(a.created_at)}</span>
        {mine && <Button size="sm" variant="ghost" className="ml-auto text-red-600" onClick={() => confirm(`Disconnect ${a.name}? Its keys and logins stop working immediately.`) && revoke.mutate()}>Disconnect</Button>}
      </div>
      {mine && (
        <div className="mt-3 grid gap-4 text-sm md:grid-cols-3">
          <div>
            <div className="mb-1 text-[13px] font-medium text-muted-foreground">Can</div>
            {ACTIONS.map((x) => (
              <label key={x} className="flex items-center gap-1.5"><input type="checkbox" checked={perms.actions.includes(x)} onChange={() => toggle("actions", x)} />{x}</label>
            ))}
          </div>
          <div>
            <div className="mb-1 text-[13px] font-medium text-muted-foreground">Needs approval to</div>
            {["delete"].map((x) => (
              <label key={x} className="flex items-center gap-1.5"><input type="checkbox" checked={perms.approval.includes(x)} onChange={() => toggle("approval", x)} />{x}</label>
            ))}
            <div className="mb-1 mt-3 text-[13px] font-medium text-muted-foreground">Projects</div>
            <label className="flex items-center gap-1.5"><input type="checkbox" checked={allProjects} onChange={() => save.mutate({ ...perms, projects: allProjects ? [] : ["*"] })} />All I can see</label>
            {!allProjects && projects.data?.projects.map((p) => (
              <label key={p.id} className="flex items-center gap-1.5">
                <input type="checkbox" checked={perms.projects.includes(p.id)} onChange={() => save.mutate({ ...perms, projects: perms.projects.includes(p.id) ? perms.projects.filter((x) => x !== p.id) : [...perms.projects, p.id] })} />{p.name}
              </label>
            ))}
          </div>
          <div>
            <div className="mb-1 text-[13px] font-medium text-muted-foreground">API keys</div>
            {keys.data?.keys.map((k) => (
              <div key={k.id} className="flex items-center justify-between gap-2">
                <span className="font-mono text-xs">{k.hint}</span>
                <span className="text-xs text-muted-foreground/70">{k.last_used_at ? `used ${ago(k.last_used_at)}` : "never used"}</span>
                <button className="text-xs text-muted-foreground/70 hover:text-red-600" onClick={() => rmKey.mutate(k.id)}>revoke</button>
              </div>
            ))}
            {newKey && <div className="my-2"><p className="mb-1 text-xs text-amber-700">Copy it now; it won't be shown again.</p><CopyBox text={newKey} label="Copy API key" /></div>}
            <Button size="sm" variant="outline" className="mt-1" onClick={() => mkKey.mutate()}><KeyRound className="size-3.5" />New key</Button>
          </div>
        </div>
      )}
      {mine && <WebhookSection a={a} />}
      {!mine && <div className="mt-2 flex flex-wrap gap-1">{perms.actions.map((x) => <Badge key={x}>{x}</Badge>)}</div>}
      <ErrorText error={save.error || revoke.error || mkKey.error || rmKey.error} />
    </Card>
  );
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
  return (
    <div className="mt-4 border-t border-border pt-3 text-sm">
      <div className="mb-1 flex items-center gap-1.5 text-[13px] font-medium text-muted-foreground"><WebhookIcon className="size-3.5" />Webhook</div>
      <p className="mb-2 max-w-xl text-xs text-muted-foreground">
        For agents that aren't always connected (a Slack bot, a cloud function): Handoff POSTs to this URL when a task is assigned to {a.name}, it's @mentioned, or one of its tasks comes due — so a recurring task assigned to it runs on a schedule. Verify <code>X-Handoff-Signature: t=…,v1=…</code> as HMAC-SHA256 of <code>t.body</code> with the secret.
      </p>
      <form className="flex max-w-xl gap-2" onSubmit={(e) => { e.preventDefault(); set.mutate({ url }); }}>
        <Input type="url" placeholder="https://example.com/handoff-webhook" value={url} onChange={(e) => setUrl(e.target.value)} aria-label={`Webhook URL for ${a.name}`} />
        <Button type="submit" size="sm" variant="outline" disabled={set.isPending || url === (w?.url ?? "")}>Save</Button>
      </form>
      {secret && <div className="my-2 max-w-xl"><p className="mb-1 text-xs text-amber-700">Signing secret. Copy it now; it won't be shown again.</p><CopyBox text={secret} label="Copy webhook secret" /></div>}
      {w && (
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <span className={cn("text-xs", w.last_error ? "text-red-600" : "text-muted-foreground")}>
            {w.last_at ? (w.last_error ? `Last delivery failed ${ago(w.last_at)}: ${w.last_error}` : `Last delivery ${w.last_status} · ${ago(w.last_at)}`) : "Nothing sent yet"}
          </span>
          <Button size="sm" variant="ghost" onClick={() => test.mutate()} disabled={test.isPending}>Send test</Button>
          <Button size="sm" variant="ghost" onClick={() => set.mutate({ url: w.url, rotate: true })}>Rotate secret</Button>
          <Button size="sm" variant="ghost" className="text-red-600" onClick={() => { setUrl(""); setSecret(null); set.mutate({ url: "" }); }}>Remove</Button>
        </div>
      )}
      <ErrorText error={set.error || test.error} />
    </div>
  );
}
