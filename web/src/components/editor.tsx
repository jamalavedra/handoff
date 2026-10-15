import { lazy, Suspense, useRef, useState } from "react";
import { toast } from "sonner";
import { cn, localDrafts } from "../lib";
import { Markdown } from "./markdown";
import type { Caret } from "./rich-editor";

// The editor (Tiptap/ProseMirror) is a sizeable chunk; load it when someone heads for the note.
const loadEditor = () => import("./rich-editor");
const RichEditor = lazy(loadEditor);

const readStyle = "text-[15px] leading-relaxed text-foreground/80 sm:text-sm";

// A draft whose save failed after its editor was gone waits here, per document, until it's restored
// and saved or discarded.
const drafts = localDrafts("handoff-draft:");

// DocEditor shows a markdown document (a note: agents write their longer work here) and edits it in
// place: click into the text and it becomes a rich editor at that spot, saved back as markdown.
// Key it by document: a draft stays with the document it was written for.
export function DocEditor({ id, body, name, empty, onSave, disabled }: { id: string; body: string; name: string; empty: string; onSave: (body: string) => Promise<unknown>; disabled: boolean }) {
  const [editing, setEditing] = useState<{ caret: Caret; base: string; draft?: string } | null>(null);
  const [lost, setLost] = useState(() => drafts.get(id));
  // Just-saved text, shown until the refetched task catches up.
  const [saved, setSaved] = useState<{ text: string; over: string } | null>(null);
  const view = useRef<HTMLDivElement>(null);
  const shown = saved && saved.over === body ? saved.text : body;

  const edit = (caret: Caret) => { if (!disabled) setEditing({ caret, base: shown }); };

  if (editing) {
    const fallback = <div className={cn("-mx-2 rounded-lg px-2 py-1", readStyle)}><Markdown text={editing.base} /></div>;
    return (
      <Suspense fallback={fallback}>
        <RichEditor
          body={editing.base}
          draft={editing.draft}
          changed={body !== editing.base && !(saved && saved.over === body)}
          placeholder="Write, or type ## for a heading, - for a list, [] for a checklist…"
          caret={editing.caret}
          onSave={(md) => onSave(md).then(() => { drafts.drop(id); setLost(null); })}
          onSaved={(text) => { setSaved({ text, over: body }); setEditing((e) => e && { ...e, base: text, draft: undefined }); }}
          onLost={(md) => { drafts.set(id, md); toast.error(`Couldn't save your edits to “${name}”. Open it again to restore them.`); }}
          onClose={() => setEditing(null)}
        />
      </Suspense>
    );
  }
  const recover = lost !== null && !disabled && (
    <div className="mb-2 flex items-center gap-3 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-900">
      <span className="min-w-0 flex-1">Your last edits here didn't save.</span>
      <button className="text-xs font-medium hover:underline" onClick={() => setEditing({ caret: "end", base: shown, draft: lost })}>Restore</button>
      <button className="text-xs text-amber-700/70 hover:text-red-600" onClick={() => { drafts.drop(id); setLost(null); }}>Discard</button>
    </div>
  );
  if (!shown) return disabled ? null : (
    <>{recover}<button className="text-left text-sm text-muted-foreground/70 hover:text-foreground/80" onPointerEnter={loadEditor} onFocus={loadEditor} onClick={() => edit("end")}>
      {empty}
    </button></>
  );
  return (
    <>{recover}<div
      ref={view}
      role={disabled ? undefined : "button"}
      tabIndex={disabled ? undefined : 0}
      aria-label={disabled ? undefined : "Edit note"}
      onPointerEnter={disabled ? undefined : loadEditor}
      onFocus={disabled ? undefined : loadEditor}
      onClick={(e) => {
        // Selecting text to copy it, or following a link, isn't editing.
        if (window.getSelection()?.toString() || (e.target as HTMLElement).closest("a, input")) return;
        const r = view.current!.getBoundingClientRect();
        edit(e.detail ? { x: e.clientX - r.left - 8, y: e.clientY - r.top - 4 } : "end");
      }}
      onKeyDown={(e) => { if (e.key === "Enter" && e.target === e.currentTarget) { e.preventDefault(); edit("end"); } }}
      className={cn("-mx-2 rounded-lg px-2 py-1", readStyle, !disabled && "cursor-text hover:bg-muted")}
    >
      <Markdown text={shown} />
    </div></>
  );
}
