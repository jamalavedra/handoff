export type Ref = { id: string; name: string };

export type Parsed = {
  title: string;
  due?: string;
  assignee?: Ref;
  project?: Ref;
  priority?: number;
  repeat?: Repeat;
};

export type Repeat = "daily" | "weekdays" | "weekly" | "biweekly" | "monthly" | "yearly";

export const repeatLabel: Record<Repeat, string> = {
  daily: "Every day", weekdays: "Every weekday", weekly: "Every week", biweekly: "Every 2 weeks", monthly: "Every month", yearly: "Every year",
};

// scheduleLabel says when a repeating task runs, anchored on its due date: "Every Friday", "Every month on the 14th".
export function scheduleLabel(repeat: Repeat, due?: string): string {
  if (!due || repeat === "daily" || repeat === "weekdays") return repeatLabel[repeat];
  const [y, m, d] = due.split("-").map(Number);
  const at = new Date(y, m - 1, d);
  const weekday = at.toLocaleDateString("en-US", { weekday: "long" });
  if (repeat === "weekly") return `Every ${weekday}`;
  if (repeat === "biweekly") return `Every other ${weekday}`;
  if (repeat === "monthly") return `Every month on the ${ordinal(d)}`;
  return `Every year on ${at.toLocaleDateString("en-US", { month: "short", day: "numeric" })}`;
}

function ordinal(n: number) {
  const s = n % 100 >= 11 && n % 100 <= 13 ? "th" : ({ 1: "st", 2: "nd", 3: "rd" } as Record<number, string>)[n % 10] ?? "th";
  return `${n}${s}`;
}

const WEEKDAYS = ["sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"];
const MONTHS = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"];
const MONTH = "(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)";
const LEAD = "(?:(?:on|by|due)\\s+)?";

export function isoDate(d: Date) {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

function addDays(d: Date, n: number) {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);
}

function monthDay(month: string, day: string, today: Date) {
  const m = MONTHS.indexOf(month.slice(0, 3).toLowerCase());
  const d = Number(day);
  if (m < 0 || d < 1 || d > 31) return undefined;
  for (let y = today.getFullYear(); y <= today.getFullYear() + 8; y++) {
    const at = new Date(y, m, d);
    if (at.getMonth() === m && at >= today) return at;
  }
  return undefined;
}

type DateRule = { re: RegExp; at: (m: RegExpMatchArray, today: Date) => Date | undefined };

const dateRules: DateRule[] = [
  { re: new RegExp(`${LEAD}(\\d{4})-(\\d{2})-(\\d{2})`, "i"), at: (m) => {
    const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
    return d.getMonth() === Number(m[2]) - 1 ? d : undefined;
  } },
  { re: /(?:(?:on|by|due)\s+)?(?:today|tonight)/i, at: (_, t) => t },
  { re: /(?:(?:on|by|due)\s+)?(?:tomorrow|tmrw?)/i, at: (_, t) => addDays(t, 1) },
  { re: /(?:(?:by|due)\s+)?next\s+week/i, at: (_, t) => addDays(t, ((8 - t.getDay()) % 7) || 7) },
  { re: /in\s+(\d{1,3})\s+(day|week)s?/i, at: (m, t) => addDays(t, Number(m[1]) * (m[2].toLowerCase() === "week" ? 7 : 1)) },
  { re: new RegExp(`${LEAD}(?:next\\s+)?(${WEEKDAYS.join("|")})`, "i"), at: (m, t) => {
    const wd = WEEKDAYS.indexOf(m[1].toLowerCase());
    return addDays(t, ((wd - t.getDay() + 7) % 7) || 7);
  } },
  { re: new RegExp(`${LEAD}${MONTH}\\s+(\\d{1,2})(?:st|nd|rd|th)?`, "i"), at: (m, t) => monthDay(m[1], m[2], t) },
  { re: new RegExp(`${LEAD}(\\d{1,2})(?:st|nd|rd|th)?\\s+${MONTH}`, "i"), at: (m, t) => monthDay(m[2], m[1], t) },
];

type RepeatRule = { re: RegExp; repeat: Repeat; start: (m: RegExpMatchArray, today: Date) => Date };

// Repeats are read before dates, so "every friday" isn't taken as a one-off Friday. Each sets the
// first due date unless the text also names one.
const repeatRules: RepeatRule[] = [
  { re: /every\s+day|daily/i, repeat: "daily", start: (_, t) => t },
  { re: /every\s+weekday|weekdays/i, repeat: "weekdays", start: (_, t) => addDays(t, t.getDay() === 6 ? 2 : t.getDay() === 0 ? 1 : 0) },
  { re: new RegExp(`every\\s+(${WEEKDAYS.join("|")})`, "i"), repeat: "weekly", start: (m, t) => addDays(t, (WEEKDAYS.indexOf(m[1].toLowerCase()) - t.getDay() + 7) % 7) },
  { re: /every\s+(?:other|2)\s+weeks?|biweekly|fortnightly/i, repeat: "biweekly", start: (_, t) => t },
  { re: /every\s+week|weekly/i, repeat: "weekly", start: (_, t) => t },
  { re: /every\s+month|monthly/i, repeat: "monthly", start: (_, t) => t },
  { re: /every\s+year|yearly|annually/i, repeat: "yearly", start: (_, t) => t },
];

const norm = (s: string) => s.toLowerCase().replace(/[\s_-]+/g, "");
const continuesWord = (s: string) => /^[_-]*[\p{L}\p{N}]/u.test(s);

// matchRef finds the longest name in refs that the text right after a sigil starts with.
// "Legal bot" matches "@legal bot", "@legal-bot" and "@legalbot".
function matchRef(rest: string, refs: Ref[]): { ref: Ref; len: number } | undefined {
  let best: { ref: Ref; len: number } | undefined;
  for (const ref of refs) {
    const target = norm(ref.name);
    if (!target) continue;
    let seen = "";
    for (let i = 0; i < rest.length; i++) {
      const ch = rest[i];
      if (!/[\s_-]/.test(ch)) seen += ch.toLowerCase();
      if (!target.startsWith(seen)) break;
      if (seen === target && !continuesWord(rest.slice(i + 1))) {
        if (!best || i + 1 > best.len) best = { ref, len: i + 1 };
        break;
      }
    }
  }
  return best;
}

function takeRef(text: string, sigil: string, refs: Ref[]): [string, Ref | undefined] {
  for (let i = 0; i < text.length; i++) {
    if (text[i] !== sigil || (i > 0 && !/\s/.test(text[i - 1]))) continue;
    const m = matchRef(text.slice(i + 1), refs);
    if (m) return [text.slice(0, i) + text.slice(i + 1 + m.len), m.ref];
  }
  return [text, undefined];
}

function bounded(re: RegExp) {
  return new RegExp(`(^|\\s)(${re.source})(?=$|[\\s.,;!?])`, re.flags);
}

// parseQuickAdd reads "Send contract to legal tomorrow @Marta #Sales !1" into a
// title plus a due date, assignee, project and priority. Unmatched @/# words stay in the title.
export function parseQuickAdd(text: string, opts: { members: Ref[]; projects: Ref[]; today?: Date }): Parsed {
  const now = opts.today ?? new Date();
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const out: Parsed = { title: "" };
  let t = text;
  [t, out.assignee] = takeRef(t, "@", opts.members);
  [t, out.project] = takeRef(t, "#", opts.projects);

  const pr = t.match(/(^|\s)(?:!|p)([0-4])(?=$|[\s.,;])/i);
  if (pr) {
    out.priority = Number(pr[2]);
    t = t.slice(0, pr.index!) + pr[1] + t.slice(pr.index! + pr[0].length);
  }

  let start: Date | undefined;
  for (const rule of repeatRules) {
    const m = t.match(bounded(rule.re));
    if (!m) continue;
    out.repeat = rule.repeat;
    start = rule.start(m[2].match(rule.re)!, today);
    t = t.slice(0, m.index!) + m[1] + t.slice(m.index! + m[0].length);
    break;
  }

  for (const rule of dateRules) {
    const m = t.match(bounded(rule.re));
    if (!m) continue;
    const inner = m[2].match(rule.re)!;
    const at = rule.at(inner, today);
    if (!at) continue;
    out.due = isoDate(at);
    t = t.slice(0, m.index!) + m[1] + t.slice(m.index! + m[0].length);
    break;
  }

  if (!out.due && start) out.due = isoDate(start);
  out.title = t.replace(/\s+/g, " ").trim();
  return out;
}

export type DueLabel = { text: string; tone: "overdue" | "today" | "soon" | "later" };

// dueLabel turns a YYYY-MM-DD due date into "Today", "Tomorrow", "Fri" or "Oct 14".
export function dueLabel(due: string, now: Date = new Date()): DueLabel {
  const [y, m, d] = due.split("-").map(Number);
  const at = new Date(y, m - 1, d);
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const days = Math.round((at.getTime() - today.getTime()) / 86400000);
  const month = at.toLocaleDateString("en-US", { month: "short", day: "numeric", ...(y !== today.getFullYear() ? { year: "numeric" } : {}) });
  if (days === 0) return { text: "Today", tone: "today" };
  if (days === 1) return { text: "Tomorrow", tone: "soon" };
  if (days === -1) return { text: "Yesterday", tone: "overdue" };
  if (days < 0) return { text: month, tone: "overdue" };
  if (days < 7) return { text: at.toLocaleDateString("en-US", { weekday: "short" }), tone: "soon" };
  return { text: month, tone: "later" };
}

export type DueSection<T> = { key: "overdue" | "today" | "upcoming" | "none"; title: string; tasks: T[] };

// groupByDue splits tasks into Overdue / Today / Upcoming / No date, dropping empty
// sections. Dated sections are ordered by due date; ties keep the input order.
export function groupByDue<T extends { due?: string }>(tasks: T[], now: Date = new Date()): DueSection<T>[] {
  const today = isoDate(now);
  const sections: DueSection<T>[] = [
    { key: "overdue", title: "Overdue", tasks: [] },
    { key: "today", title: "Today", tasks: [] },
    { key: "upcoming", title: "Upcoming", tasks: [] },
    { key: "none", title: "No date", tasks: [] },
  ];
  for (const t of tasks) {
    const i = !t.due ? 3 : t.due < today ? 0 : t.due === today ? 1 : 2;
    sections[i].tasks.push(t);
  }
  for (const s of sections) if (s.key !== "none") s.tasks.sort((a, b) => a.due!.localeCompare(b.due!));
  return sections.filter((s) => s.tasks.length > 0);
}

export type BoardColumn = "todo" | "working" | "done";

// boardColumn: closed tasks are Done; started or claimed ones are Working; the rest are To do.
export function boardColumn(t: { status: string; claimed_by?: unknown }): BoardColumn {
  if (t.status === "done" || t.status === "cancelled") return "done";
  if (t.status === "in_progress" || t.claimed_by) return "working";
  return "todo";
}

// agentOnIt: an agent is working on an open task right now: triaging it, holding its claim, or
// typing in its chat (`here` is the task's live presence, known only on the task page).
// Nothing works on a trashed task: triage skips it, even if it was still pending.
export function agentOnIt(t: { status: string; triage?: string; claimed_by?: { kind: string }; deleted_at?: string }, here?: { member: { kind: string }; typing?: boolean }[]): boolean {
  if (t.status === "done" || t.status === "cancelled" || t.deleted_at) return false;
  return t.triage === "pending" || t.claimed_by?.kind === "agent" || !!here?.some((h) => h.typing && h.member.kind === "agent");
}

const wordChar = /[\p{L}\p{N}._]/u;

// mentionQuery returns the "@partial" being typed just before the caret, if any.
export function mentionQuery(text: string, caret: number): { start: number; query: string } | null {
  const before = text.slice(0, Math.max(caret, 0));
  const at = before.lastIndexOf("@");
  if (at < 0 || (at > 0 && wordChar.test(before[at - 1]))) return null;
  const query = before.slice(at + 1);
  if (query.length > 30 || /\n|\s\s/.test(query)) return null;
  return { start: at, query };
}

// splitMentions marks "@Name" for known names, longest first, so comments can highlight them.
export function splitMentions(body: string, names: string[]): { text: string; mention: boolean }[] {
  const sorted = names.filter(Boolean).sort((a, b) => b.length - a.length);
  const out: { text: string; mention: boolean }[] = [];
  let buf = "";
  for (let i = 0; i < body.length; ) {
    if (body[i] === "@" && (i === 0 || !wordChar.test(body[i - 1]))) {
      const rest = body.slice(i + 1).toLowerCase();
      const n = sorted.find((n) => rest.startsWith(n.toLowerCase()) && !/[\p{L}\p{N}]/u.test(rest.charAt(n.length)));
      if (n) {
        if (buf) out.push({ text: buf, mention: false });
        out.push({ text: body.slice(i, i + 1 + n.length), mention: true });
        buf = "";
        i += 1 + n.length;
        continue;
      }
    }
    buf += body[i];
    i++;
  }
  if (buf) out.push({ text: buf, mention: false });
  return out;
}
