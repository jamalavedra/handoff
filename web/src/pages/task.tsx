import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { ago, api, type Comment, type Event, type Member, type Task } from "../lib";
import { Button, Card, ErrorText, Input, Priority, Select, StatusBadge, Textarea, Who } from "../components/ui";
import { EventLine, useInvalidate } from "./tasks";

type Detail = { task: Task; comments: Comment[]; events: Event[] };

export function TaskPage() {
  const { id } = useParams({ strict: false }) as { id: string };
  const q = useQuery({ queryKey: ["task", id], queryFn: () => api.tool<Detail>("get_task", { id }) });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const invalidate = useInvalidate();
  const [comment, setComment] = useState("");
  const [blocker, setBlocker] = useState("");
  const update = useMutation({ mutationFn: (patch: Record<string, unknown>) => api.tool("update_task", { id, ...patch }), onSuccess: invalidate });
  const close = useMutation({ mutationFn: (status: string) => api.tool("close_task", { id, status }), onSuccess: invalidate });
  const trash = useMutation({ mutationFn: () => api.tool("trash_task", { id }), onSuccess: invalidate });
  const restore = useMutation({ mutationFn: () => api.tool("restore_task", { id }), onSuccess: invalidate });
  const claim = useMutation({ mutationFn: () => api.tool("claim_task", { id }), onSuccess: invalidate });
  const release = useMutation({ mutationFn: () => api.tool("release_task", { id }), onSuccess: invalidate });
  const addComment = useMutation({ mutationFn: () => api.tool("comment", { id, body: comment }), onSuccess: () => { setComment(""); invalidate(); } });
  const link = useMutation({ mutationFn: (v: { blocker: string; remove?: boolean }) => api.tool("link_tasks", { blocker: v.blocker, blocked: id, remove: v.remove }), onSuccess: () => { setBlocker(""); invalidate(); } });

  if (q.error) return <ErrorText error={q.error} />;
  if (!q.data) return null;
  const { task: t, comments, events } = q.data;
  const closed = t.status === "done" || t.status === "cancelled";
  const err = update.error || close.error || trash.error || restore.error || claim.error || release.error || addComment.error || link.error;
  const timeline: { at: string; e?: Event; c?: Comment }[] = [...events.map((e) => ({ at: e.at, e })), ...comments.map((c) => ({ at: c.created_at, c }))].sort((a, b) => a.at.localeCompare(b.at));

  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_260px]">
      <div className="min-w-0">
        <div className="mb-1 flex items-center gap-2 text-sm text-zinc-500">
          <Link to="/projects/$id" params={{ id: t.project_id }} className="hover:underline">{t.project}</Link>
          <span>/</span><span className="font-mono">{t.id}</span>
          {t.deleted_at && <span className="text-red-600">in trash</span>}
        </div>
        <h1 className="mb-3 text-xl font-semibold">{t.title}</h1>
        {t.body && <Card className="mb-4 whitespace-pre-wrap p-4 text-sm">{t.body}</Card>}
        <div className="mb-6 flex flex-wrap gap-2">
          {!closed && !t.deleted_at && (
            <>
              <Button size="sm" onClick={() => close.mutate("done")}>Mark done</Button>
              {t.claimed_by ? <Button size="sm" variant="outline" onClick={() => release.mutate()}>Release</Button> : t.ready && <Button size="sm" variant="outline" onClick={() => claim.mutate()}>I'm on it</Button>}
              <Button size="sm" variant="ghost" onClick={() => close.mutate("cancelled")}>Cancel task</Button>
            </>
          )}
          {closed && !t.deleted_at && <Button size="sm" variant="outline" onClick={() => update.mutate({ status: "open" })}>Reopen</Button>}
          {t.deleted_at ? <Button size="sm" variant="outline" onClick={() => restore.mutate()}>Restore</Button> : <Button size="sm" variant="ghost" className="text-red-600" onClick={() => trash.mutate()}>Trash</Button>}
        </div>
        <ErrorText error={err} />
        <h2 className="mb-2 text-sm font-medium text-zinc-500">Timeline</h2>
        <Card className="px-4 py-2">
          {timeline.map((x, i) =>
            x.e ? (
              x.e.kind === "commented" ? null : <EventLine key={i} e={x.e} showTask={false} />
            ) : (
              <div key={i} className="my-2 rounded-md bg-zinc-50 p-3 text-sm">
                <div className="mb-1 text-xs text-zinc-500"><Who m={x.c!.author} by={x.c!.on_behalf_of} /> · {ago(x.c!.created_at)}</div>
                <div className="whitespace-pre-wrap">{x.c!.body}</div>
              </div>
            ),
          )}
        </Card>
        {!t.deleted_at && (
          <form className="mt-3 flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); addComment.mutate(); }}>
            <Textarea rows={2} placeholder="Add a comment" value={comment} onChange={(e) => setComment(e.target.value)} aria-label="Comment" />
            <Button size="sm" className="self-end" disabled={!comment.trim()}>Comment</Button>
          </form>
        )}
      </div>
      <aside className="flex flex-col gap-4 text-sm">
        <Field label="Status"><StatusBadge status={t.status} />{t.ready && !closed && <span className="ml-2 text-xs text-green-700">ready</span>}</Field>
        {t.claimed_by && <Field label="Being worked on by"><Who m={t.claimed_by} /> <div className="text-xs text-zinc-500">until {new Date(t.claim_expires_at!).toLocaleTimeString()}</div></Field>}
        <Field label="Assignee">
          <Select className="w-full" value={t.assignee?.id ?? ""} onChange={(e) => update.mutate({ assignee: e.target.value })} aria-label="Assignee">
            <option value="">Unassigned</option>
            {members.data?.members.map((m) => <option key={m.id} value={m.id}>{m.name}{m.kind === "agent" ? " (agent)" : ""}</option>)}
          </Select>
        </Field>
        <Field label="Priority">
          <Select className="w-full" value={t.priority} onChange={(e) => update.mutate({ priority: Number(e.target.value) })} aria-label="Priority">
            {[0, 1, 2, 3, 4].map((p) => <option key={p} value={p}>P{p}</option>)}
          </Select>
        </Field>
        <Field label="Due"><Input type="date" value={t.due ?? ""} onChange={(e) => update.mutate({ due: e.target.value })} aria-label="Due" /></Field>
        <Field label="Waits on">
          {t.blocked_by.map((b) => (
            <div key={b} className="flex items-center justify-between">
              <Link to="/tasks/$id" params={{ id: b }} className="font-mono text-xs hover:underline">{b}</Link>
              <button className="text-xs text-zinc-400 hover:text-red-600" onClick={() => link.mutate({ blocker: b, remove: true })}>remove</button>
            </div>
          ))}
          <form className="mt-1 flex gap-1" onSubmit={(e) => { e.preventDefault(); link.mutate({ blocker }); }}>
            <Input placeholder="task id" value={blocker} onChange={(e) => setBlocker(e.target.value)} className="h-8" aria-label="Blocker id" />
            <Button size="sm" variant="outline" disabled={!blocker}>Add</Button>
          </form>
        </Field>
        {t.blocks.length > 0 && <Field label="Blocks">{t.blocks.map((b) => <Link key={b} to="/tasks/$id" params={{ id: b }} className="block font-mono text-xs hover:underline">{b}</Link>)}</Field>}
        <Field label="Created"><Who m={t.created_by} by={t.on_behalf_of} /><div className="text-xs text-zinc-500">{ago(t.created_at)}</div></Field>
        <Field label="Priority key"><span className="text-xs text-zinc-500"><Priority p={0} /> urgent … <Priority p={4} /> someday</span></Field>
      </aside>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <div><div className="mb-1 text-xs font-medium uppercase tracking-wide text-zinc-400">{label}</div>{children}</div>;
}
