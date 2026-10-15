import { useEffect, useRef, useState } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useCanGoBack, useNavigate, useParams, useRouter } from "@tanstack/react-router";
import { ChevronLeft, History, Link2, MoreHorizontal, Plus, RotateCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ago, api, cn, type Event, type Note } from "../lib";
import { Card, Empty, ErrorText } from "../components/app";
import { DocEditor } from "../components/editor";
import { undoable } from "../components/feedback";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Skeleton } from "@/components/ui/skeleton";
import { EventLine } from "./tasks";

// Notes are markdown documents inside a project: what the team knows about it. They're made from the
// project's Notes tab, and the workspace's agent reads them as context for the project's tasks.

type NoteDetail = { note: Note; events: Event[] };

const pageSize = 100;

// useProjectNotes pages through a project's notes, newest first.
export function useProjectNotes(project: string) {
  const q = useInfiniteQuery({
    queryKey: ["notes", "project", project],
    queryFn: ({ pageParam }) => api.tool<{ notes: Note[] }>("list_notes", { project, limit: pageSize, offset: pageParam }),
    initialPageParam: 0,
    getNextPageParam: (last, all) => (last.notes.length < pageSize ? undefined : all.length * pageSize),
  });
  return { ...q, notes: q.data?.pages.flatMap((p) => p.notes) };
}

// ProjectNotes lists a project's notes, newest first, with more on request.
export function ProjectNotes({ notes, more, loadingMore }: { notes?: Note[]; more?: () => void; loadingMore?: boolean }) {
  if (!notes) {
    return (
      <Card aria-busy="true" aria-label="Loading notes">
        {[58, 44, 66].map((w) => <div key={w} className="border-b border-border px-4 py-3.5 last:border-0"><Skeleton className="h-3.5 rounded-full" style={{ width: `${w}%` }} /></div>)}
      </Card>
    );
  }
  if (!notes.length) return <Card><Empty>No notes yet. Notes are this project's context: decisions, preferences, references. The agent reads them whenever it works on a task here.</Empty></Card>;
  return (
    <Card>
      {notes.map((n) => (
        <Link key={n.id} to="/notes/$id" params={{ id: n.id }} className="flex items-center justify-between gap-3 border-b border-border px-4 py-3 last:border-0 hover:bg-muted">
          <div className="min-w-0">
            <div className="truncate text-sm font-medium"><span aria-hidden className="mr-1.5">📝</span>{n.title}</div>
            <div className="truncate text-xs text-muted-foreground">
              {n.created_by?.name ?? "Someone"}{n.on_behalf_of ? ` for ${n.on_behalf_of.name}` : ""}{n.updated_by && n.updated_by.id !== n.created_by?.id ? ` · edited by ${n.updated_by.name}` : ""}
            </div>
          </div>
          <span className="shrink-0 text-xs text-muted-foreground/80">{ago(n.updated_at)}</span>
        </Link>
      ))}
      {more && (
        <button onClick={more} disabled={loadingMore} className="w-full border-t border-border px-4 py-2.5 text-sm text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-60">
          {loadingMore ? "Loading…" : "Show more"}
        </button>
      )}
    </Card>
  );
}

// NewNote starts an untitled note in a project and opens it, title first.
export function NewNote({ project }: { project: string }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const m = useMutation({
    mutationFn: () => api.tool<{ note: Note }>("create_note", { project, title: "Untitled" }),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ["notes"] });
      fresh.add(r.note.id);
      navigate({ to: "/notes/$id", params: { id: r.note.id } });
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : "Couldn't create the note"),
  });
  return (
    <Button variant="secondary" size="sm" className="text-[13px]" disabled={m.isPending} onClick={() => m.mutate()}>
      <Plus className="size-4" />New note
    </Button>
  );
}

// Notes just made here open with their title selected, ready to be named.
const fresh = new Set<string>();

function NoteTitle({ note, disabled, onSave }: { note: Note; disabled: boolean; onSave: (title: string) => void }) {
  const [v, setV] = useState(note.title);
  const discard = useRef(false);
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => setV(note.title), [note.title]);
  useEffect(() => {
    if (fresh.delete(note.id)) {
      ref.current?.focus();
      ref.current?.select();
    }
  }, [note.id]);
  const commit = () => {
    if (discard.current) { discard.current = false; return; }
    const next = v.replace(/\s+/g, " ").trim();
    if (!next) setV(note.title);
    else if (next !== note.title) onSave(next);
  };
  return (
    <textarea
      ref={ref}
      aria-label="Title"
      rows={1}
      value={v}
      readOnly={disabled}
      maxLength={200}
      onChange={(e) => setV(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") { e.preventDefault(); e.currentTarget.blur(); }
        if (e.key === "Escape") { discard.current = true; setV(note.title); e.currentTarget.blur(); }
      }}
      className="-mx-1 w-full resize-none rounded-md bg-transparent px-1 text-2xl leading-tight font-semibold outline-none [field-sizing:content] focus:bg-card focus:ring-2 focus:ring-input"
    />
  );
}

export function NotePage() {
  const { id } = useParams({ strict: false }) as { id: string };
  const navigate = useNavigate();
  const router = useRouter();
  const canGoBack = useCanGoBack();
  const qc = useQueryClient();
  const q = useQuery({ queryKey: ["note", id], queryFn: () => api.tool<NoteDetail>("get_note", { id }) });
  const [menu, setMenu] = useState(false);
  const [activityOpen, setActivityOpen] = useState(false);
  const update = useMutation({
    mutationFn: (patch: { title?: string; body?: string }) => api.tool<{ note: Note }>("update_note", { id, ...patch }),
    onSuccess: (r) => {
      qc.setQueryData<NoteDetail>(["note", id], (d) => (d ? { ...d, note: r.note } : d));
      qc.invalidateQueries();
    },
  });
  const trash = useMutation({
    mutationFn: () => api.tool("trash_note", { id }),
    onSuccess: () => { qc.invalidateQueries(); undoable(qc, "Moved to trash", () => api.tool("restore_note", { id })); },
  });
  const restore = useMutation({ mutationFn: () => api.tool("restore_note", { id }), onSuccess: () => qc.invalidateQueries() });
  const goBack = () => (canGoBack ? router.history.back() : navigate({ to: "/" }));
  const back = (
    <button onClick={goBack} className="-ml-2 flex h-9 items-center gap-0.5 rounded-full pr-2 pl-1 text-sm text-muted-foreground hover:bg-accent" aria-label="Back">
      <ChevronLeft className="size-5" /><span className="hidden sm:inline">Back</span>
    </button>
  );
  if (q.error) return <div>{back}<ErrorText error={q.error} /></div>;
  if (!q.data) return back;
  const n = q.data.note;
  const locked = !!n.deleted_at;
  const copy = () => {
    const url = `${window.location.origin}/notes/${n.id}`;
    navigator.clipboard?.writeText(url).then(() => toast("Link copied", { description: url }), () => toast.error("Couldn't copy the link"));
  };
  const item = "flex w-full items-center gap-2.5 px-3 py-2.5 text-left text-sm hover:bg-muted";
  return (
    <div className="mx-auto flex max-w-3xl flex-col">
      <div className="mb-3 flex items-center gap-1">
        {back}
        <span className="min-w-0 flex-1 truncate text-sm text-muted-foreground/70">
          <Link to="/projects/$id" params={{ id: n.project_id }} search={(p) => ({ ...p, tab: "notes" })} className="hover:text-foreground/80 hover:underline">{n.project}</Link>
          <span className="mx-1.5">/</span>Notes
          {locked && <span className="ml-2 font-medium text-red-600">In trash</span>}
        </span>
        <button onClick={copy} className="flex size-9 items-center justify-center rounded-full text-muted-foreground hover:bg-accent" aria-label="Copy link" title="Copy link">
          <Link2 className="size-4" />
        </button>
        <Popover open={menu} onOpenChange={setMenu}>
          <PopoverTrigger asChild>
            <button aria-label="More actions" className="flex size-9 items-center justify-center rounded-full text-muted-foreground hover:bg-accent"><MoreHorizontal className="size-5" /></button>
          </PopoverTrigger>
          <PopoverContent align="end" className="w-48 overflow-hidden rounded-xl p-0 py-1">
            <div role="menu">
              <button role="menuitem" className={cn(item, "text-foreground/80")} onClick={() => { setActivityOpen(true); setMenu(false); }}><History className="size-4" />Activity <span className="ml-auto text-xs text-muted-foreground/70 tabular-nums">{q.data.events.length}</span></button>
              <button role="menuitem" className={cn(item, "text-foreground/80")} onClick={() => { copy(); setMenu(false); }}><Link2 className="size-4" />Copy link</button>
              {locked
                ? <button role="menuitem" className={cn(item, "text-foreground/80")} onClick={() => { restore.mutate(); setMenu(false); }}><RotateCcw className="size-4" />Restore</button>
                : <button role="menuitem" className={cn(item, "text-red-600")} onClick={() => { trash.mutate(); setMenu(false); }}><Trash2 className="size-4" />Move to trash</button>}
            </div>
          </PopoverContent>
        </Popover>
      </div>

      <div className="mb-1"><NoteTitle note={n} disabled={locked} onSave={(title) => update.mutate({ title })} /></div>
      <p className="mb-5 text-[13px] text-muted-foreground">
        {n.created_by?.name ?? "Someone"}{n.on_behalf_of ? ` for ${n.on_behalf_of.name}` : ""}
        {" · "}updated {ago(n.updated_at)}{n.updated_by && n.updated_by.id !== n.created_by?.id ? ` by ${n.updated_by.name}` : ""}
      </p>
      <ErrorText error={update.error || trash.error || restore.error} />

      <div className="mb-10">
        <DocEditor key={n.id} id={n.id} body={n.body ?? ""} name={n.title} empty="Write the note…" disabled={locked} onSave={(body) => update.mutateAsync({ body })} />
      </div>

      <Dialog open={activityOpen} onOpenChange={setActivityOpen}>
        <DialogContent className="max-h-[80dvh] overflow-y-auto rounded-2xl sm:max-w-lg">
          <DialogTitle>Activity</DialogTitle>
          <DialogDescription>Written by {n.created_by?.name ?? "someone"}{n.on_behalf_of ? ` for ${n.on_behalf_of.name}` : ""} · {ago(n.created_at)}</DialogDescription>
          <div>
            {q.data.events.map((e) => (
              <EventLine key={e.id} e={e} showTask={false} showNote={false} restoreKind="note_updated" onRestore={locked ? undefined : (body) => update.mutate({ body }, { onSuccess: () => { toast("Previous version restored"); setActivityOpen(false); } })} />
            ))}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}
