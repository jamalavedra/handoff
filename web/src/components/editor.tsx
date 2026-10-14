import { lazy, Suspense, useRef, useState } from "react";
import { cn, type Task } from "../lib";
import { Markdown } from "./markdown";
import type { Caret } from "./rich-editor";

// The editor (Tiptap/ProseMirror) is a sizeable chunk; load it when someone heads for the details.
const loadEditor = () => import("./rich-editor");
const RichEditor = lazy(loadEditor);

const readStyle = "text-[15px] leading-relaxed text-foreground/80 sm:text-sm";

// BodyEditor shows a task's details (its document: agents write results here) and edits them in
// place: click into the text and it becomes a rich editor at that spot, saved back as markdown.
export function BodyEditor({ t, onSave, disabled }: { t: Task; onSave: (body: string) => Promise<unknown>; disabled: boolean }) {
  const body = t.body ?? "";
  const [editing, setEditing] = useState<{ caret: Caret; base: string } | null>(null);
  // Just-saved text, shown until the refetched task catches up.
  const [saved, setSaved] = useState<{ text: string; over: string } | null>(null);
  const view = useRef<HTMLDivElement>(null);
  const shown = saved && saved.over === body ? saved.text : body;
  const empty = t.assignee?.kind === "agent" ? `${t.assignee.name}'s work on this will show up here. Add details…` : "Add details…";

  const edit = (caret: Caret) => { if (!disabled) setEditing({ caret, base: shown }); };

  if (editing) {
    const fallback = <div className={cn("-mx-2 rounded-lg px-2 py-1", readStyle)}><Markdown text={editing.base} /></div>;
    return (
      <Suspense fallback={fallback}>
        <RichEditor
          body={editing.base}
          changed={body !== editing.base && !(saved && saved.over === body)}
          placeholder="Write, or type ## for a heading, - for a list, [] for a checklist…"
          caret={editing.caret}
          onSave={onSave}
          onClose={(text) => { if (text !== undefined) setSaved({ text, over: body }); setEditing(null); }}
        />
      </Suspense>
    );
  }
  if (!shown) return disabled ? null : (
    <button className="text-left text-sm text-muted-foreground/70 hover:text-foreground/80" onPointerEnter={loadEditor} onFocus={loadEditor} onClick={() => edit("end")}>
      {empty}
    </button>
  );
  return (
    <div
      ref={view}
      role={disabled ? undefined : "button"}
      tabIndex={disabled ? undefined : 0}
      aria-label={disabled ? undefined : "Edit details"}
      onPointerEnter={disabled ? undefined : loadEditor}
      onFocus={disabled ? undefined : loadEditor}
      onClick={(e) => {
        // Selecting text to copy it, or following a link, isn't editing.
        if (window.getSelection()?.toString() || (e.target as HTMLElement).closest("a")) return;
        const r = view.current!.getBoundingClientRect();
        edit(e.detail ? { x: e.clientX - r.left - 8, y: e.clientY - r.top - 4 } : "end");
      }}
      onKeyDown={(e) => { if (e.key === "Enter" && e.target === e.currentTarget) { e.preventDefault(); edit("end"); } }}
      className={cn("-mx-2 rounded-lg px-2 py-1", readStyle, !disabled && "cursor-text hover:bg-muted")}
    >
      <Markdown text={shown} />
    </div>
  );
}
