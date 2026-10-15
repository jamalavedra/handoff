// The sources an agent's message cites. (Task checklists are todos now; see todos.ts.)

// sources are the sites a message links to, once each, in order: [{ host, url }].
export function sources(md: string, max = 4) {
  const out: { host: string; url: string }[] = [];
  for (const m of md.matchAll(/https?:\/\/[^\s)<>\]"']+/g)) {
    let host: string;
    try {
      host = new URL(m[0]).hostname.replace(/^www\./, "");
    } catch {
      continue;
    }
    if (out.some((s) => s.host === host)) continue;
    out.push({ host, url: m[0] });
    if (out.length === max) break;
  }
  return out;
}
