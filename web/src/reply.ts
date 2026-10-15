// Replies in a task's chat: a message can answer another one, optionally quoting the part it's about.

// maxQuote matches what the server keeps of a quote.
export const maxQuote = 500;

// cleanQuote folds a selection to one line and clips it the way the server does.
export function cleanQuote(s: string, max = maxQuote): string {
  const t = s.replace(/\s+/g, " ").trim();
  const r = [...t];
  return r.length > max ? r.slice(0, max - 1).join("") + "…" : t;
}

// snippet is the start of a message as plain text, for the reply chip when nothing is quoted.
export function snippet(md: string, max = 140): string {
  return cleanQuote(md.replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1").replace(/[#*_`>~]/g, ""), max);
}

export type ReplyDraft = { id: string; author?: string; quote?: string; excerpt: string };

// replyArgs is what the comment tool takes for a draft reply.
export function replyArgs(d: ReplyDraft | null): { reply_to?: string; quote?: string } {
  if (!d) return {};
  return d.quote ? { reply_to: d.id, quote: d.quote } : { reply_to: d.id };
}

// selected is the message a selection lies in and its text, when it lies in exactly one.
export function selected(anchorMsg: string | null, focusMsg: string | null, text: string): { id: string; quote: string } | null {
  const quote = cleanQuote(text);
  if (!anchorMsg || anchorMsg !== focusMsg || !quote) return null;
  return { id: anchorMsg, quote };
}

export const messageDomId = (id: string) => `msg-${id}`;
