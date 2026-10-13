import { useEffect, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { api, type Member } from "../lib";
import { Badge, Button, Card, ErrorText, Input, PageHeader, Select } from "../components/ui";
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
      <h2 className="mb-2 text-sm font-medium text-zinc-500">People</h2>
      <Card className="mb-3 p-3">
        <form className="flex flex-col gap-2 sm:flex-row" onSubmit={(e) => { e.preventDefault(); invite.mutate(); }}>
          <Input required type="email" placeholder="colleague@company.com" value={inv.email} onChange={(e) => setInv({ ...inv, email: e.target.value })} />
          <Select value={inv.role} onChange={(e) => setInv({ ...inv, role: e.target.value })} aria-label="Role"><option value="member">Member</option><option value="admin">Admin</option></Select>
          <Button type="submit">Invite</Button>
        </form>
        <p className="mt-2 text-xs text-zinc-500">Invited people sign in at {me.data?.base_url} with their email.</p>
        <ErrorText error={invite.error} />
      </Card>
      <Card className="mb-8">
        {people.map((m) => (
          <div key={m.id} className="flex flex-wrap items-center gap-3 border-b border-zinc-100 px-4 py-2.5 text-sm last:border-0">
            <div className="min-w-40 flex-1"><div className="font-medium">{m.name}</div><div className="text-xs text-zinc-500">{m.email}</div></div>
            {m.status === "invited" && <Badge>invited</Badge>}
            <Select value={m.role} onChange={(e) => change.mutate({ id: m.id, role: e.target.value })} aria-label={`Role for ${m.name}`}><option value="member">Member</option><option value="admin">Admin</option></Select>
            {m.id !== me.data?.member?.id && <Button size="sm" variant="ghost" className="text-red-600" onClick={() => confirm(`Remove ${m.name}? Their agents are disconnected too.`) && change.mutate({ id: m.id, remove: true })}>Remove</Button>}
          </div>
        ))}
        <div className="px-4"><ErrorText error={change.error} /></div>
      </Card>
      <h2 className="mb-2 text-sm font-medium text-zinc-500">Workspace</h2>
      <Card className="p-4">
        <form className="flex max-w-md flex-col gap-3 text-sm" onSubmit={(e) => { e.preventDefault(); saveOrg.mutate(); }}>
          <label className="font-medium">Name<Input value={org.name} onChange={(e) => setOrg({ ...org, name: e.target.value })} /></label>
          <label className="font-medium">Custom domain<Input placeholder="tasks.acme.com" value={org.custom_domain} onChange={(e) => setOrg({ ...org, custom_domain: e.target.value })} />
            <span className="text-xs font-normal text-zinc-500">Point a CNAME at {me.data?.org?.slug}.{new URL(me.data?.base_url ?? "http://x").host.split(".").slice(1).join(".")}; the certificate is issued automatically.</span>
          </label>
          <label className="font-medium">Anyone with this email domain can join<Input placeholder="acme.com" value={org.allowed_email_domain} onChange={(e) => setOrg({ ...org, allowed_email_domain: e.target.value })} /></label>
          <Button className="self-start" type="submit">Save</Button>
          {saveOrg.isSuccess && <span className="text-xs text-green-700">Saved.</span>}
          <ErrorText error={saveOrg.error} />
        </form>
        <div className="mt-6 border-t border-zinc-100 pt-4 text-sm">
          <div className="mb-1 font-medium">Beads</div>
          <p className="mb-2 text-zinc-600">Export every task as a beads <code>issues.jsonl</code>, or import one with <code>handoff bd import</code> / <code>POST /api/beads/import</code>.</p>
          <a className="underline" href="/api/beads/export">Download issues.jsonl</a>
        </div>
      </Card>
    </>
  );
}
