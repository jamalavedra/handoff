// What the browser keeps of an assistant conversation, and how it goes back to the server.

export type TaskRef = { id: string; title: string };
export type Step = { tool: string; summary: string; tasks?: TaskRef[]; error?: string };
export type Msg = {
  role: "user" | "assistant";
  content: string;
  steps?: Step[];
  // error: the turn stopped early; steps (if any) were still done.
  error?: boolean;
  // turn: the id a user message was sent with, so a retry resumes that turn on the server.
  turn?: string;
};

// The server's limit for one message, in bytes.
export const maxMessageBytes = 8000;
const maxNotedTasks = 10;

const encoder = new TextEncoder();

// clipBytes shortens text to at most max UTF-8 bytes without splitting a character.
export function clipBytes(text: string, max: number) {
  if (encoder.encode(text).length <= max) return text;
  let out = "";
  let used = 0;
  for (const ch of text) {
    const n = encoder.encode(ch).length;
    if (used + n > max) break;
    out += ch;
    used += n;
  }
  return out;
}

// toApi turns a kept message into what the server reads. The model only sees text, so the ids of
// tasks the assistant changed travel with its reply as a short note (ids only, a few at most), and
// the whole message stays within the server's limit.
export function toApi(m: Msg): { role: Msg["role"]; content: string } {
  const ids = [...new Set((m.steps ?? []).filter((s) => !s.error && s.tool !== "get_task" && s.tool !== "search_tasks").flatMap((s) => (s.tasks ?? []).map((t) => t.id)))];
  if (m.role !== "assistant" || !ids.length) return { role: m.role, content: clipBytes(m.content, maxMessageBytes) };
  const shown = ids.slice(0, maxNotedTasks);
  const note = `\n\n(Tasks I changed: ${shown.join(", ")}${ids.length > shown.length ? `, and ${ids.length - shown.length} more` : ""})`;
  return { role: m.role, content: clipBytes(m.content, maxMessageBytes - encoder.encode(note).length) + note };
}

type Stored = { org: string; member: string; msgs: Msg[] };

// parseStored returns the kept conversation only if it belongs to this workspace and member.
export function parseStored(raw: string | null, org: string, member: string): Msg[] {
  try {
    const v = JSON.parse(raw ?? "null") as Stored | null;
    return v && v.org === org && v.member === member && Array.isArray(v.msgs) ? v.msgs : [];
  } catch {
    return [];
  }
}

export function stored(org: string, member: string, msgs: Msg[]) {
  return JSON.stringify({ org, member, msgs: msgs.slice(-40) } satisfies Stored);
}
