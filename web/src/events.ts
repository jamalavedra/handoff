// How a change in a history reads: its verb and a short "what".

import type { Event } from "./lib";

const verbs: Record<string, string> = {
  created: "created", updated: "updated", claimed: "started working on", released: "released", closed: "closed", commented: "commented on",
  linked: "added a dependency to", unlinked: "removed a dependency from", trashed: "trashed", restored: "restored",
  approval_requested: "asked to delete", approval_approved: "approved deleting", approval_rejected: "rejected deleting",
  agent_added: "added an agent", agent_revoked: "revoked an agent", agent_permissions_changed: "changed an agent's permissions",
  api_key_created: "created an API key", api_key_revoked: "revoked an API key", project_created: "created a project",
  triaged: "triaged", triage_failed: "couldn't triage", repeat_ended: "ended the repeating series of", run_now: "ran",
  member_invited: "invited someone", member_changed: "changed a member", org_changed: "changed workspace settings", project_changed: "changed a project",
  todos_added: "added todos to", todo_updated: "changed a todo on", todo_removed: "removed a todo from",
  reverted: "reverted a change to",
  note_created: "wrote a note", note_updated: "updated a note", note_trashed: "trashed a note", note_restored: "restored a note",
};

// Keys in an update's data that say how, not what.
const quiet = new Set(["from_project_id", "previous_body", "note_id", "project_id", "assignee_id", "todo_id", "from", "from_title"]);

// The task fields an update records old values for (from_<key>), with the key that holds the name
// shown for the new value when it differs.
const fields: Record<string, { name: string; shown?: string }> = {
  title: { name: "title" }, status: { name: "status" }, priority: { name: "priority" }, assignee_id: { name: "assignee", shown: "assignee" },
  due: { name: "due" }, labels: { name: "labels" }, repeat: { name: "repeat" }, project_id: { name: "project", shown: "project" },
};
const statusText: Record<string, string> = { open: "open", in_progress: "in progress", done: "done", cancelled: "archived" };
const priorityText = ["urgent", "high", "normal", "low", "someday"];

// Names maps member and project ids to their names, for the old value of an assignee or project.
export type Names = Record<string, string>;

function show(key: string, v: unknown, names?: Names): string {
  if (v === "" || v == null || (Array.isArray(v) && v.length === 0)) return "none";
  if (key === "status") return statusText[String(v)] ?? String(v);
  if (key === "priority") return priorityText[Number(v)] ?? String(v);
  if (key === "assignee_id" || key === "project_id") return names?.[String(v)] ?? "another";
  const s = Array.isArray(v) ? v.join(", ") : String(v);
  return s.length > 40 ? `${s.slice(0, 39)}…` : s;
}

export type Change = { key: string; field: string; from: string; to: string };

// changes is what an update changed, from what to what.
export function changes(e: Event, names?: Names): Change[] {
  const d = e.data;
  if (e.kind !== "updated") return [];
  return Object.entries(fields).filter(([k]) => `from_${k}` in d && k in d).map(([k, f]) => ({
    key: k, field: f.name, from: show(k, d[`from_${k}`], names), to: f.shown && typeof d[f.shown] === "string" ? String(d[f.shown]) : show(k, d[k], names),
  }));
}

// revertable: revert_change can undo this event (unless something changed it again since).
export function revertable(e: Event) {
  const d = e.data;
  switch (e.kind) {
    case "updated": return changes(e).length > 0;
    case "closed": case "trashed": case "restored": return true;
    case "todo_updated": return "from" in d || "from_done" in d || "from_result" in d;
  }
  return false;
}

export function verb(e: Event) {
  if (e.kind === "todo_updated" && typeof e.data.done === "boolean") return e.data.done ? "ticked a todo on" : "unticked a todo on";
  return verbs[e.kind] ?? e.kind;
}

// eventDetail is the short "what" after an event's verb.
export function eventDetail(e: Event, names?: Names): string {
  const d = e.data;
  switch (e.kind) {
    case "closed": return `${d.status}${d.reason ? `: ${d.reason}` : ""}`;
    case "updated": {
      const ch = changes(e, names);
      const covered = new Set(ch.flatMap((c) => [c.key, fields[c.key].shown ?? c.key]));
      const rest = Object.keys(d).filter((k) => !quiet.has(k) && !k.startsWith("from_") && !covered.has(k)).map((k) => (k === "body" ? "text" : k));
      return [...ch.map((c) => `${c.field} ${c.from} → ${c.to}`), ...rest].join(", ");
    }
    case "reverted": return ((d.fields as string[] | undefined) ?? []).map((k) => fields[k]?.name ?? k).join(", ");
    // A note's events always carry its title; it only changed when from_title is there.
    case "note_updated": return [...("from_title" in d ? ["title"] : []), ...("body" in d ? ["text"] : [])].join(", ");
    case "commented": return String(d.excerpt ?? "");
    case "triaged": return ((d.changed as string[] | undefined) ?? []).join(", ");
    case "triage_failed": return String(d.error ?? "");
    case "linked": case "unlinked": return `waits on ${d.blocker}`;
    case "run_now": return String(d.agent ?? "");
    case "todos_added": return ((d.todos as string[] | undefined) ?? []).join(" · ");
    case "todo_updated": return `${d.text ?? ""}${typeof d.result === "string" && d.result ? ` → ${d.result}` : ""}`;
    case "todo_removed": return String(d.text ?? "");
  }
  return "";
}
