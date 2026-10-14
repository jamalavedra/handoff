// The chat composer's @ menu: people and agents first, then tasks. Pure helpers here; the menu
// lives in components/mention-menu.tsx.

type Named = { id: string; name: string };
type Linkable = { id: string; title: string };

export type MentionItem<M extends Named = Named, T extends Linkable = Linkable> = { kind: "member"; member: M } | { kind: "task"; task: T };

// matchMembers ranks names starting with the query (or a word in them) above other matches.
// An empty query keeps the given order.
export function matchMembers<M extends Named>(members: M[], query: string, limit = 5): M[] {
  const q = query.trim().toLowerCase();
  if (!q) return members.slice(0, limit);
  const rank = (name: string) => {
    const n = name.toLowerCase();
    if (n.startsWith(q)) return 0;
    if (n.split(/\s+/).some((w) => w.startsWith(q))) return 1;
    return n.includes(q) ? 2 : -1;
  };
  return members
    .map((m, i) => ({ m, i, r: rank(m.name) }))
    .filter((x) => x.r >= 0)
    .sort((a, b) => a.r - b.r || a.i - b.i)
    .slice(0, limit)
    .map((x) => x.m);
}

// mentionItems is the menu's flat order, which keyboard navigation walks. With nothing typed
// the tasks are a short "recent" tail under the members.
export function mentionItems<M extends Named, T extends Linkable>(members: M[], tasks: T[], query: string, exclude?: string): MentionItem<M, T>[] {
  const people = matchMembers(members, query, query.trim() ? 5 : 6);
  const found = tasks.filter((t) => t.id !== exclude).slice(0, query.trim() ? 5 : 3);
  return [...people.map((member) => ({ kind: "member" as const, member })), ...found.map((task) => ({ kind: "task" as const, task }))];
}

// step moves the highlight, wrapping at both ends.
export function step(index: number, delta: number, count: number): number {
  if (count <= 0) return 0;
  return (((index + delta) % count) + count) % count;
}

// mentionText is what picking an item writes: "@Name " or a markdown link to the task.
export function mentionText(item: MentionItem): string {
  if (item.kind === "member") return `@${item.member.name} `;
  const title = item.task.title.replace(/[[\]\\]/g, "\\$&").replace(/\s+/g, " ").trim() || item.task.id;
  // Same path as tabs.taskHref, spelled out so node can run the tests without a bundler.
  return `[${title}](/tasks/${encodeURIComponent(item.task.id)}) `;
}

// replaceQuery swaps the "@partial" between start and the caret for the picked text.
export function replaceQuery(text: string, start: number, caret: number, insert: string): { text: string; caret: number } {
  return { text: text.slice(0, start) + insert + text.slice(caret), caret: start + insert.length };
}

// insertAt drops an "@" at the caret (the composer's @ button), with a space before it when
// it would otherwise run into a word.
export function insertAt(text: string, caret: number): { text: string; caret: number } {
  const at = Math.min(Math.max(caret, 0), text.length);
  const pad = at > 0 && !/\s/.test(text[at - 1]) ? " @" : "@";
  return { text: text.slice(0, at) + pad + text.slice(at), caret: at + pad.length };
}
