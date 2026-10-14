import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Globe, Plug, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api, cn } from "../lib";
import { Card, Empty, ErrorText } from "../components/app";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { NativeSelect } from "@/components/ui/native-select";
import { SettingsHeader } from "./settings";

type Tool = { name: string; description?: string; read_only: boolean; destructive?: boolean; enabled: boolean };
type Connection = { id: string; name: string; url: string; auth: string; header_name?: string; has_secret: boolean; tools: Tool[]; last_error?: string };

// Hosted MCP servers that work well for the agent. Parallel needs no account; Exa works without a key
// at low limits, and a key raises them.
const presets = [
  { name: "Web search", vendor: "Parallel", url: "https://search.parallel.ai/mcp", auth: "none", header: "", note: "Free, no account needed" },
  { name: "Exa search", vendor: "Exa", url: "https://mcp.exa.ai/mcp", auth: "header", header: "x-api-key", note: "Works without a key; add one for higher limits", optionalKey: true },
];

export function ConnectionsPage() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["connections"], queryFn: () => api.tool<{ connections: Connection[] }>("list_connections") });
  const connections = q.data?.connections ?? [];
  const refresh = () => qc.invalidateQueries({ queryKey: ["connections"] });
  // Coming back from a service's sign-in screen.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const connected = params.get("connected"), error = params.get("error");
    if (!connected && !error) return;
    if (connected) toast(`${connected} connected`);
    else toast.error(error);
    window.history.replaceState(null, "", window.location.pathname);
  }, []);
  return (
    <>
      <SettingsHeader active="connections" />
      <p className="mb-4 max-w-2xl text-sm text-foreground/70">
        Connect remote MCP servers so the workspace's agent can look things up while it works: when it triages a task, answers in a task's chat, or takes one on. Tools that only read are switched on; tools that change things stay off.
      </p>
      <h2 className="mb-2 text-[13px] font-medium text-muted-foreground">Add</h2>
      <div className="mb-3 grid gap-3 sm:grid-cols-2">
        {presets.map((p) => <Preset key={p.url} p={p} added={connections.some((c) => c.url === p.url)} onAdded={refresh} />)}
      </div>
      <CustomConnection onAdded={refresh} />
      <h2 className="mt-8 mb-2 text-[13px] font-medium text-muted-foreground">Connected</h2>
      <ErrorText error={q.error} />
      {q.data && connections.length === 0 && <Card><Empty>Nothing connected yet. Web search is a good first one.</Empty></Card>}
      <div className="flex flex-col gap-3">
        {connections.map((c) => <ConnectionCard key={c.id} c={c} onChange={refresh} />)}
      </div>
    </>
  );
}

function Preset({ p, added, onAdded }: { p: (typeof presets)[number]; added: boolean; onAdded: () => void }) {
  const [key, setKey] = useState("");
  const add = useMutation({
    mutationFn: () => api.tool("add_connection", { name: p.name, url: p.url, auth: key ? p.auth : "none", header_name: key ? p.header : undefined, secret: key || undefined }),
    onSuccess: () => { toast(`${p.vendor} connected`); setKey(""); onAdded(); },
  });
  return (
    <Card className="flex flex-col gap-2 p-4">
      <div className="flex items-center gap-2">
        <span className="flex size-8 items-center justify-center rounded-lg bg-accent"><Globe className="size-4" /></span>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">{p.name} <span className="font-normal text-muted-foreground">· {p.vendor}</span></div>
          <div className="text-xs text-muted-foreground">{p.note}</div>
        </div>
      </div>
      {p.optionalKey && !added && <Input type="password" autoComplete="off" placeholder="API key (optional)" value={key} onChange={(e) => setKey(e.target.value)} aria-label={`${p.vendor} API key`} />}
      <Button size="sm" variant={added ? "ghost" : "secondary"} disabled={added || add.isPending} onClick={() => add.mutate()} className="self-start">
        <Plug className="size-3.5" />{added ? "Connected" : add.isPending ? "Connecting…" : "Connect"}
      </Button>
      <ErrorText error={add.error} />
    </Card>
  );
}

function CustomConnection({ onAdded }: { onAdded: () => void }) {
  const blank = { name: "", url: "", auth: "oauth", header_name: "", secret: "" };
  const [f, setF] = useState(blank);
  const [open, setOpen] = useState(false);
  const add = useMutation({
    mutationFn: async () => {
      if (f.auth !== "oauth") { await api.tool("add_connection", f); return null; }
      // The service shows its own approval screen, then sends the browser back to this page.
      const { authorize_url } = await api.tool<{ authorize_url: string }>("start_connection_oauth", { name: f.name, url: f.url, origin: window.location.origin });
      return authorize_url;
    },
    onSuccess: (r) => {
      if (r) { window.location.assign(r); return; }
      toast(`${f.name} connected`); setF(blank); setOpen(false); onAdded();
    },
  });
  if (!open) return <Button size="sm" variant="ghost" onClick={() => setOpen(true)}><Plug className="size-3.5" />Another MCP server…</Button>;
  return (
    <Card className="p-4">
      <form className="flex max-w-lg flex-col gap-3 text-sm" onSubmit={(e) => { e.preventDefault(); if (!add.isPending) add.mutate(); }}>
        <label className="font-medium">Name<Input required placeholder="e.g. Company wiki" value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} /></label>
        <label className="font-medium">Server URL<Input required type="url" placeholder="https://…/mcp" value={f.url} onChange={(e) => setF({ ...f, url: e.target.value })} />
          <span className="text-xs font-normal text-muted-foreground">A remote MCP server over streamable HTTP.</span>
        </label>
        <label className="font-medium">Sign-in
          <NativeSelect value={f.auth} onChange={(e) => setF({ ...f, auth: e.target.value })} aria-label="Sign-in">
            <option value="oauth">Sign in with the service</option>
            <option value="none">None</option>
            <option value="bearer">Bearer token</option>
            <option value="header">API key in a header</option>
          </NativeSelect>
        </label>
        {f.auth === "header" && <label className="font-medium">Header name<Input required placeholder="x-api-key" value={f.header_name} onChange={(e) => setF({ ...f, header_name: e.target.value })} /></label>}
        {f.auth === "oauth" && <span className="-mt-1 text-xs text-muted-foreground">You'll approve access on the service's own page. The connection is the workspace's and uses your access there.</span>}
        {f.auth !== "none" && f.auth !== "oauth" && <label className="font-medium">{f.auth === "bearer" ? "Token" : "API key"}<Input required type="password" autoComplete="off" value={f.secret} onChange={(e) => setF({ ...f, secret: e.target.value })} />
          <span className="text-xs font-normal text-muted-foreground">Stored encrypted and never shown again.</span>
        </label>}
        <div className="flex gap-2">
          <Button type="submit" disabled={add.isPending}>{add.isPending ? "Connecting…" : f.auth === "oauth" ? "Continue to sign in" : "Connect"}</Button>
          <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
        </div>
        <ErrorText error={add.error} />
      </form>
    </Card>
  );
}

function ConnectionCard({ c, onChange }: { c: Connection; onChange: () => void }) {
  const set = useMutation({ mutationFn: (enabled: string[]) => api.tool("set_connection_tools", { id: c.id, enabled }), onSuccess: onChange });
  const refresh = useMutation({ mutationFn: () => api.tool("refresh_connection", { id: c.id }), onSuccess: () => { toast("Tools refreshed"); onChange(); } });
  const remove = useMutation({ mutationFn: () => api.tool("remove_connection", { id: c.id }), onSuccess: () => { toast(`${c.name} disconnected`); onChange(); } });
  const on = c.tools.filter((t) => t.enabled).map((t) => t.name);
  const toggle = (name: string) => set.mutate(on.includes(name) ? on.filter((n) => n !== name) : [...on, name]);
  return (
    <Card className="p-4">
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <span className="flex size-8 items-center justify-center rounded-lg bg-accent"><Plug className="size-4" /></span>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">{c.name}</div>
          <div className="truncate font-mono text-xs text-muted-foreground">{c.url}{c.auth === "oauth" ? " · signed in" : c.has_secret ? " · key saved" : ""}</div>
        </div>
        <Button size="sm" variant="ghost" onClick={() => refresh.mutate()} disabled={refresh.isPending} title="List its tools again"><RefreshCw className={cn("size-3.5", refresh.isPending && "animate-spin")} />Refresh</Button>
        <Button size="sm" variant="ghost" className="text-red-600" onClick={() => confirm(`Disconnect ${c.name}? The agent stops using its tools.`) && remove.mutate()}><Trash2 className="size-3.5" />Remove</Button>
      </div>
      {c.last_error && <p className="mb-2 text-xs text-red-600">Last check failed: {c.last_error}</p>}
      <div className="flex flex-col divide-y divide-border">
        {c.tools.map((t) => (
          <label key={t.name} className={cn("flex items-start gap-3 py-2 text-sm", t.destructive ? "opacity-60" : "cursor-pointer")}>
            <input type="checkbox" className="mt-1" checked={t.enabled} disabled={t.destructive || set.isPending} onChange={() => toggle(t.name)} />
            <span className="min-w-0 flex-1">
              <span className="flex flex-wrap items-center gap-1.5">
                <span className="font-mono text-[13px]">{t.name}</span>
                {t.read_only && <span className="rounded-full bg-green-50 px-1.5 text-[11px] text-green-700">reads only</span>}
                {!t.read_only && !t.destructive && <span className="rounded-full bg-amber-50 px-1.5 text-[11px] text-amber-700">not marked read-only</span>}
                {t.destructive && <span className="rounded-full bg-red-50 px-1.5 text-[11px] text-red-700">changes things · not available yet</span>}
              </span>
              {t.description && <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">{t.description}</span>}
            </span>
          </label>
        ))}
      </div>
      <ErrorText error={set.error || refresh.error || remove.error} />
    </Card>
  );
}
