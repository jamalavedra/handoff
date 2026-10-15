// A task is a short plain text plus todos, its steps. Pure helpers for showing and editing them.

export type Todo = { id: string; text: string; done: boolean; result?: string; done_by?: { id: string; name: string; kind: "human" | "agent" }; done_at?: string; position: number };

export type TodoProgress = { done: number; total: number };

// progressLabel is the "2/5" a task row shows, or "" when it has no todos.
export function progressLabel(p?: TodoProgress) {
  return p && p.total > 0 ? `${p.done}/${p.total}` : "";
}

export type TextPart = { text: string; url?: string };

// linkify splits plain text into runs, with web addresses as links. Trailing punctuation stays text.
export function linkify(text: string): TextPart[] {
  const out: TextPart[] = [];
  let at = 0;
  for (const m of text.matchAll(/https?:\/\/[^\s<>"']+/g)) {
    let url = m[0];
    const tail = url.match(/[.,;:!?)\]]+$/)?.[0] ?? "";
    // A closing paren that opened inside the address belongs to it (wikipedia.org/wiki/X_(y)).
    const keep = tail.startsWith(")") && url.slice(0, -tail.length).includes("(") ? 1 : 0;
    url = url.slice(0, url.length - tail.length + keep);
    if (m.index > at) out.push({ text: text.slice(at, m.index) });
    out.push({ text: url, url });
    at = m.index + url.length;
  }
  if (at < text.length) out.push({ text: text.slice(at) });
  return out;
}

// oneLine folds what's typed or pasted into a todo onto one line.
export const oneLine = (s: string) => s.replace(/\s+/g, " ").trim();

// patchTodo changes one todo in a list, for optimistic updates.
export function patchTodo(todos: Todo[], id: string, patch: Partial<Todo>): Todo[] {
  return todos.map((t) => (t.id === id ? { ...t, ...patch } : t));
}

// moveTodo puts a todo at index `to` (clamped), renumbering positions like the server does.
export function moveTodo(todos: Todo[], id: string, to: number): Todo[] {
  const t = todos.find((x) => x.id === id);
  if (!t) return todos;
  const rest = todos.filter((x) => x.id !== id);
  const at = Math.min(Math.max(to, 0), rest.length);
  return [...rest.slice(0, at), t, ...rest.slice(at)].map((x, i) => ({ ...x, position: i }));
}
