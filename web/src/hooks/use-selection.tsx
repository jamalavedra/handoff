import { createContext, useContext, useEffect, useRef, useState } from "react";
import { useRouterState } from "@tanstack/react-router";
import { add, all, box, none, ordered, range, toggle, touched, type Box, type Selection } from "../selection";
import { rows, typing } from "../components/shortcuts";

type Ctx = { sel: Selection; set: (s: Selection) => void };
const SelectionContext = createContext<Ctx>({ sel: none, set: () => {} });

// The ids of the task rows on screen, in the order they show.
const visible = () => rows().map((el) => el.dataset.taskId!).filter(Boolean);

// Pointer-downs here don't start a marquee: they belong to a control or a row.
const notEmpty = "a, button, input, textarea, select, label, [role], [contenteditable], [data-task-row]";

// SelectionProvider holds the selected tasks for the page on screen. ⌘A selects every row, Escape
// clears, and dragging over empty space draws a marquee. Moving to another page clears it.
export function SelectionProvider({ children }: { children: React.ReactNode }) {
  const [sel, set] = useState(none);
  const [marquee, setMarquee] = useState<Box | null>(null);
  const latest = useRef(sel);
  latest.current = sel;
  const path = useRouterState({ select: (s) => s.location.pathname });
  useEffect(() => set(none), [path]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || typing(e.target) || document.querySelector("[role=dialog], [role=menu]")) return;
      if (e.key === "Escape" && latest.current.ids.length) return set(none);
      if (e.key !== "a" || !(e.metaKey || e.ctrlKey) || e.shiftKey || e.altKey) return;
      // Only when the list has focus (or nothing does), so ⌘A still selects text elsewhere.
      const at = document.activeElement;
      if (at && at !== document.body && !at.closest("[data-task-row]")) return;
      const order = visible();
      if (!order.length) return;
      e.preventDefault();
      set(all(order));
    };
    const onDown = (e: PointerEvent) => {
      const t = e.target as HTMLElement;
      if (e.button !== 0 || e.pointerType !== "mouse" || !t.closest("main") || t.closest(notEmpty)) return;
      const keep = e.metaKey || e.ctrlKey || e.shiftKey;
      const base = keep ? latest.current : none;
      // shortcut: row positions are read once, so scrolling mid-drag misses rows; fine for one screen.
      const rects = rows().map((el) => ({ id: el.dataset.taskId!, rect: el.getBoundingClientRect() }));
      const [x0, y0] = [e.clientX, e.clientY];
      let started = false;
      const move = (m: PointerEvent) => {
        if (!started && Math.hypot(m.clientX - x0, m.clientY - y0) < 4) return;
        if (!started) document.getSelection()?.removeAllRanges();
        started = true;
        const b = box(x0, y0, m.clientX, m.clientY);
        setMarquee(b);
        set(add(base, touched(rects, b)));
      };
      const up = () => {
        window.removeEventListener("pointermove", move);
        window.removeEventListener("pointerup", up);
        document.body.style.userSelect = "";
        setMarquee(null);
        // A plain click on empty space clears the selection.
        if (!started && !keep && latest.current.ids.length) set(none);
      };
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", move);
      window.addEventListener("pointerup", up);
    };
    window.addEventListener("keydown", onKey);
    window.addEventListener("pointerdown", onDown);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("pointerdown", onDown);
    };
  }, []);
  return (
    <SelectionContext.Provider value={{ sel, set }}>
      {children}
      {marquee && <div aria-hidden className="pointer-events-none fixed z-50 rounded-sm border border-blue-400 bg-blue-400/10" style={{ left: marquee.left, top: marquee.top, width: marquee.right - marquee.left, height: marquee.bottom - marquee.top }} />}
    </SelectionContext.Provider>
  );
}

export const taskMime = "application/x-handoff-task";

// useSelection is the page's selection, plus the props a task row spreads to take part in it:
// ⌘/Ctrl-click toggles, Shift-click selects a range, and dragging a selected row drags them all.
export function useSelection() {
  const { sel, set } = useContext(SelectionContext);
  const row = (id: string, draggable = true) => ({
    "data-task-id": id,
    "data-selected": sel.ids.includes(id) ? "" : undefined,
    // Shift-click would otherwise select the text between the clicks.
    onMouseDownCapture: (e: React.MouseEvent) => { if (e.shiftKey) e.preventDefault(); },
    onClickCapture: (e: React.MouseEvent) => {
      if (!(e.metaKey || e.ctrlKey || e.shiftKey)) return;
      e.preventDefault();
      e.stopPropagation();
      set(e.shiftKey ? range(sel, visible(), id) : toggle(sel, id));
    },
    draggable,
    onDragStart: draggable ? (e: React.DragEvent) => {
      const ids = sel.ids.includes(id) ? ordered(sel, visible()) : [id];
      e.dataTransfer.setData(taskMime, JSON.stringify(ids));
      e.dataTransfer.effectAllowed = "move";
      if (ids.length > 1) stackImage(e, ids.length);
    } : undefined,
  });
  return { ids: sel.ids, clear: () => set(none), row };
}

// A stack of cards with the count, as the drag image for several tasks.
function stackImage(e: React.DragEvent, n: number) {
  const el = document.createElement("div");
  el.textContent = `${n} tasks`;
  el.className = "fixed -top-96 left-0 rounded-xl border border-border bg-card px-3 py-2 text-sm font-medium text-foreground";
  el.style.boxShadow = "4px 4px 0 -1px var(--card), 4px 4px 0 0 var(--border), 8px 8px 0 -1px var(--card), 8px 8px 0 0 var(--border)";
  document.body.append(el);
  e.dataTransfer.setDragImage(el, 20, 20);
  setTimeout(() => el.remove());
}

// useTaskDrop makes an element a drop target for dragged tasks (board columns, sidebar projects).
export function useTaskDrop(onDrop: (ids: string[]) => void) {
  const [over, setOver] = useState(false);
  const props = {
    onDragOver: (e: React.DragEvent) => {
      if (!e.dataTransfer.types.includes(taskMime)) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      setOver(true);
    },
    onDragLeave: (e: React.DragEvent) => { if (!e.currentTarget.contains(e.relatedTarget as Node)) setOver(false); },
    onDrop: (e: React.DragEvent) => {
      e.preventDefault();
      setOver(false);
      try {
        const ids: unknown = JSON.parse(e.dataTransfer.getData(taskMime));
        if (Array.isArray(ids)) onDrop(ids.filter((x): x is string => typeof x === "string"));
      } catch {
        // not a task drag
      }
    },
  };
  return { over, props };
}
