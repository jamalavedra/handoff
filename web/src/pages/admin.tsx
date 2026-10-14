import { useEffect, useState } from "react";
import { Sparkles } from "lucide-react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, type Member } from "../lib";
import { Badge, Card, ErrorText, PageHeader } from "../components/app";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { useMe } from "../main";
import { useInvalidate } from "./tasks";

export function AdminPage() {
  const me = useMe();
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const invalidate = useInvalidate();
  const [inv, setInv] = useState({ email: "", role: "member" });
  const [org, setOrg] = useState({ name: "", custom_domain: "", allowed_email_domain: "" });
  useEffect(() => {
    const o = me.data?.org;
    if (o) setOrg({ name: o.name, custom_domain: o.custom_domain ?? "", allowed_email_domain: o.allowed_email_domain ?? "" });
  }, [me.data]);
  const invite = useMutation({ mutationFn: () => api.tool("invite", inv), onSuccess: () => { setInv({ email: "", role: "member" }); invalidate(); } });
  const change = useMutation({ mutationFn: (v: { id: string; role?: string; remove?: boolean }) => api.tool("update_member", v), onSuccess: invalidate });
  const saveOrg = useMutation({ mutationFn: () => api.tool("update_org", org), onSuccess: invalidate });
  if (me.data?.member?.role !== "admin") return <p className="text-sm">Only admins can see this page.</p>;
  const people = members.data?.members.filter((m) => m.kind === "human") ?? [];
  return (
    <>
      <PageHeader title="Admin" />
      <h2 className="mb-2 text-sm font-medium text-muted-foreground">People</h2>
      <Card className="mb-3 p-3">
        <form className="flex flex-col gap-2 sm:flex-row" onSubmit={(e) => { e.preventDefault(); invite.mutate(); }}>
          <Input required type="email" placeholder="colleague@company.com" value={inv.email} onChange={(e) => setInv({ ...inv, email: e.target.value })} />
          <NativeSelect value={inv.role} onChange={(e) => setInv({ ...inv, role: e.target.value })} aria-label="Role"><option value="member">Member</option><option value="admin">Admin</option></NativeSelect>
          <Button type="submit">Invite</Button>
        </form>
        <p className="mt-2 text-xs text-muted-foreground">Invited people sign in at {me.data?.base_url} with their email.</p>
        <ErrorText error={invite.error} />
      </Card>
      <Card className="mb-8">
        {people.map((m) => (
          <div key={m.id} className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-2.5 text-sm last:border-0">
            <div className="min-w-40 flex-1"><div className="font-medium">{m.name}</div><div className="text-xs text-muted-foreground">{m.email}</div></div>
            {m.status === "invited" && <Badge>invited</Badge>}
            <NativeSelect value={m.role} onChange={(e) => change.mutate({ id: m.id, role: e.target.value })} aria-label={`Role for ${m.name}`}><option value="member">Member</option><option value="admin">Admin</option></NativeSelect>
            {m.id !== me.data?.member?.id && <Button size="sm" variant="ghost" className="text-red-600" onClick={() => confirm(`Remove ${m.name}? Their agents are disconnected too.`) && change.mutate({ id: m.id, remove: true })}>Remove</Button>}
          </div>
        ))}
        <div className="px-4"><ErrorText error={change.error} /></div>
      </Card>
      <TriageSettings agents={members.data?.members.filter((m) => m.kind === "agent" && m.status === "active") ?? []} />
      <h2 className="mb-2 text-sm font-medium text-muted-foreground">Workspace</h2>
      <Card className="p-4">
        <form className="flex max-w-md flex-col gap-3 text-sm" onSubmit={(e) => { e.preventDefault(); saveOrg.mutate(); }}>
          <label className="font-medium">Name<Input value={org.name} onChange={(e) => setOrg({ ...org, name: e.target.value })} /></label>
          <label className="font-medium">Custom domain<Input placeholder="tasks.acme.com" value={org.custom_domain} onChange={(e) => setOrg({ ...org, custom_domain: e.target.value })} />
            <span className="text-xs font-normal text-muted-foreground">Point a CNAME at {me.data?.org?.slug}.{new URL(me.data?.base_url ?? "http://x").host.split(".").slice(1).join(".")}; the certificate is issued automatically.</span>
          </label>
          <label className="font-medium">Anyone with this email domain can join<Input placeholder="acme.com" value={org.allowed_email_domain} onChange={(e) => setOrg({ ...org, allowed_email_domain: e.target.value })} /></label>
          <Button className="self-start" type="submit">Save</Button>
          {saveOrg.isSuccess && <span className="text-xs text-green-700">Saved.</span>}
          <ErrorText error={saveOrg.error} />
        </form>
        <div className="mt-6 border-t border-border pt-4 text-sm">
          <div className="mb-1 font-medium">Beads</div>
          <p className="mb-2 text-foreground/70">Export every task as a beads <code>issues.jsonl</code>, or import one with <code>handoff bd import</code> / <code>POST /api/beads/import</code>.</p>
          <a className="underline" href="/api/beads/export">Download issues.jsonl</a>
        </div>
      </Card>
    </>
  );
}

type Triage = { enabled: boolean; agent?: { id: string; name: string }; base_url?: string; model?: string; has_credential: boolean };

// Any OpenAI-compatible endpoint works; these are shortcuts for the common ones.
const providers = [
  { name: "Anthropic", url: "https://api.anthropic.com/v1", model: "claude-haiku-5-5" },
  { name: "OpenAI", url: "https://api.openai.com/v1", model: "" },
  { name: "OpenRouter", url: "https://openrouter.ai/api/v1", model: "" },
  { name: "Vercel AI Gateway", url: "https://ai-gateway.vercel.sh/v1", model: "" },
];

function TriageSettings({ agents }: { agents: Member[] }) {
  const invalidate = useInvalidate();
  const q = useQuery({ queryKey: ["triage"], queryFn: () => api.tool<Triage>("get_triage") });
  const [f, setF] = useState({ agent: "", base_url: providers[0].url, model: providers[0].model, credential: "" });
  useEffect(() => {
    const t = q.data;
    if (t?.enabled) setF({ agent: t.agent?.id ?? "", base_url: t.base_url ?? "", model: t.model ?? "", credential: "" });
  }, [q.data]);
  const save = useMutation({ mutationFn: () => api.tool<Triage>("set_triage", f), onSuccess: () => { setF((x) => ({ ...x, credential: "" })); invalidate(); } });
  const off = useMutation({ mutationFn: () => api.tool("disable_triage"), onSuccess: invalidate });
  const preset = providers.find((p) => p.url === f.base_url)?.name ?? "custom";
  const t = q.data;
  return (
    <>
      <h2 className="mb-2 flex items-center gap-1.5 text-sm font-medium text-muted-foreground"><Sparkles className="size-3.5" />Triage</h2>
      <Card className="mb-8 p-4">
        <p className="mb-3 max-w-xl text-sm text-foreground/70">
          When someone creates a task, this agent reads it, tidies the title, fills in the due date, priority, project and labels the text implies, suggests an owner if nobody was picked, and adds a short plan as a comment. Use the same agent your team already talks to. Without one, new tasks are left as written.
        </p>
        {t?.enabled && <p className="mb-3 text-sm"><span className="font-medium">On</span> · {t.agent?.name} · {t.model}</p>}
        {agents.length === 0 ? (
          <p className="text-sm text-muted-foreground">Connect or add an agent on the Agents page first.</p>
        ) : (
          <form className="flex max-w-md flex-col gap-3 text-sm" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
            <label className="font-medium">Agent
              <NativeSelect required value={f.agent} onChange={(e) => setF({ ...f, agent: e.target.value })} aria-label="Triage agent">
                <option value="" disabled>Choose an agent</option>
                {agents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
              </NativeSelect>
            </label>
            <label className="font-medium">Model provider
              <NativeSelect value={preset} onChange={(e) => { const p = providers.find((x) => x.name === e.target.value); setF({ ...f, base_url: p?.url ?? "", model: p?.model || f.model }); }} aria-label="Model provider">
                {providers.map((p) => <option key={p.name} value={p.name}>{p.name}</option>)}
                <option value="custom">Other (OpenAI-compatible)</option>
              </NativeSelect>
            </label>
            {preset === "custom" && <label className="font-medium">Base URL<Input required placeholder="https://…/v1" value={f.base_url} onChange={(e) => setF({ ...f, base_url: e.target.value })} /></label>}
            <label className="font-medium">Model<Input required placeholder="model id as the provider names it" value={f.model} onChange={(e) => setF({ ...f, model: e.target.value })} /></label>
            <label className="font-medium">API key or OAuth token
              <Input type="password" autoComplete="off" required={!t?.has_credential} placeholder={t?.has_credential ? "Saved · leave empty to keep it" : "sk-…"} value={f.credential} onChange={(e) => setF({ ...f, credential: e.target.value })} />
              <span className="text-xs font-normal text-muted-foreground">Sent as a bearer token. Stored encrypted and never shown again.</span>
            </label>
            <div className="flex items-center gap-2">
              <Button type="submit" disabled={save.isPending}>{t?.enabled ? "Save" : "Turn on triage"}</Button>
              {t?.enabled && <Button type="button" variant="ghost" onClick={() => off.mutate()} disabled={off.isPending}>Turn off</Button>}
              {save.isSuccess && <span className="text-xs text-green-700">Saved.</span>}
            </div>
            <ErrorText error={save.error || off.error || q.error} />
          </form>
        )}
      </Card>
    </>
  );
}
