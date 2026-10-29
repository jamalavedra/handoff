// A selection of tasks in a list or on the board. `order` is always the visible order of the rows, and
// the anchor is where a Shift-click range starts.
export type Selection = { ids: string[]; anchor?: string };

export const none: Selection = { ids: [] };

// toggle adds or removes one task (⌘-click) and moves the anchor to it.
export function toggle(s: Selection, id: string): Selection {
  return { ids: s.ids.includes(id) ? s.ids.filter((x) => x !== id) : [...s.ids, id], anchor: id };
}

// range selects from the anchor to `id` (Shift-click), replacing what was selected. With no anchor on
// screen it selects just `id`.
export function range(s: Selection, order: string[], id: string): Selection {
  const to = order.indexOf(id);
  if (to < 0) return s;
  const from = s.anchor ? order.indexOf(s.anchor) : -1;
  if (from < 0) return { ids: [id], anchor: id };
  return { ids: order.slice(Math.min(from, to), Math.max(from, to) + 1), anchor: s.anchor };
}

export function all(order: string[]): Selection {
  return { ids: [...order], anchor: order[0] };
}

// add joins ids to a selection without repeats (a marquee drawn with ⌘ or Shift held).
export function add(s: Selection, ids: string[]): Selection {
  return { ids: [...s.ids, ...ids.filter((id) => !s.ids.includes(id))], anchor: s.anchor ?? ids[0] };
}

// ordered lists the selected ids in visible order, dropping any no longer on screen.
export function ordered(s: Selection, order: string[]): string[] {
  return order.filter((id) => s.ids.includes(id));
}

export type Box = { left: number; top: number; right: number; bottom: number };

// box is the rectangle between two pointer positions, dragged in any direction.
export function box(x0: number, y0: number, x1: number, y1: number): Box {
  return { left: Math.min(x0, x1), top: Math.min(y0, y1), right: Math.max(x0, x1), bottom: Math.max(y0, y1) };
}

// touched lists the rows a marquee overlaps.
export function touched(rows: { id: string; rect: Box }[], b: Box): string[] {
  return rows.filter(({ rect: r }) => r.left < b.right && r.right > b.left && r.top < b.bottom && r.bottom > b.top).map((r) => r.id);
}
