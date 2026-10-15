import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Check, ChevronRight, GripVertical, Plus, X } from "lucide-react";
import { ago, api, cn, type Note, type Task } from "../lib";
import { linkify, moveTodo, oneLine, patchTodo, type Todo } from "../todos";
import { markLocal } from "./feedback";
import { ErrorText } from "./app";

// A task reads as its text, then its todos, then the notes written for it.

const textStyle = "text-[15px] leading-relaxed text-foreground/80 sm:text-sm";

// TaskText is the task's own text: plain, a line or two. Click to edit; it saves on leaving the field
// or ⌘/Ctrl+Enter, and Esc puts it back. Addresses in it are links.
export function TaskText({ body, disabled, onSave }: { body: string; disabled: boolean; onSave: (body: string) => Promise<unknown> }) {
  const [editing, setEditing] = useState(false);
  const [v, setV] = useState(body);
  const discard = useRef(false);
  useEffect(() => { if (!editing) setV(body); }, [body, editing]);
  const edit = () => { if (!disabled) setEditing(true); };
  const commit = async () => {
    if (discard.current) { discard.current = false; setV(body); setEditing(false); return; }
    const next = v.trim();
    if (next === body.trim()) return setEditing(false);
    try {
      await onSave(next);
      setEditing(false);
    } catch {
      // the page shows the error; the text stays here to try again
    }
  };
  if (editing) {
    return (
      <textarea
        autoFocus
        aria-label="Task text"
        value={v}
        placeholder="Add details…"
        onChange={(e) => setV(e.target.value)}
        onFocus={(e) => e.currentTarget.setSelectionRange(e.currentTarget.value.length, e.currentTarget.value.length)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) { e.preventDefault(); e.currentTarget.blur(); }
          if (e.key === "Escape") { e.preventDefault(); discard.current = true; e.currentTarget.blur(); }
        }}
        className={cn("-mx-2 block min-h-16 w-[calc(100%+1rem)] resize-none rounded-lg bg-card px-2 py-1 outline-none ring-2 ring-input [field-sizing:content]", textStyle)}
      />
    );
  }
  if (!body) {
    return disabled ? null : (
      <button className="text-left text-sm text-muted-foreground/70 hover:text-foreground/80" onClick={edit}>Add details…</button>
    );
  }
  return (
    <div
      role={disabled ? undefined : "button"}
      tabIndex={disabled ? undefined : 0}
      aria-label={disabled ? undefined : "Edit task text"}
      onClick={(e) => {
        // Selecting text to copy it, or following a link, isn't editing.
        if (window.getSelection()?.toString() || (e.target as HTMLElement).closest("a")) return;
        edit();
      }}
      onKeyDown={(e) => { if (e.key === "Enter" && e.target === e.currentTarget) { e.preventDefault(); edit(); } }}
      className={cn("-mx-2 rounded-lg px-2 py-1 break-words whitespace-pre-wrap", textStyle, !disabled && "cursor-text hover:bg-muted")}
    >
      {linkify(body).map((p, i) => p.url ? (
        <a key={i} href={p.url} target="_blank" rel="noreferrer noopener" className="underline decoration-foreground/30 underline-offset-2 hover:decoration-foreground">{p.text}</a>
      ) : p.text)}
    </div>
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
        <ul className="-mx-2 flex flex-col">
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
          className="-mx-2 flex items-center gap-2.5 rounded-lg px-2 py-1"
          onSubmit={(e) => { e.preventDefault(); add(); }}
        >
          <Plus className="size-4 shrink-0 text-muted-foreground/60" />
          <input
            ref={addRef}
            value={adding}
            onChange={(e) => setAdding(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Escape" && adding) { e.preventDefault(); e.stopPropagation(); setAdding(""); } }}
            placeholder="Add a todo…"
            aria-label="Add a todo"
            maxLength={300}
            className="min-w-0 flex-1 bg-transparent py-0.5 text-[15px] outline-none placeholder:text-muted-foreground/60 sm:text-sm"
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
      className={cn("group relative flex items-start gap-2.5 rounded-lg px-2 py-1.5 hover:bg-muted/60", dragging && "opacity-40")}
    >
      {!disabled && <GripVertical aria-hidden className="absolute top-2 -left-3 size-3.5 cursor-grab text-muted-foreground/40 opacity-0 group-hover:opacity-100" />}
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
          "mt-[3px] flex size-4 shrink-0 items-center justify-center rounded-[5px] border-[1.5px] transition-colors disabled:cursor-default",
          td.done ? "border-primary bg-primary text-primary-foreground" : "border-foreground/40 hover:border-foreground",
        )}
      >
        {td.done && <Check className="size-3" strokeWidth={3} />}
      </button>
      <div className="min-w-0 flex-1 text-[15px] leading-snug sm:text-sm">
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
            className="w-full rounded bg-card px-1 -mx-1 outline-none ring-2 ring-input"
          />
        ) : (
          <button
            type="button"
            disabled={disabled}
            onClick={() => setEditing(true)}
            onKeyDown={moveKeys}
            className={cn("text-left break-words disabled:cursor-default", td.done ? "text-muted-foreground" : "text-foreground/90")}
          >
            {td.text}
          </button>
        )}
        {td.result && !editing && <span className="text-muted-foreground"> <span aria-hidden>→</span><span className="sr-only">result:</span> {td.result}</span>}
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

// NoteCards are the notes written for a task: each opens the note.
export function NoteCards({ notes }: { notes: Note[] }) {
  if (!notes.length) return null;
  return (
    <section aria-label="Notes" className="mb-6 flex flex-col gap-1.5">
      {notes.map((n) => (
        <Link
          key={n.id}
          to="/notes/$id"
          params={{ id: n.id }}
          className="flex min-h-11 items-center gap-2.5 rounded-xl border border-border bg-card px-3 py-2 text-sm transition-colors hover:bg-muted"
        >
          <span aria-hidden>📝</span>
          <span className="min-w-0 flex-1">
            <span className="block truncate font-medium">{n.title}</span>
            <span className="block truncate text-xs text-muted-foreground">{n.project} · updated {ago(n.updated_at)}{n.updated_by ? ` by ${n.updated_by.name}` : ""}</span>
          </span>
          <ChevronRight className="size-4 shrink-0 text-muted-foreground/60" />
        </Link>
      ))}
    </section>
  );
}
