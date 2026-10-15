import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowUp, AtSign, ChevronDown, Globe, MessageSquare, Search, SmilePlus } from "lucide-react";
import { sources } from "../checklist";
import { toast } from "sonner";
import { ago, api, cn, type Comment, type Event, type Lookup, type Member, type MemberRef, type Task } from "../lib";
import { mentionQuery } from "../quickadd";
import { insertAt, mentionText, replaceQuery, step } from "../mention";
import { MentionMenu, useMentionItems, type Item } from "./mention-menu";
import { useMe } from "../main";
import { Markdown } from "./markdown";
import { Avatar, ErrorText, Who } from "./app";
import { MicButton } from "./dictation";
import { useDictation } from "@/hooks/use-dictation";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { ActivityTrail, LiveTrail } from "./activity-trail";
import { useAgentLine, useChatInView } from "../agent-activity";
import { cleanQuote, replyArgs, snippet, type ReplyDraft } from "../reply";
import { QuotedReply, ReplyAction, ReplyChip, ReplyContext, ReplyTarget, SelectionReply } from "./reply";

// A task's chat is its conversation with the team and its agents. It lives in the assistant panel
// ("This task"), beside the task it's about; the task itself shows a preview that opens it.

export type Here = { member: MemberRef; typing?: boolean };
export type Detail = { task: Task; comments: Comment[]; events: Event[]; here?: Here[] };

export const names = (h: Here[]) => (h.length <= 2 ? h.map((x) => x.member.name).join(" and ") : `${h[0].member.name} and ${h.length - 1} others`);

export const taskQuery = (id: string) => ({ queryKey: ["task", id], queryFn: () => api.tool<Detail>("get_task", { id }) });

// TaskChat is the thread and its composer, filling the panel it's in.
export function TaskChat({ id, focus, drawer, active = true }: { id: string; focus?: number; drawer?: boolean; active?: boolean }) {
  const qc = useQueryClient();
  const q = useQuery({ ...taskQuery(id), refetchInterval: 10000 });
  const members = useQuery({ queryKey: ["members"], queryFn: () => api.tool<{ members: Member[] }>("list_members") });
  const meId = useMe().data?.member?.id;
  const [comment, setComment] = useState("");
  const [caret, setCaret] = useState(0);
  const [pick, setPick] = useState(0);
  // Esc closes the @ menu for the mention it was showing; typing another @ opens it again.
  const [dismissed, setDismissed] = useState<number | null>(null);
  const ref = useRef<HTMLTextAreaElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  // The message (or the part of one) the next message replies to.
  const [reply, setReply] = useState<ReplyDraft | null>(null);
  const startReply = useCallback((c: Comment, quote?: string) => {
    setReply({ id: c.id, author: c.author?.name, quote: quote ? cleanQuote(quote) : undefined, excerpt: snippet(c.body) });
    ref.current?.focus();
  }, []);
  const endRef = useRef<HTMLDivElement>(null);
  const lastTyping = useRef(0);
  const typing = () => {
    if (Date.now() - lastTyping.current < 3000) return;
    lastTyping.current = Date.now();
    api.tool("watch_task", { id, typing: true }).catch(() => {});
  };
  const dictation = useDictation((text) => { setComment(text); setCaret(text.length); typing(); });
  // Switching tasks drops the draft and dictation that belonged to the old one.
  const { cancel: cancelDictation } = dictation;
  useEffect(() => () => { cancelDictation(); setComment(""); setCaret(0); setReply(null); }, [id, cancelDictation]);
  useEffect(() => { if (active && (focus || !drawer)) ref.current?.focus(); }, [focus, id, drawer, active]);
  // Mounted only while the panel is open; active while it shows this chat.
  useChatInView(id, active);
  const count = q.data?.comments.length ?? 0;
  useEffect(() => endRef.current?.scrollIntoView({ block: "end" }), [count, id]);
  const send = useMutation({
    // A quick answer stands alone: it drops the reply that was open (`drop`) instead of answering it.
    mutationFn: (v: { body: string; reply?: ReplyDraft | null; drop?: ReplyDraft | null }) => api.tool("comment", { id, body: v.body, ...replyArgs(v.reply ?? null) }),
    onSuccess: (_, v) => { setComment(""); const sent = v.reply ?? v.drop; if (sent) setReply((r) => (r === sent ? null : r)); lastTyping.current = 0; qc.invalidateQueries({ queryKey: ["task"] }); qc.invalidateQueries({ queryKey: ["tasks"] }); },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  });

  const people = members.data?.members.filter((m) => m.status !== "removed") ?? [];
  const memberNames = people.map((m) => m.name);
  const typed = mentionQuery(comment, caret);
  const mq = typed && typed.start !== dismissed ? typed : null;
  const suggestions = useMentionItems(mq?.query ?? null, people, id);
  const picked = step(pick, 0, suggestions.length);
  // edit writes the composer and puts the caret back where the change ends.
  const edit = (next: { text: string; caret: number }) => {
    setComment(next.text);
    setCaret(next.caret);
    setPick(0);
    requestAnimationFrame(() => {
      ref.current?.focus();
      ref.current?.setSelectionRange(next.caret, next.caret);
    });
  };
  const insertMention = (item: Item) => {
    if (mq) edit(replaceQuery(comment, mq.start, caret, mentionText(item)));
  };
  const t = q.data?.task;
  const comments = q.data?.comments ?? [];
  const typers = (q.data?.here ?? []).filter((h) => h.typing);
  // Nothing to write into until the task has loaded, and never into the trash.
  const locked = !t || !!t.deleted_at;

  return (
    <ReplyContext.Provider value={locked ? null : startReply}>
      <div ref={scrollRef} className="relative min-h-0 flex-1 overflow-y-auto px-4 py-4" aria-live="polite">
        {q.error && <ErrorText error={q.error} />}
        {q.data && comments.length === 0 && (
          <div className="mt-8 flex flex-col items-center gap-2 text-center text-sm text-muted-foreground">
            <MessageSquare className="size-5 opacity-60" />
            <p>No messages yet.</p>
            <p className="max-w-60 text-xs">{t?.assignee?.kind === "agent" ? `Ask ${t.assignee.name} anything about this task, or type @ to bring someone in.` : "Talk about this task here. Type @ to mention someone or an agent."}</p>
          </div>
        )}
        <section className="flex flex-col gap-3" aria-label="Comments">
          {comments.map((c, i) => (
            <ReplyTarget key={c.id} c={c}>
              <Bubble
                c={c} mine={c.author?.id === meId} mentions={memberNames} locked={locked}
                // The agent's question, while the task waits on me: its answers are a tap away.
                answers={i === comments.length - 1 && t?.waiting_on?.id === meId && !send.isPending ? c.options : undefined}
                onAnswer={(a) => send.mutate({ body: a, drop: reply })}
              />
            </ReplyTarget>
          ))}
        </section>
        <SelectionReply scroller={scrollRef} comments={comments} />
        <LiveTrail task={id} />
        <div ref={endRef} />
      </div>
      {!locked && (
        <form
          className={cn("relative mx-3 mt-1 flex flex-wrap items-end gap-2 rounded-2xl border border-border bg-card p-1.5 pl-3 shadow-sm focus-within:ring-2 focus-within:ring-input", drawer ? "mb-[calc(0.75rem+env(safe-area-inset-bottom))]" : "mb-3")}
          onSubmit={(e) => { e.preventDefault(); dictation.cancel(); if (comment.trim() && !send.isPending) send.mutate({ body: comment, reply }); }}
        >
          {reply && <ReplyChip draft={reply} onCancel={() => { setReply(null); ref.current?.focus(); }} />}
          <MentionMenu items={suggestions} active={picked} onPick={insertMention} onHover={setPick} />
          {typers.length > 0 && <span aria-live="polite" className="absolute -top-6 left-3 animate-pulse text-xs text-muted-foreground">{names(typers)} {typers.length === 1 ? "is" : "are"} typing…</span>}
          <textarea
            rows={1}
            ref={ref}
            placeholder={t?.assignee?.kind === "agent" ? `Message ${t.assignee.name}…` : "Message… type @ to mention"}
            value={comment}
            role="combobox"
            aria-expanded={suggestions.length > 0}
            aria-controls={suggestions.length ? "mention-menu" : undefined}
            aria-activedescendant={suggestions.length ? `mention-${picked}` : undefined}
            onChange={(e) => {
              dictation.cancel();
              const at = e.target.selectionStart ?? 0;
              setComment(e.target.value);
              setCaret(at);
              setPick(0);
              // A dismissed menu stays closed while that same @ is being typed; a new @ reopens it.
              if (mentionQuery(e.target.value, at)?.start !== dismissed) setDismissed(null);
              typing();
            }}
            onSelect={(e) => setCaret(e.currentTarget.selectionStart ?? 0)}
            onKeyDown={(e) => {
              if (suggestions.length) {
                if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); setPick(step(picked, e.key === "ArrowDown" ? 1 : -1, suggestions.length)); return; }
                if ((e.key === "Enter" && !e.nativeEvent.isComposing) || e.key === "Tab") { e.preventDefault(); insertMention(suggestions[picked]); return; }
                if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); setDismissed(mq?.start ?? null); return; }
              }
              // Esc closes the @ menu first, then drops the reply.
              if (e.key === "Escape" && reply) { e.preventDefault(); e.stopPropagation(); setReply(null); return; }
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); e.currentTarget.form?.requestSubmit(); }
            }}
            aria-label="Comment"
            className="max-h-40 min-h-9 flex-1 resize-none bg-transparent py-1.5 text-base outline-none [field-sizing:content] placeholder:text-muted-foreground/70 sm:text-sm"
          />
          <button type="button" onClick={() => { dictation.cancel(); setDismissed(null); edit(insertAt(comment, ref.current?.selectionStart ?? caret)); }} aria-label="Mention a person, agent or task" title="Mention a person, agent or task" className="flex size-9 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none">
            <AtSign className="size-[18px]" />
          </button>
          {dictation.supported && <MicButton listening={dictation.listening} onToggle={() => dictation.toggle(comment)} wave={dictation} />}
          <button disabled={!comment.trim() || send.isPending} aria-label="Send comment" className="flex size-9 shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground disabled:bg-foreground/10 disabled:text-muted-foreground/70">
            <ArrowUp className="size-4" />
          </button>
        </form>
      )}
    </ReplyContext.Provider>
  );
}

// Bubble is one message; an agent's carries the trace of what it did underneath.
function Bubble({ c, mine, mentions, locked, answers, onAnswer }: { c: Comment; mine: boolean; mentions: string[]; locked: boolean; answers?: string[]; onAnswer: (a: string) => void }) {
  const cited = c.author?.kind === "agent" ? sources(c.body) : [];
  return (
    <div className={cn("flex gap-2.5", mine && "flex-row-reverse")}>
      {!mine && <Avatar m={c.author} className="mt-0.5" />}
      <div className="flex min-w-0 max-w-[88%] flex-col gap-1.5">
        <div className={cn("min-w-0 rounded-2xl px-3 py-2 text-[15px] sm:text-sm", mine ? "rounded-tr-md bg-accent" : "rounded-tl-md bg-card shadow-[0_0_0_1px_rgba(0,0,0,0.06)]")}>
          {!mine && <div className="mb-0.5 text-xs text-muted-foreground"><Who m={c.author} by={c.on_behalf_of} /> · {ago(c.created_at)}</div>}
          {!c.activity?.length && c.lookups?.length ? <LookedUp lookups={c.lookups} /> : null}
          <QuotedReply r={c.reply_to} />
          <Markdown text={c.body} mentions={mentions} />
          {cited.length > 0 && (
            <div className="mt-2 flex flex-wrap items-center gap-1 text-xs text-muted-foreground">
              Sources
              {cited.map((s) => (
                <a key={s.host} href={s.url} target="_blank" rel="noreferrer noopener" className="flex max-w-44 items-center gap-1 rounded-full border border-border px-2 py-0.5 hover:bg-muted hover:text-foreground">
                  <Globe className="size-3 shrink-0" /><span className="truncate">{s.host}</span>
                </a>
              ))}
            </div>
          )}
          <Reactions c={c} disabled={locked} />
          {mine && <div className="mt-0.5 text-right text-[11px] text-muted-foreground/70">{ago(c.created_at)}</div>}
        </div>
        {c.activity?.length ? <ActivityTrail steps={c.activity} className="px-1" /> : null}
        {answers && answers.length > 0 && (
          <div className="flex flex-wrap gap-1.5" role="group" aria-label="Quick answers">
            {answers.map((a) => (
              <button key={a} type="button" onClick={() => onAnswer(a)} className="rounded-full border border-border bg-card px-3 py-1 text-sm hover:bg-[linear-gradient(var(--accent),var(--accent))]">
                {a}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// ChatPreview sits at the bottom of a task: the latest message and a way into the chat.
// While the workspace's agent works on the task, it shows its latest step instead.
export function ChatPreview({ task, comments, agent, typing, onOpen }: { task?: string; comments: Comment[]; agent?: MemberRef; typing?: string; onOpen: () => void }) {
  const last = comments.at(-1);
  typing = useAgentLine(task) ?? typing;
  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label={`Open chat${comments.length ? `, ${comments.length} messages` : ""}`}
      className="sticky bottom-[calc(0.75rem+env(safe-area-inset-bottom))] z-10 mt-auto flex w-full items-center gap-2.5 rounded-2xl border border-border bg-card p-1.5 pl-3 text-left shadow-lg transition-colors hover:bg-[linear-gradient(var(--accent),var(--accent))] md:bottom-4"
    >
      <MessageSquare className="size-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1 py-1.5 text-sm">
        {typing ? (
          <span className="animate-pulse text-muted-foreground">{typing}</span>
        ) : last ? (
          <span className="flex min-w-0 gap-1"><span className="shrink-0 font-medium">{last.author?.name ?? "Someone"}:</span><span className="truncate text-foreground/70">{plain(last.body)}</span></span>
        ) : (
          <span className="text-muted-foreground/70">{agent ? `Message ${agent.name}…` : "Message… type @ to mention"}</span>
        )}
      </span>
      {comments.length > 0 && <span className="shrink-0 rounded-full bg-accent px-2 py-0.5 text-xs tabular-nums text-muted-foreground">{comments.length}</span>}
      <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground"><ArrowUp className="size-4" /></span>
    </button>
  );
}

// plain flattens a message to one line of text for the preview.
const plain = (md: string) => md.replace(/[#*_`>[\]]|\(https?:[^)]*\)/g, "").replace(/\s+/g, " ").trim();

const quickReactions = ["👍", "✅", "👀", "🎉", "❤️", "🙏", "😄", "🚀"];

// LookedUp shows what the agent checked in connected services before answering: the services at a
// glance, each call when opened.
function LookedUp({ lookups }: { lookups: Lookup[] }) {
  const sources = [...new Set(lookups.map((l) => l.source))];
  return (
    <details className="group mb-1 text-xs text-muted-foreground">
      <summary className="flex cursor-pointer list-none items-center gap-1 hover:text-foreground [&::-webkit-details-marker]:hidden">
        <Search className="size-3" />Looked up {sources.join(", ")}
        <ChevronDown className="size-3 transition-transform group-open:rotate-180" />
      </summary>
      <ul className="mt-1 flex flex-col gap-0.5 border-l border-border pl-2">
        {lookups.map((l, i) => (
          <li key={i} className={cn("truncate", l.failed && "line-through opacity-70")} title={l.failed ? "This lookup failed" : undefined}>
            <span className="text-foreground/70">{l.source}</span> · {l.query ? `“${l.query}”` : l.tool}
          </li>
        ))}
      </ul>
    </details>
  );
}

// Reactions are the light way to answer in a thread: ✅ "done", 👀 "looking", 👍 "agreed". Agents use them too.
function Reactions({ c, disabled }: { c: Comment; disabled: boolean }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const react = useMutation({
    mutationFn: (emoji: string) => api.tool("react", { comment: c.id, emoji }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["task"] }),
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  });
  const reactions = c.reactions ?? [];
  if (disabled && !reactions.length) return null;
  return (
    <div className="mt-1.5 flex flex-wrap items-center gap-1">
      {reactions.map((r) => (
        <button
          key={r.emoji}
          type="button"
          disabled={disabled || react.isPending}
          title={r.by.join(", ")}
          aria-pressed={!!r.mine}
          aria-label={`${r.emoji} ${r.count}, ${r.mine ? "remove yours" : "react"}`}
          onClick={() => react.mutate(r.emoji)}
          className={cn("flex h-6 items-center gap-1 rounded-full border px-1.5 text-xs tabular-nums transition-colors", r.mine ? "border-ring bg-accent text-foreground" : "border-border text-muted-foreground hover:bg-muted")}
        >
          <span className="text-sm leading-none">{r.emoji}</span>{r.count}
        </button>
      ))}
      {!disabled && (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <button type="button" aria-label="Add reaction" className="flex size-6 items-center justify-center rounded-full text-muted-foreground/70 transition-colors hover:bg-muted hover:text-foreground">
              <SmilePlus className="size-3.5" />
            </button>
          </PopoverTrigger>
          <PopoverContent align="start" className="flex w-auto gap-0.5 rounded-full p-1">
            {quickReactions.map((e) => (
              <button key={e} type="button" disabled={react.isPending} aria-label={`React ${e}`} onClick={() => { setOpen(false); react.mutate(e); }} className="flex size-8 items-center justify-center rounded-full text-lg transition-transform hover:scale-110 hover:bg-muted">
                {e}
              </button>
            ))}
          </PopoverContent>
        </Popover>
      )}
      {!disabled && <ReplyAction c={c} />}
    </div>
  );
}
