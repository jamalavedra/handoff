import { createContext, useContext, useEffect, useState, type ReactNode, type RefObject } from "react";
import { Reply, X } from "lucide-react";
import type { Comment, ReplyRef } from "../lib";
import { messageDomId, selected, type ReplyDraft } from "../reply";

// Replying to a message, or to one part of it, in a task's chat. TaskChat provides what starting a
// reply does; outside a chat (or in the trash) nothing here shows.

export const ReplyContext = createContext<((c: Comment, quote?: string) => void) | null>(null);

// ReplyTarget wraps a message: where a quoted header jumps to, and what a selection is matched to.
export function ReplyTarget({ c, children }: { c: Comment; children: ReactNode }) {
  return (
    <div id={messageDomId(c.id)} data-reply-id={c.id} className="group/msg -mx-2 rounded-2xl px-2 transition-colors duration-700 data-[flash]:bg-primary/10">
      {children}
    </div>
  );
}

// ReplyAction sits with a message's reactions: shown on hover, always on touch screens.
export function ReplyAction({ c }: { c: Comment }) {
  const reply = useContext(ReplyContext);
  if (!reply) return null;
  return (
    <button
      type="button"
      aria-label="Reply"
      title="Reply"
      onClick={() => reply(c)}
      className="flex size-6 items-center justify-center rounded-full text-muted-foreground/70 opacity-0 transition hover:bg-muted hover:text-foreground focus-visible:opacity-100 group-hover/msg:opacity-100 pointer-coarse:opacity-100"
    >
      <Reply className="size-3.5" />
    </button>
  );
}

const flashes = new WeakMap<HTMLElement, number>();

// jumpTo scrolls to a message and briefly highlights it.
function jumpTo(id: string) {
  const el = document.getElementById(messageDomId(id));
  if (!el) return;
  const still = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  el.scrollIntoView({ block: "center", behavior: still ? "auto" : "smooth" });
  el.dataset.flash = "";
  clearTimeout(flashes.get(el));
  flashes.set(el, window.setTimeout(() => delete el.dataset.flash, 1600));
}

// QuotedReply is the header of a reply: who it answers and the part it's about.
export function QuotedReply({ r }: { r?: ReplyRef }) {
  if (!r) return null;
  return (
    <button
      type="button"
      onClick={() => jumpTo(r.id)}
      title="Show the original message"
      className="mb-1 block w-full border-l-2 border-foreground/20 pl-2 text-left text-xs text-muted-foreground transition-colors hover:border-foreground/40 hover:text-foreground"
    >
      <span className="line-clamp-2">
        <span className="font-medium">{r.author?.name ?? "Someone"}</span>: {r.quoted ? `“${r.excerpt}”` : r.excerpt}
      </span>
    </button>
  );
}

// ReplyChip sits above the composer while writing a reply.
export function ReplyChip({ draft, onCancel }: { draft: ReplyDraft; onCancel: () => void }) {
  return (
    <div className="flex w-full min-w-0 basis-full items-center gap-1.5 border-l-2 border-primary/40 py-0.5 pl-2 text-xs text-muted-foreground">
      <Reply className="size-3.5 shrink-0" />
      <span className="min-w-0 flex-1 truncate">
        Replying to <span className="font-medium text-foreground">{draft.author ?? "message"}</span>: “{draft.quote ?? draft.excerpt}”
      </span>
      <button type="button" onClick={onCancel} aria-label="Cancel reply" title="Cancel reply (Esc)" className="flex size-6 shrink-0 items-center justify-center rounded-full hover:bg-muted hover:text-foreground">
        <X className="size-3.5" />
      </button>
    </div>
  );
}

type Spot = { id: string; quote: string; top: number; left: number };

// SelectionReply offers "Reply to this" over text selected with the mouse inside one message. It
// lives in the scrolling thread (which must be positioned), so it scrolls with the text.
export function SelectionReply({ scroller, comments }: { scroller: RefObject<HTMLDivElement | null>; comments: Comment[] }) {
  const reply = useContext(ReplyContext);
  const [spot, setSpot] = useState<Spot | null>(null);
  useEffect(() => {
    const el = scroller.current;
    if (!el || !reply) return;
    // Only a message's own text can be quoted: not its quoted header, names or reactions.
    const bodyOf = (n: Node | null) => (n instanceof Element ? n : n?.parentElement)?.closest<HTMLElement>(".md") ?? null;
    const msgOf = (b: HTMLElement | null) => b?.closest<HTMLElement>("[data-reply-id]")?.dataset.replyId ?? null;
    let frame = 0;
    const check = (e: PointerEvent) => {
      if (e.pointerType !== "mouse") return;
      cancelAnimationFrame(frame);
      // Once the browser has settled the selection.
      frame = requestAnimationFrame(() => {
        const sel = window.getSelection();
        if (!sel || sel.isCollapsed || !sel.rangeCount || !el.contains(sel.anchorNode)) return setSpot(null);
        const body = bodyOf(sel.anchorNode);
        const s = body && body === bodyOf(sel.focusNode) ? selected(msgOf(body), msgOf(body), sel.toString()) : null;
        if (!s) return setSpot(null);
        const r = sel.getRangeAt(0).getBoundingClientRect();
        const box = el.getBoundingClientRect();
        const above = r.top - box.top > 40;
        setSpot({
          ...s,
          top: (above ? r.top - 34 : r.bottom + 6) - box.top + el.scrollTop,
          left: Math.min(Math.max(r.left + r.width / 2 - box.left, 64), box.width - 64),
        });
      });
    };
    const clear = () => {
      const sel = window.getSelection();
      if (!sel || sel.isCollapsed) setSpot(null);
    };
    document.addEventListener("pointerup", check);
    document.addEventListener("selectionchange", clear);
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener("pointerup", check);
      document.removeEventListener("selectionchange", clear);
    };
  }, [scroller, reply]);
  const c = spot && comments.find((x) => x.id === spot.id);
  if (!spot || !c || !reply) return null;
  return (
    <button
      type="button"
      style={{ top: spot.top, left: spot.left }}
      // Keep the selection while clicking.
      onPointerDown={(e) => e.preventDefault()}
      onClick={() => {
        reply(c, spot.quote);
        window.getSelection()?.removeAllRanges();
        setSpot(null);
      }}
      className="absolute z-20 flex h-7 -translate-x-1/2 items-center gap-1 rounded-full border border-border bg-popover px-2.5 text-xs font-medium text-popover-foreground shadow-md hover:bg-accent"
    >
      <Reply className="size-3.5" />Reply to this
    </button>
  );
}
