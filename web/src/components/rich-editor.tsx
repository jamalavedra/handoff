import { useEffect, useRef, useState } from "react";
import { EditorContent, useEditor, useEditorState, type Editor, type UseEditorOptions } from "@tiptap/react";
import { Bold, Code, Heading2, Italic, Link2, List, ListChecks, ListOrdered, SquareCode, TextQuote, Unlink } from "lucide-react";
import { Selection, TextSelection } from "@tiptap/pm/state";
import { Button } from "@/components/ui/button";
import { cn } from "../lib";
import { richExtensions, toMarkdown } from "../richtext";

const mac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
const mod = mac ? "⌘" : "Ctrl+";

export type Caret = { x: number; y: number } | "end";

// RichEditor edits a task body in place, formatted as it reads, and hands back markdown.
// ⌘/Ctrl+Enter saves, Esc cancels, and leaving the editor (a click elsewhere, Tab away) saves if
// anything changed. Switching windows doesn't count as leaving. It's loaded on demand by BodyEditor.
export default function RichEditor({ body, changed, placeholder, caret, onSave, onClose }: {
  body: string;
  changed: boolean; // someone else saved the body since editing began
  placeholder: string;
  caret: Caret;
  onSave: (body: string) => Promise<unknown>;
  onClose: (saved?: string) => void;
}) {
  const wrap = useRef<HTMLDivElement>(null);
  const linkInput = useRef<HTMLInputElement>(null);
  const [link, setLink] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const base = useRef<string | null>(null);
  const done = useRef(false);
  // Keyboard handlers are created once with the editor, so they call through this ref.
  const act = useRef({ save: () => {}, cancel: () => {}, link: () => {}, leave: () => {} });

  // Options are made once: useEditor compares them on every render and re-applies them (resetting the
  // view) when anything, like a fresh extensions array, looks different.
  const [options] = useState((): UseEditorOptions => ({
    immediatelyRender: true,
    shouldRerenderOnTransaction: false,
    extensions: richExtensions(placeholder),
    content: body,
    editorProps: {
      attributes: { class: "md min-h-24 outline-none", role: "textbox", "aria-multiline": "true", "aria-label": "Details" },
      // These keys are the editor's alone: the window-level ones (⌘K menu, Esc closing the panel) stand down.
      handleKeyDown: (_view, e) => {
        const cmd = e.metaKey || e.ctrlKey;
        const run = e.key === "Enter" && cmd ? act.current.save : e.key === "Escape" ? act.current.cancel : e.key.toLowerCase() === "k" && e.shiftKey && cmd ? act.current.link : null;
        if (!run) return false;
        e.stopPropagation();
        run();
        return true;
      },
    },
  }));
  const editor = useEditor({ ...options, immediatelyRender: true });
  // What "unchanged" means: the body as the editor writes it, so opening and closing never rewrites
  // an agent's markdown.
  if (base.current === null) base.current = toMarkdown(editor);

  const save = async () => {
    if (done.current || saving) return;
    const md = toMarkdown(editor);
    if (md === base.current) return close();
    setSaving(true);
    try {
      await onSave(md);
      close(md);
    } catch {
      setSaving(false); // the page shows the error; keep the draft
    }
  };
  const close = (saved?: string) => { done.current = true; onClose(saved); };
  const openLink = () => setLink(editor.getAttributes("link").href ?? "");
  act.current = { save, cancel: () => close(), link: openLink, leave: () => leave() };

  // Put the caret where the read view was clicked (the layouts match), or at the end.
  // A frame later, once mounted and laid out. Focus first, then select from the state as it is after
  // focusing (focusing can itself dispatch).
  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      if (editor.isDestroyed) return;
      const { view } = editor;
      const r = view.dom.getBoundingClientRect();
      const at = caret === "end" ? null : view.posAtCoords({ left: r.left + caret.x, top: r.top + caret.y });
      view.focus();
      const doc = view.state.doc;
      view.dispatch(view.state.tr.setSelection(at ? TextSelection.near(doc.resolve(Math.min(at.pos, doc.content.size))) : Selection.atEnd(doc)));
    });
    return () => cancelAnimationFrame(frame);
  }, [editor]); // eslint-disable-line react-hooks/exhaustive-deps

  // Leaving with unsaved changes (say the panel switches tasks) still saves them.
  useEffect(() => () => {
    if (done.current) return;
    try {
      const md = toMarkdown(editor);
      if (md !== base.current) onSave(md).catch(() => {});
    } catch { /* editor already gone */ }
  }, [editor]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => { if (link !== null) linkInput.current?.focus(); }, [link !== null]); // eslint-disable-line react-hooks/exhaustive-deps

  // Leaving = pressing somewhere outside the editor, or tabbing to another control. A blur with no new
  // focus target (switching windows, the editor re-mounting) doesn't count.
  const leave = () => { setLink(null); save(); };
  const outside = (n: EventTarget | null) => n instanceof Node && n !== document.documentElement && !wrap.current?.contains(n);
  useEffect(() => {
    const down = (e: PointerEvent) => { if (outside(e.target)) act.current.leave(); };
    document.addEventListener("pointerdown", down, true);
    return () => document.removeEventListener("pointerdown", down, true);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const applyLink = () => {
    const href = (link ?? "").trim();
    const chain = editor.chain().focus().extendMarkRange("link");
    if (!href) chain.unsetLink().run();
    else {
      const url = /^[a-z][\w+.-]*:/i.test(href) || href.startsWith("/") || href.startsWith("#") ? href : `https://${href}`;
      if (editor.state.selection.empty && !editor.isActive("link")) chain.insertContent({ type: "text", text: href, marks: [{ type: "link", attrs: { href: url } }] }).run();
      else chain.setLink({ href: url }).run();
    }
    setLink(null);
  };

  return (
    <div ref={wrap} onBlur={(e) => { if (outside(e.relatedTarget)) leave(); }} className="-mx-2 rounded-lg px-2 pb-2 ring-1 ring-border focus-within:ring-foreground/25">
      <div role="toolbar" aria-label="Formatting" className="sticky top-0 z-10 -mx-2 mb-1 flex items-center gap-0.5 overflow-x-auto rounded-t-lg border-b border-border bg-background/95 px-1.5 py-1 backdrop-blur [scrollbar-width:none]">
        {link === null ? <Tools editor={editor} onLink={openLink} /> : (
          <div className="flex min-w-0 flex-1 items-center gap-1">
            <Link2 className="ml-1 size-3.5 shrink-0 text-muted-foreground" />
            <input
              ref={linkInput}
              value={link}
              onChange={(e) => setLink(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") { e.preventDefault(); applyLink(); }
                if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); setLink(null); editor.commands.focus(); }
              }}
              placeholder="Paste or type a link"
              aria-label="Link address"
              className="h-7 min-w-0 flex-1 bg-transparent px-1 text-[13px] outline-none placeholder:text-muted-foreground/70"
            />
            {editor.isActive("link") && <Tool label="Remove link" icon={Unlink} onClick={() => { editor.chain().focus().extendMarkRange("link").unsetLink().run(); setLink(null); }} />}
            <Button type="button" size="sm" variant="ghost" className="h-7 px-2.5 text-[13px]" onMouseDown={(e) => e.preventDefault()} onClick={applyLink}>Apply</Button>
          </div>
        )}
      </div>
      <EditorContent editor={editor} className="text-[15px] leading-relaxed text-foreground/80 sm:text-sm" />
      <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
        <span className="min-w-0 flex-1 truncate">
          {changed ? <span className="text-amber-700 dark:text-amber-400">Updated by someone else meanwhile; saving replaces it.</span> : <span className="hidden [@media(pointer:fine)]:inline">{mod}↵ to save · Esc to cancel</span>}
        </span>
        <Button type="button" size="sm" variant="ghost" className="h-7 px-2.5 text-[13px]" onMouseDown={(e) => e.preventDefault()} onClick={() => close()}>Cancel</Button>
        <Button type="button" size="sm" className="h-7 px-3 text-[13px]" disabled={saving} onMouseDown={(e) => e.preventDefault()} onClick={save}>Save</Button>
      </div>
    </div>
  );
}

function Tools({ editor, onLink }: { editor: Editor; onLink: () => void }) {
  const on = useEditorState({
    editor,
    selector: ({ editor: e }) => ({
      bold: e.isActive("bold"),
      italic: e.isActive("italic"),
      heading: e.isActive("heading"),
      bullets: e.isActive("bulletList"),
      numbers: e.isActive("orderedList"),
      tasks: e.isActive("taskList"),
      link: e.isActive("link"),
      code: e.isActive("code"),
      block: e.isActive("codeBlock"),
      quote: e.isActive("blockquote"),
    }),
  });
  const run = () => editor.chain().focus();
  return (
    <>
      <Tool label={`Bold (${mod}B)`} icon={Bold} active={on.bold} onClick={() => run().toggleBold().run()} />
      <Tool label={`Italic (${mod}I)`} icon={Italic} active={on.italic} onClick={() => run().toggleItalic().run()} />
      <Tool label="Heading (## )" icon={Heading2} active={on.heading} onClick={() => (on.heading && !editor.isActive("heading", { level: 2 }) ? run().setHeading({ level: 2 }).run() : run().toggleHeading({ level: 2 }).run())} />
      <span className="mx-1 h-4 w-px shrink-0 bg-border" />
      <Tool label="Bulleted list (- )" icon={List} active={on.bullets} onClick={() => run().toggleBulletList().run()} />
      <Tool label="Numbered list (1. )" icon={ListOrdered} active={on.numbers} onClick={() => run().toggleOrderedList().run()} />
      <Tool label="Checklist ([] )" icon={ListChecks} active={on.tasks} onClick={() => run().toggleTaskList().run()} />
      <span className="mx-1 h-4 w-px shrink-0 bg-border" />
      <Tool label={`Link (${mod}⇧K)`} icon={Link2} active={on.link} onClick={onLink} />
      <Tool label={`Code (${mod}E)`} icon={Code} active={on.code} onClick={() => run().toggleCode().run()} />
      <Tool label="Code block (```)" icon={SquareCode} active={on.block} onClick={() => run().toggleCodeBlock().run()} />
      <Tool label="Quote (> )" icon={TextQuote} active={on.quote} onClick={() => run().toggleBlockquote().run()} />
    </>
  );
}

function Tool({ label, icon: Icon, active, onClick }: { label: string; icon: typeof Bold; active?: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      aria-pressed={active}
      // Keep focus (and the selection) in the editor.
      onMouseDown={(e) => e.preventDefault()}
      onClick={onClick}
      className={cn("flex size-7 shrink-0 items-center justify-center rounded-full text-muted-foreground hover:bg-accent hover:text-foreground", active && "bg-accent text-foreground")}
    >
      <Icon className="size-3.5" />
    </button>
  );
}
