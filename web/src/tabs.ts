// Desktop tabs: a browser-style strip of open pages and tasks. Pure state here; the strip lives in components/tabs.tsx.

// A tab is a location (pathname + search). `opener` is the tab it was opened from, so a run of
// background tabs opened from one place lines up in order, like a browser.
export type Tab = { id: string; href: string; opener?: string };
export type Tabs = { tabs: Tab[]; active: string };

export const home = "/";

// What a tab is named after: a task page, a project, a note, or another page.
export type Target = { kind: "task"; id: string } | { kind: "project"; id: string } | { kind: "note"; id: string } | { kind: "page"; path: string };

const decode = (s: string) => {
  try {
    return decodeURIComponent(s);
  } catch {
    return s;
  }
};

export function target(href: string): Target {
  const path = href.split(/[?#]/)[0];
  const task = path.match(/^\/tasks\/([^/]+)/)?.[1];
  if (task) return { kind: "task", id: decode(task) };
  const project = path.match(/^\/projects\/([^/]+)/)?.[1];
  if (project) return { kind: "project", id: decode(project) };
  const note = path.match(/^\/notes\/([^/]+)/)?.[1];
  if (note) return { kind: "note", id: decode(note) };
  return { kind: "page", path };
}

export const taskHref = (id: string) => `/tasks/${encodeURIComponent(id)}`;

// tabHref is what a tab holds for a location. Tabs are full pages, so a task open in the side
// panel (?task=) is held as its own task page, and selecting the tab opens it full size.
export function tabHref(href: string): string {
  // On a task page the page itself wins over a stray ?task=.
  if (target(href).kind === "task") return href;
  const q = href.indexOf("?");
  const id = q < 0 ? null : new URLSearchParams(href.slice(q + 1).split("#")[0]).get("task");
  return id ? taskHref(id) : href;
}

const activeTab = (s: Tabs) => s.tabs.find((t) => t.id === s.active);

// visit follows the router. Normally the active tab takes the new location; going back or forward
// onto a location another tab holds switches to that tab instead, so Back undoes a tab switch.
export function visit(s: Tabs, href: string, traverse: boolean): Tabs {
  href = tabHref(href);
  const cur = activeTab(s);
  if (cur?.href === href) return s;
  const other = traverse ? s.tabs.find((t) => t.href === href) : undefined;
  if (other) return { ...s, active: other.id };
  return { ...s, tabs: s.tabs.map((t) => (t.id === s.active ? { ...t, href } : t)) };
}

// open adds a tab after the active one (and after its earlier background children). `foreground`
// switches to it. A location that is already open is reused rather than duplicated.
export function open(s: Tabs, href: string, id: string, { foreground = false } = {}): Tabs {
  href = tabHref(href);
  const existing = s.tabs.find((t) => t.href === href);
  if (existing) return foreground ? { ...s, active: existing.id } : s;
  let at = s.tabs.findIndex((t) => t.id === s.active) + 1;
  while (at < s.tabs.length && s.tabs[at].opener === s.active) at++;
  const tab: Tab = { id, href, opener: foreground ? undefined : s.active };
  return { tabs: [...s.tabs.slice(0, at), tab, ...s.tabs.slice(at)], active: foreground ? id : s.active };
}

// fresh is the + button: always a new tab on My tasks, at the end of the strip.
export function fresh(s: Tabs, id: string): Tabs {
  return { tabs: [...s.tabs, { id, href: home }], active: id };
}

export function activate(s: Tabs, id: string): Tabs {
  return s.tabs.some((t) => t.id === id) ? { ...s, active: id } : s;
}

// close removes a tab. Closing the active one moves to its right-hand neighbour, else the left;
// closing the last tab leaves a fresh one on My tasks.
export function close(s: Tabs, id: string, freshId: string): Tabs {
  const i = s.tabs.findIndex((t) => t.id === id);
  if (i < 0) return s;
  const tabs = s.tabs.filter((t) => t.id !== id);
  if (!tabs.length) return { tabs: [{ id: freshId, href: home }], active: freshId };
  if (s.active !== id) return { ...s, tabs };
  return { tabs, active: tabs[Math.min(i, tabs.length - 1)].id };
}

// restore rebuilds tabs saved by an earlier visit around the URL being loaded: a reload lands on
// its own tab, any other link opens as a new tab beside the one that was active.
export function restore(saved: unknown, href: string, id: string): Tabs {
  const single = { tabs: [{ id, href }], active: id };
  if (!saved || typeof saved !== "object") return single;
  const { tabs, active } = saved as Partial<Tabs>;
  if (!Array.isArray(tabs)) return single;
  const ids = new Set<string>();
  const clean: Tab[] = [];
  for (const t of tabs as Partial<Tab>[]) {
    // Same-origin paths only, and no duplicate ids a bad write could have left behind.
    if (!t || typeof t.id !== "string" || typeof t.href !== "string" || !/^\/(?!\/)/.test(t.href) || ids.has(t.id)) continue;
    ids.add(t.id);
    const to = tabHref(t.href);
    clean.push(typeof t.opener === "string" ? { id: t.id, href: to, opener: t.opener } : { id: t.id, href: to });
  }
  if (!clean.length) return single;
  // Openers keep later background tabs in order; one pointing at a tab that's gone means nothing.
  for (const t of clean) if (t.opener !== undefined && !ids.has(t.opener)) delete t.opener;
  const cur = clean.find((t) => t.id === active) ?? clean[0];
  return arrive({ tabs: clean, active: cur.id }, href, id);
}

// arrive brings a location in from outside (a fresh load, or returning to desktop width): the
// active tab if it's already there, else the tab that has it, else a new tab beside the active one.
export function arrive(s: Tabs, href: string, id: string): Tabs {
  href = tabHref(href);
  if (s.tabs.find((t) => t.id === s.active)?.href === href) return s;
  return open(s, href, id, { foreground: true });
}

// Forward is enabled when this browser tab's history has entries past the current one. The
// bookkeeping lives in sessionStorage, which a browser copies into tabs opened from this one;
// `keys` (each entry's history key by index) tells this history apart from such a copy.
export type HistoryMarks = { keys: (string | null)[]; max: number };

export function resumeHistory(saved: unknown, index: number, key: string): HistoryMarks {
  const m = saved as Partial<HistoryMarks> | null;
  if (key && m && Array.isArray(m.keys) && typeof m.max === "number" && m.keys[index] === key) return { keys: m.keys, max: m.max };
  return stepHistory({ keys: [], max: index }, index, key, true);
}

export function stepHistory(m: HistoryMarks, index: number, key: string, push: boolean): HistoryMarks {
  const keys = push ? m.keys.slice(0, index) : m.keys.slice();
  while (keys.length < index) keys.push(null);
  keys[index] = key;
  return { keys, max: push ? index : Math.max(m.max, index) };
}
