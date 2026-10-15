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
  note_created: "wrote a note", note_updated: "updated a note", note_trashed: "trashed a note", note_restored: "restored a note",
};

// Keys in an update's data that say how, not what.
const quiet = new Set(["from_project_id", "previous_body", "note_id", "project_id", "todo_id", "from", "from_title"]);

export function verb(e: Event) {
  if (e.kind === "todo_updated" && typeof e.data.done === "boolean") return e.data.done ? "ticked a todo on" : "unticked a todo on";
  return verbs[e.kind] ?? e.kind;
}

// eventDetail is the short "what" after an event's verb.
export function eventDetail(e: Event): string {
  const d = e.data;
  switch (e.kind) {
    case "closed": return `${d.status}${d.reason ? `: ${d.reason}` : ""}`;
    case "updated": return Object.keys(d).filter((k) => !quiet.has(k)).map((k) => (k === "body" ? "text" : k)).join(", ");
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
