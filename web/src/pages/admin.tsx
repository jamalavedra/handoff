import { useEffect, useState } from "react";
import { Bot, ChevronDown, Sparkles } from "lucide-react";
import { toast } from "sonner";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, cn, type Member } from "../lib";
import { Badge, Card, ErrorText } from "../components/app";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { useMe } from "../main";
import { SettingsHeader } from "./settings";
import { Navigate } from "@tanstack/react-router";
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
  // General settings are for admins; everyone else lands on their agents.
  if (me.data && me.data.member?.role !== "admin") return <Navigate to="/settings/agents" replace />;
  const people = members.data?.members.filter((m) => m.kind === "human") ?? [];
  return (
    <>
      <SettingsHeader active="general" />
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

type Usage = { month: string; input_tokens: number; output_tokens: number; calls: number; background_calls: number };
type Triage = {
  enabled: boolean; agent?: { id: string; name: string }; base_url?: string; model?: string; has_credential: boolean;
  background_base_url?: string; background_model?: string; background_has_credential?: boolean; monthly_token_cap?: number; usage?: Usage;
};

// Any OpenAI-compatible endpoint works; these are the common ones, each with a sensible default model.
const providers = [
  { name: "Google Gemini", url: "https://generativelanguage.googleapis.com/v1beta/openai", model: "gemini-2.5-flash", key: "Gemini API key (aistudio.google.com/apikey)" },
  { name: "Anthropic", url: "https://api.anthropic.com/v1", model: "claude-haiku-5-5", key: "Anthropic API key" },
  { name: "OpenAI", url: "https://api.openai.com/v1", model: "", key: "OpenAI API key" },
  { name: "OpenRouter", url: "https://openrouter.ai/api/v1", model: "google/gemini-2.5-flash", key: "OpenRouter API key" },
  { name: "Vercel AI Gateway", url: "https://ai-gateway.vercel.sh/v1", model: "google/gemini-2.5-flash", key: "AI Gateway API key" },
];

function TriageSettings({ agents }: { agents: Member[] }) {
  const invalidate = useInvalidate();
  const q = useQuery({ queryKey: ["triage"], queryFn: () => api.tool<Triage>("get_triage") });
  const [f, setF] = useState({ agent: "", base_url: providers[0].url, model: providers[0].model, credential: "" });
  const [advanced, setAdvanced] = useState(false);
  useEffect(() => {
    const t = q.data;
    if (t?.enabled) setF({ agent: t.agent?.id ?? "", base_url: t.base_url ?? "", model: t.model ?? "", credential: "" });
  }, [q.data]);
  // One agent? It's the obvious choice.
  useEffect(() => {
    if (!f.agent && agents.length === 1) setF((x) => ({ ...x, agent: agents[0].id }));
  }, [agents, f.agent]);
  const provider = providers.find((p) => p.url === f.base_url);

  const makeAgent = useMutation({
    mutationFn: () => api.tool<{ id: string }>("create_agent", { name: "Triage" }),
    onSuccess: (m) => { setF((x) => ({ ...x, agent: m.id })); invalidate(); },
  });
  const test = useMutation({ mutationFn: () => api.tool<{ ok: boolean; reply?: string; error?: string; seconds: string }>("test_triage", { base_url: f.base_url, model: f.model, credential: f.credential }) });
  const pick = (name: string) => {
    test.reset();
    const p = providers.find((x) => x.name === name);
    if (p) { setF({ ...f, base_url: p.url, model: p.model }); if (!p.model) setAdvanced(true); }
    else { setF({ ...f, base_url: "" }); setAdvanced(true); }
  };  const save = useMutation({ mutationFn: () => api.tool<Triage>("set_triage", f), onSuccess: () => { setF((x) => ({ ...x, credential: "" })); test.reset(); invalidate(); toast("Triage is on. Create a task to see it work."); } });
  const off = useMutation({ mutationFn: () => api.tool("disable_triage"), onSuccess: () => { invalidate(); toast("Triage is off"); } });
  const t = q.data;
  const canTest = !!f.base_url && !!f.model && (!!f.credential || !!t?.has_credential);
  return (
    <>
      <h2 className="mb-2 flex items-center gap-1.5 text-sm font-medium text-muted-foreground"><Sparkles className="size-3.5" />Triage</h2>
      <Card className="mb-8 p-4">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <span className={cn("rounded-full px-2 py-0.5 text-xs font-medium", t?.enabled ? "bg-green-50 text-green-700" : "bg-accent text-muted-foreground")}>{t?.enabled ? "On" : "Off"}</span>
          {t?.enabled && <span className="text-sm text-muted-foreground">{t.agent?.name} · {t.model}</span>}
        </div>
        <p className="mb-4 max-w-xl text-sm text-foreground/70">
          Every task someone creates is read by this agent: it tidies the title, fills in the date, priority, project and labels the text implies, suggests an owner, and adds a short plan.
        </p>
        <form className="flex max-w-md flex-col gap-3 text-sm" onSubmit={(e) => { e.preventDefault(); save.mutate(); }}>
          <label className="font-medium">Agent
            {agents.length === 0 ? (
              <div className="mt-1 flex items-center gap-2">
                <Button type="button" size="sm" variant="outline" onClick={() => makeAgent.mutate()} disabled={makeAgent.isPending}><Bot className="size-3.5" />Create a “Triage” agent</Button>
                <span className="text-xs font-normal text-muted-foreground">or connect one on the Agents page</span>
              </div>
            ) : (
              <NativeSelect required value={f.agent} onChange={(e) => setF({ ...f, agent: e.target.value })} aria-label="Triage agent">
                <option value="" disabled>Choose an agent</option>
                {agents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
              </NativeSelect>
            )}
          </label>
          <label className="font-medium">Model provider
            <NativeSelect value={provider?.name ?? "other"} onChange={(e) => pick(e.target.value)} aria-label="Model provider">
              {providers.map((p) => <option key={p.name} value={p.name}>{p.name}</option>)}
              <option value="other">Other (OpenAI-compatible)</option>
            </NativeSelect>
          </label>
          <label className="font-medium">{provider?.key ?? "API key or OAuth token"}
            <Input type="password" autoComplete="off" required={!t?.has_credential} placeholder={t?.has_credential ? "Saved · leave empty to keep it" : "Paste your key"} value={f.credential} onChange={(e) => { setF({ ...f, credential: e.target.value }); test.reset(); }} />
          </label>
          <button type="button" onClick={() => setAdvanced(!advanced)} className="flex items-center gap-1 self-start text-xs text-muted-foreground hover:text-foreground">
            <ChevronDown className={cn("size-3.5 transition-transform", !advanced && "-rotate-90")} />Model {f.model ? <span className="font-mono">{f.model}</span> : null}
          </button>
          {advanced && (
            <div className="flex flex-col gap-3 rounded-lg bg-muted p-3">
              <label className="font-medium">Base URL<Input required placeholder="https://…/v1" value={f.base_url} onChange={(e) => { setF({ ...f, base_url: e.target.value }); test.reset(); }} /></label>
              <label className="font-medium">Model<Input required placeholder="model id as the provider names it" value={f.model} onChange={(e) => { setF({ ...f, model: e.target.value }); test.reset(); }} /></label>
              <span className="text-xs text-muted-foreground">The key is sent as a bearer token and stored encrypted.</span>
            </div>
          )}
          {test.data && (
            <p className={cn("rounded-lg px-3 py-2 text-xs", test.data.ok ? "bg-green-50 text-green-800" : "bg-red-50 text-red-800")}>
              {test.data.ok ? <>Works ({test.data.seconds}s): “{test.data.reply}”</> : <>Didn't work: {test.data.error}</>}
            </p>
          )}
          <div className="flex items-center gap-2">
            <Button type="button" variant="outline" onClick={() => test.mutate()} disabled={!canTest || test.isPending}>{test.isPending ? "Testing…" : "Test"}</Button>
            <Button type="submit" disabled={save.isPending || !f.agent}>{t?.enabled ? "Save" : "Turn on"}</Button>
            {t?.enabled && <Button type="button" variant="ghost" onClick={() => off.mutate()} disabled={off.isPending}>Turn off</Button>}
          </div>
          <ErrorText error={save.error || off.error || q.error || test.error || makeAgent.error} />
        </form>
        {t?.enabled && <BackgroundSettings t={t} />}
      </Card>
    </>
  );
}

const tokens = (n: number) => n >= 1e6 ? `${(n / 1e6).toFixed(1)}M` : n >= 1e3 ? `${Math.round(n / 1e3)}k` : String(n);

// The model for the agent's background jobs (memory upkeep, reviews), and the monthly cap that pauses them.
function BackgroundSettings({ t }: { t: Triage }) {
  const invalidate = useInvalidate();
  const [f, setF] = useState({ base_url: "", model: "", credential: "" });
  const [cap, setCap] = useState("");
  useEffect(() => {
    setF({ base_url: t.background_base_url || t.base_url || "", model: t.background_model ?? "", credential: "" });
    setCap(t.monthly_token_cap ? String(t.monthly_token_cap) : "");
  }, [t.background_base_url, t.background_model, t.base_url, t.monthly_token_cap]);
  const done = (msg: string) => () => { setF((x) => ({ ...x, credential: "" })); invalidate(); toast(msg); };
  const save = useMutation({ mutationFn: (v: typeof f) => api.tool<Triage>("set_background_model", v), onSuccess: done("Background model saved") });
  const clear = useMutation({ mutationFn: () => api.tool<Triage>("set_background_model", {}), onSuccess: done("Background jobs use the agent's model") });
  const saveCap = useMutation({ mutationFn: () => api.tool<Triage>("set_monthly_token_cap", { monthly_token_cap: Number(cap) || 0 }), onSuccess: done("Monthly cap saved") });
  const sameEndpoint = f.base_url.replace(/\/+$/, "") === t.base_url;
  const u = t.usage;
  const used = u ? u.input_tokens + u.output_tokens : 0;
  const stopped = !!t.monthly_token_cap && used >= t.monthly_token_cap;
  return (
    <div className="mt-6 max-w-md border-t border-border pt-4 text-sm">
      <div className="mb-1 font-medium">Background model <span className="font-normal text-muted-foreground">(optional)</span></div>
      <p className="mb-3 text-foreground/70">
        Writes what nobody waits for, like keeping the agent's notes tidy. A cheaper model is usually enough. Without one, it uses {t.model}.
      </p>
      <form className="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); save.mutate(f); }}>
        <label className="font-medium">Base URL<Input required placeholder="https://…/v1" value={f.base_url} onChange={(e) => setF({ ...f, base_url: e.target.value })} /></label>
        <label className="font-medium">Model<Input required placeholder="model id as the provider names it" value={f.model} onChange={(e) => setF({ ...f, model: e.target.value })} /></label>
        <label className="font-medium">API key
          <Input type="password" autoComplete="off" value={f.credential} onChange={(e) => setF({ ...f, credential: e.target.value })}
            placeholder={t.background_has_credential ? "Saved · leave empty to keep it" : sameEndpoint ? "Leave empty to use the agent's key" : "Paste the key for this endpoint"} />
        </label>
        <div className="flex items-center gap-2">
          <Button type="submit" variant="outline" disabled={save.isPending}>Save</Button>
          {t.background_model && <Button type="button" variant="ghost" onClick={() => clear.mutate()} disabled={clear.isPending}>Use the agent's model</Button>}
        </div>
      </form>
      <form className="mt-5 flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); saveCap.mutate(); }}>
        <label className="font-medium">Monthly token cap
          <div className="mt-1 flex items-center gap-2">
            <Input type="number" min={0} step={1000} placeholder="No cap" value={cap} onChange={(e) => setCap(e.target.value)} />
            <Button type="submit" variant="outline" disabled={saveCap.isPending}>Save</Button>
          </div>
          <span className="text-xs font-normal text-muted-foreground">Counts every model call this month. Once reached, background jobs pause until next month or a higher cap, then pick up where they left off; triage, chat and the assistant keep working.</span>
        </label>
        {u && (
          <p className={cn("text-xs", stopped ? "text-amber-700" : "text-muted-foreground")}>
            This month: {tokens(used)} tokens in {u.calls} calls ({u.background_calls} in the background)
            {t.monthly_token_cap ? ` · ${Math.min(100, Math.round((used / t.monthly_token_cap) * 100))}% of the cap` : ""}
            {stopped ? " · background jobs are paused until next month or a higher cap" : ""}
          </p>
        )}
      </form>
      <ErrorText error={save.error || clear.error || saveCap.error} />
    </div>
  );
}
