// Desktop tabs: a browser-style strip of open pages and tasks. Pure state here; the strip lives in components/tabs.tsx.

// A tab is a location (pathname + search). `opener` is the tab it was opened from, so a run of
// background tabs opened from one place lines up in order, like a browser.
export type Tab = { id: string; href: string; opener?: string };
export type Tabs = { tabs: Tab[]; active: string };

export const home = "/";

// What a tab is named after: a task page, a project, or another page. A ?task= side panel is
// passing detail, so the tab keeps the page's name (the panel's ⋯ menu can give the task its own tab).
export type Target = { kind: "task"; id: string } | { kind: "project"; id: string } | { kind: "page"; path: string };

export function target(href: string): Target {
  const path = href.split("?")[0];
  const task = path.match(/^\/tasks\/([^/]+)/)?.[1];
  if (task) return { kind: "task", id: decodeURIComponent(task) };
  const project = path.match(/^\/projects\/([^/]+)/)?.[1];
  if (project) return { kind: "project", id: decodeURIComponent(project) };
  return { kind: "page", path };
}

export const taskHref = (id: string) => `/tasks/${encodeURIComponent(id)}`;

const activeTab = (s: Tabs) => s.tabs.find((t) => t.id === s.active);

// visit follows the router. Normally the active tab takes the new location; going back or forward
// onto a location another tab holds switches to that tab instead, so Back undoes a tab switch.
export function visit(s: Tabs, href: string, traverse: boolean): Tabs {
  const cur = activeTab(s);
  if (cur?.href === href) return s;
  const other = traverse ? s.tabs.find((t) => t.href === href) : undefined;
  if (other) return { ...s, active: other.id };
  return { ...s, tabs: s.tabs.map((t) => (t.id === s.active ? { ...t, href } : t)) };
}

// open adds a tab after the active one (and after its earlier background children). `foreground`
// switches to it. A location that is already open is reused rather than duplicated.
export function open(s: Tabs, href: string, id: string, { foreground = false } = {}): Tabs {
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
    clean.push({ id: t.id, href: t.href });
  }
  if (!clean.length) return single;
  const cur = clean.find((t) => t.id === active) ?? clean[0];
  if (cur.href === href) return { tabs: clean, active: cur.id };
  return open({ tabs: clean, active: cur.id }, href, id, { foreground: true });
}
