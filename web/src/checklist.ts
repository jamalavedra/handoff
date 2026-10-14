// Checklists in task details ("- [ ] step") and the sources an agent's message cites.

const item = /^(\s*(?:[-*+]|\d+[.)])\s+\[)([ xX])(\])/;
const fence = /^\s*(```|~~~)/;

// toggleItem ticks or unticks the nth checklist item (0-based, in document order, skipping code).
export function toggleItem(md: string, n: number): string {
  const lines = md.split("\n");
  let inFence = false;
  let seen = 0;
  for (let i = 0; i < lines.length; i++) {
    if (fence.test(lines[i])) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    const m = lines[i].match(item);
    if (!m) continue;
    if (seen++ === n) {
      lines[i] = m[1] + (m[2] === " " ? "x" : " ") + m[3] + lines[i].slice(m[0].length);
      return lines.join("\n");
    }
  }
  return md;
}

// progress counts a checklist: how many items, how many ticked.
export function progress(md: string) {
  let inFence = false;
  let total = 0;
  let done = 0;
  for (const l of md.split("\n")) {
    if (fence.test(l)) inFence = !inFence;
    else if (!inFence) {
      const m = l.match(item);
      if (m) {
        total++;
        if (m[2] !== " ") done++;
      }
    }
  }
  return { total, done };
}

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
