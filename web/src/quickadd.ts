export type Ref = { id: string; name: string };

export type Parsed = {
  title: string;
  due?: string;
  assignee?: Ref;
  project?: Ref;
  priority?: number;
};

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
