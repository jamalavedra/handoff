import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, GripVertical, Plus, X } from "lucide-react";
import { ago, api, cn, localDrafts, type Task } from "../lib";
import { moveTodo, oneLine, patchTodo, type Todo } from "../todos";
import { markLocal } from "./feedback";
import { ErrorText } from "./app";
import { Markdown } from "./markdown";
import type { Caret } from "./rich-editor";

// A task reads as one page: its text, then its todos right under it, in the same type.

// The editor (Tiptap) is a sizeable chunk; it loads when someone heads for the text.
const loadEditor = () => import("./rich-editor");
const RichEditor = lazy(loadEditor);

const textStyle = "text-[15px] leading-relaxed text-foreground/80 sm:text-sm";

// An edit whose save hasn't gone through waits here, per task, so leaving the page mid-save (or a
// failed save after it) never loses it: the task offers it back next time.
const drafts = localDrafts("handoff-text-draft:");

// TaskText is the task's own text, shown formatted (headings, lists, bold, links) like a page, never
// as markdown source. Click anywhere in it and you're typing at that spot, in the same layout: no
// box, no toolbar. It saves on clicking away or ⌘/Ctrl+Enter; Esc puts it back. Markdown shortcuts
// (## , - , **bold**) format as you type; each line break stays a line break.
export function TaskText({ id, body, disabled, onSave }: { id: string; body: string; disabled: boolean; onSave: (body: string) => Promise<unknown> }) {
  const [editing, setEditing] = useState<{ caret: Caret; base: string; draft?: string } | null>(null);
  const [lost, setLost] = useState(() => { const d = drafts.get(id); return d !== null && d !== body ? d : null; });
  // Just-saved text, shown until the refetched task catches up.
  const [saved, setSaved] = useState<{ text: string; over: string } | null>(null);
  const view = useRef<HTMLDivElement>(null);
  const shown = saved && saved.over === body ? saved.text : body;
  const edit = (caret: Caret, draft?: string) => { if (!disabled) setEditing({ caret, base: shown, draft }); };

  if (editing) {
    const fallback = <div className={cn("-mx-2 px-2 py-1", textStyle)}><Markdown text={editing.base} breaks /></div>;
    return (
      <Suspense fallback={fallback}>
        <RichEditor
          inline
          breaks
          label="Task text"
          body={editing.base}
          draft={editing.draft}
          changed={body !== editing.base && !(saved && saved.over === body)}
          placeholder="Add details…"
          caret={editing.caret}
          onSave={(md) => { drafts.set(id, md); return onSave(md).then(() => { drafts.drop(id); setLost(null); }); }}
          onSaved={(text) => { setSaved({ text, over: body }); setEditing((e) => e && { ...e, base: text, draft: undefined }); }}
          onLost={(md) => { drafts.set(id, md); setLost(md); }}
          onClose={() => setEditing(null)}
        />
      </Suspense>
    );
  }
  const recover = lost !== null && !disabled && (
    <div className="mb-2 flex items-center gap-3 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900">
      <span className="min-w-0 flex-1">Your last edit here didn't save.</span>
      <button className="text-xs font-medium hover:underline" onClick={() => edit("end", lost)}>Restore</button>
      <button className="text-xs text-amber-700/70 hover:text-red-600" onClick={() => { drafts.drop(id); setLost(null); }}>Discard</button>
    </div>
  );
  if (!shown) {
    return disabled ? null : (
      <>{recover}<button className={cn("-mx-2 block w-[calc(100%+1rem)] rounded-lg px-2 py-1 text-left text-muted-foreground/60 hover:text-muted-foreground", textStyle)} onPointerEnter={loadEditor} onFocus={loadEditor} onClick={() => edit("end")}>Add details…</button></>
    );
  }
  return (
    <>{recover}<div
      ref={view}
      role={disabled ? undefined : "button"}
      tabIndex={disabled ? undefined : 0}
      aria-label={disabled ? undefined : "Edit task text"}
      onPointerEnter={disabled ? undefined : loadEditor}
      onFocus={disabled ? undefined : loadEditor}
      onClick={(e) => {
        // Selecting text to copy it, or following a link, isn't editing.
        if (window.getSelection()?.toString() || (e.target as HTMLElement).closest("a, input")) return;
        const r = view.current!.getBoundingClientRect();
        // The editor lays out exactly like this view, so the caret lands where the click did.
        edit(e.detail ? { x: e.clientX - r.left - 8, y: e.clientY - r.top - 4 } : "end");
      }}
      onKeyDown={(e) => { if (e.key === "Enter" && e.target === e.currentTarget) { e.preventDefault(); edit("end"); } }}
      className={cn("-mx-2 rounded-lg px-2 py-1 outline-none focus-visible:ring-2 focus-visible:ring-ring/40", textStyle, !disabled && "cursor-text")}
    >
      <Markdown text={shown} breaks />
    </div></>
  );
}

type Detail = { task: Task };

// Todos is a task's checklist of steps: tick, click a step to rename it, add at the end, remove on
// hover. A step an agent did shows its one-line result after an arrow. Steps can be dragged, or moved
// with Alt+↑/↓.
export function Todos({ task, todos, disabled }: { task: string; todos: Todo[]; disabled: boolean }) {
  const qc = useQueryClient();
  const [adding, setAdding] = useState("");
  const [dragging, setDragging] = useState<string | null>(null);
  const addRef = useRef<HTMLInputElement>(null);
  // Changes show at once; the server's answer (or a rollback) follows.
  const optimistic = (f: (ts: Todo[]) => Todo[]) => {
    const key = ["task", task];
    const prev = qc.getQueryData<Detail>(key);
    if (prev) qc.setQueryData(key, { ...prev, task: { ...prev.task, todos: f(prev.task.todos ?? []) } });
    return () => qc.setQueryData(key, prev);
  };
  const run = useMutation({
    mutationFn: (v: { tool: string; input: object; apply?: (ts: Todo[]) => Todo[] }) => { markLocal(task); return api.tool(v.tool, v.input); },
    onMutate: (v) => (v.apply ? optimistic(v.apply) : undefined),
    onError: (_e, _v, rollback) => rollback?.(),
    onSettled: () => qc.invalidateQueries(),
  });
  const update = (td: Todo, patch: { done?: boolean; text?: string; position?: number }) =>
    run.mutate({
      tool: "update_todo",
      input: { id: td.id, ...patch },
      apply: (ts) => (patch.position !== undefined ? moveTodo(ts, td.id, patch.position) : patchTodo(ts, td.id, patch)),
    });
  const remove = (td: Todo) => run.mutate({ tool: "remove_todo", input: { id: td.id }, apply: (ts) => ts.filter((x) => x.id !== td.id) });
  const add = () => {
    const text = oneLine(adding);
    if (!text) return;
    setAdding("");
    const tmp: Todo = { id: `new-${Date.now()}`, text, done: false, position: todos.length };
    run.mutate({ tool: "add_todos", input: { task, items: [text] }, apply: (ts) => [...ts, tmp] });
  };
  if (disabled && todos.length === 0) return null;
  return (
    <section aria-label="Todos" className="mb-6">
      {todos.length > 0 && (
        <ul className="-mx-2 flex flex-col py-0.5">
          {todos.map((td, i) => (
            <TodoRow
              key={td.id}
              td={td}
              disabled={disabled || td.id.startsWith("new-")}
              dragging={dragging === td.id}
              onToggle={() => update(td, { done: !td.done })}
              onRename={(text) => (text ? update(td, { text }) : remove(td))}
              onRemove={() => remove(td)}
              onMove={(to) => update(td, { position: Math.max(0, Math.min(to, todos.length - 1)) })}
              index={i}
              drag={{
                start: () => setDragging(td.id),
                end: () => setDragging(null),
                drop: () => {
                  const from = todos.find((x) => x.id === dragging);
                  if (from && from.id !== td.id) update(from, { position: i });
                  setDragging(null);
                },
              }}
            />
          ))}
        </ul>
      )}
      {!disabled && (
        <form
          className={cn("-mx-2 flex items-center gap-2.5 px-2 py-0.5", textStyle)}
          onSubmit={(e) => { e.preventDefault(); add(); }}
        >
          <Plus className="size-4 shrink-0 text-muted-foreground/50" />
          <input
            ref={addRef}
            value={adding}
            onChange={(e) => setAdding(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Escape" && adding) { e.preventDefault(); e.stopPropagation(); setAdding(""); } }}
            placeholder="Add a todo…"
            aria-label="Add a todo"
            maxLength={300}
            className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground/50"
          />
        </form>
      )}
      <ErrorText error={run.error} />
    </section>
  );
}

function TodoRow({ td, index, disabled, dragging, onToggle, onRename, onRemove, onMove, drag }: {
  td: Todo;
  index: number;
  disabled: boolean;
  dragging: boolean;
  onToggle: () => void;
  onRename: (text: string) => void;
  onRemove: () => void;
  onMove: (to: number) => void;
  drag: { start: () => void; end: () => void; drop: () => void };
}) {
  const [editing, setEditing] = useState(false);
  const [v, setV] = useState(td.text);
  const discard = useRef(false);
  useEffect(() => { if (!editing) setV(td.text); }, [td.text, editing]);
  const commit = () => {
    setEditing(false);
    if (discard.current) { discard.current = false; return; }
    const next = oneLine(v);
    if (next !== td.text) onRename(next);
  };
  // Alt+↑/↓ moves the step, from its checkbox or its text.
  const moveKeys = (e: React.KeyboardEvent) => {
    if (!e.altKey || disabled || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
    e.preventDefault();
    onMove(index + (e.key === "ArrowUp" ? -1 : 1));
  };
  const by = td.done && td.done_by ? `Done by ${td.done_by.name}${td.done_at ? `, ${ago(td.done_at)}` : ""}` : undefined;
  return (
    <li
      draggable={!disabled && !editing}
      onDragStart={(e) => { e.dataTransfer.effectAllowed = "move"; drag.start(); }}
      onDragEnd={drag.end}
      onDragOver={(e) => e.preventDefault()}
      onDrop={(e) => { e.preventDefault(); drag.drop(); }}
      className={cn("group relative flex items-start gap-2.5 px-2 py-0.5", textStyle, dragging && "opacity-40")}
    >
      {!disabled && <GripVertical aria-hidden className="absolute top-[0.45em] -left-3 size-3.5 cursor-grab text-muted-foreground/40 opacity-0 group-hover:opacity-100" />}
      {/* A line box tall, so the box sits on the text's first line like a list marker. */}
      <span className="flex h-[1lh] shrink-0 items-center">
        <button
          type="button"
          role="checkbox"
          aria-checked={td.done}
          aria-label={td.done ? `Mark “${td.text}” not done` : `Mark “${td.text}” done`}
          title={by}
          disabled={disabled}
          onClick={onToggle}
          onKeyDown={moveKeys}
          className={cn(
            "flex size-4 items-center justify-center rounded-[5px] border-[1.5px] transition-colors disabled:cursor-default",
            td.done ? "border-primary bg-primary text-primary-foreground" : "border-foreground/35 hover:border-foreground/70",
          )}
        >
          {td.done && <Check className="size-3" strokeWidth={3} />}
        </button>
      </span>
      <div className="min-w-0 flex-1">
        {/* Editing happens in place: same type, no box, so it reads like typing into the page. */}
        {editing ? (
          <input
            autoFocus
            value={v}
            maxLength={300}
            aria-label="Todo"
            onChange={(e) => setV(e.target.value)}
            onBlur={commit}
            onKeyDown={(e) => {
              if (e.key === "Enter") { e.preventDefault(); e.currentTarget.blur(); }
              if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); discard.current = true; e.currentTarget.blur(); }
            }}
            className="block w-full bg-transparent p-0 text-foreground outline-none"
          />
        ) : (
          <button
            type="button"
            disabled={disabled}
            onClick={() => setEditing(true)}
            onKeyDown={moveKeys}
            className={cn("cursor-text text-left break-words disabled:cursor-default", td.done ? "text-muted-foreground line-through decoration-foreground/25" : "text-foreground/80")}
          >
            {td.text}
          </button>
        )}
        {td.result && !editing && <TodoResult text={td.result} />}
      </div>
      {!disabled && !editing && (
        <button
          type="button"
          onClick={onRemove}
          aria-label={`Remove “${td.text}”`}
          title="Remove"
          className="flex size-5 shrink-0 items-center justify-center rounded text-muted-foreground/60 opacity-0 transition hover:bg-foreground/10 hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100 max-md:opacity-60"
        >
          <X className="size-3.5" />
        </button>
      )}
    </li>
  );
}

// TodoResult is a step's one-line result: one line, the rest on click.
function TodoResult({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  return (
    <button type="button" onClick={() => setOpen((o) => !o)} title={open ? undefined : text} aria-expanded={open} className={cn("block w-full text-left text-[13px] text-muted-foreground hover:text-foreground/80", !open && "truncate")}>
      <span aria-hidden>→ </span><span className="sr-only">result: </span>{text}
    </button>
  );
}
