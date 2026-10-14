import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useRouterState, useSearch } from "@tanstack/react-router";
import { AlertCircle, ArrowUp, CalendarDays, Check, CheckCircle2, ChevronRight, FileText, FolderKanban, ListPlus, MessageSquare, Pencil, Plus, RotateCcw, Search, Sparkles, SquarePen, Users, X } from "lucide-react";
import { api, cn, projectName, type Me, type MemberRef, type Project, type Task } from "../lib";
import { Markdown } from "./markdown";
import { TaskLink } from "./tasklink";
import { Avatar } from "./app";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Drawer, DrawerContent, DrawerDescription, DrawerTitle } from "@/components/ui/drawer";

// The assistant is a chat with the workspace's triage agent: it answers questions about tasks and
// makes changes as that agent, for the person asking. The conversation lives in this tab only.

type TaskRef = { id: string; title: string };
type Step = { tool: string; summary: string; tasks?: TaskRef[]; error?: string };
type Msg = { role: "user" | "assistant"; content: string; steps?: Step[]; error?: boolean };
type Status = { enabled: boolean; agent?: MemberRef };
type Reply = { reply: string; steps: Step[]; agent?: MemberRef; changed: boolean };

type Ctx = { open: boolean; setOpen: (o: boolean) => void; toggle: () => void };
const AssistantContext = createContext<Ctx>({ open: false, setOpen: () => {}, toggle: () => {} });
export const useAssistant = () => useContext(AssistantContext);

const desktop = window.matchMedia("(min-width: 768px)");
const roomy = window.matchMedia("(min-width: 1280px)");

function useMedia(mq: MediaQueryList) {
  return useSyncExternalStore(
    (l) => {
      mq.addEventListener("change", l);
      return () => mq.removeEventListener("change", l);
    },
    () => mq.matches,
  );
}

// From the sidebar breakpoint up the assistant is a side panel; below it, a drawer.
const useDesktop = () => useMedia(desktop);

// useAssistantInset is how far the open side panel pushes things anchored to the right edge (toasts).
export function useAssistantInset() {
  const a = useAssistant();
  const isDesktop = useDesktop();
  const xl = useMedia(roomy);
  if (!a.open || !isDesktop) return undefined;
  return xl ? "calc(25rem + 24px)" : "calc(22rem + 24px)";
}

const storeKey = "handoff-assistant";

function load(): Msg[] {
  try {
    const v = JSON.parse(sessionStorage.getItem(storeKey) ?? "[]");
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}

// clearAssistant forgets this tab's conversation, e.g. on sign-out.
export function clearAssistant() {
  try {
    sessionStorage.removeItem(storeKey);
  } catch {
    // nothing stored
  }
}

function save(msgs: Msg[]) {
  try {
    sessionStorage.setItem(storeKey, JSON.stringify(msgs.slice(-40)));
  } catch {
    // private mode or full: the chat just won't survive a reload
  }
}

// AssistantProvider owns the open state and ⌘J. The panel itself is rendered by <AssistantPanel />.
export function AssistantProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const toggle = useCallback(() => setOpen((o) => !o), []);
  const ctx = useMemo(() => ({ open, setOpen, toggle }), [open, toggle]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "j" && (e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey) {
        e.preventDefault();
        toggle();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [toggle]);
  return <AssistantContext.Provider value={ctx}>{children}</AssistantContext.Provider>;
}

// AssistantButton is the sidebar entry.
export function AssistantButton() {
  const a = useAssistant();
  return (
    <button
      onClick={a.toggle}
      aria-pressed={a.open}
      className={cn(
        "group flex min-h-8 items-center gap-2 rounded-[10px] px-2 py-1 text-[14px] font-medium text-foreground/70 transition-colors hover:bg-sidebar-accent/60",
        a.open && "bg-sidebar-accent text-foreground shadow-[0_1px_2px_rgba(0,0,0,0.06)]",
      )}
    >
      <Sparkles className={cn("size-4 text-foreground/50 transition-colors group-hover:text-foreground/80", a.open && "text-violet-600 group-hover:text-violet-600")} />
      <span className="flex-1 text-left">Assistant</span>
      <kbd className="rounded border border-border px-1 font-sans text-[10px] text-muted-foreground">⌘J</kbd>
    </button>
  );
}

// usePageContext is what the person is looking at: the open task, or the current project or page.
function usePageContext() {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const panelTask = useSearch({ strict: false }).task;
  const task = path.match(/^\/tasks\/([^/]+)/)?.[1] ?? panelTask;
  const project = task ? undefined : path.match(/^\/projects\/([^/]+)/)?.[1];
  const t = useQuery({ queryKey: ["task", task], queryFn: () => api.tool<{ task: Task }>("get_task", { id: task }), enabled: !!task, refetchInterval: false });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const p = projects.data?.projects.find((x) => x.id === project);
  const pages: Record<string, string> = { "/": "My tasks", "/notifications": "Notifications", "/team": "Team", "/projects": "Projects", "/activity": "Activity", "/agents": "Agents", "/trash": "Trash", "/admin": "Admin" };
  if (task) return { key: `task:${task}`, kind: "task" as const, task, label: t.data?.task.title ?? task };
  if (project) return { key: `project:${project}`, kind: "project" as const, project, label: p ? projectName(p) : "Project" };
  return { key: `page:${path}`, kind: "page" as const, label: pages[path] ?? "Handoff" };
}

const stepIcon: Record<string, typeof Search> = {
  search_tasks: Search, get_task: FileText, list_projects: FolderKanban, list_members: Users,
  create_tasks: Plus, update_task: Pencil, close_task: CheckCircle2, comment: MessageSquare,
};

// The model only sees text, so tasks the assistant touched travel with its reply as a short note.
function toApi(m: Msg) {
  const touched = (m.steps ?? []).filter((s) => !s.error && s.tasks?.length && s.tool !== "get_task");
  if (m.role !== "assistant" || !touched.length) return { role: m.role, content: m.content };
  const note = touched.map((s) => `${s.tool}: ${s.tasks!.map((t) => `${t.id} "${t.title}"`).join(", ")}`).join("; ");
  return { role: m.role, content: `${m.content}\n\n(Tasks I touched: ${note})` };
}

// AssistantPanel renders the chat as a right-hand panel on desktop and a bottom drawer on phones.
export function AssistantPanel({ me }: { me: Me }) {
  const a = useAssistant();
  const isDesktop = useDesktop();
  if (isDesktop) {
    if (!a.open) return null;
    return (
      <aside aria-label="Assistant" className="sticky top-0 flex h-dvh w-[22rem] shrink-0 flex-col border-l border-border bg-background xl:w-[25rem]">
        <Chat me={me} onClose={() => a.setOpen(false)} />
      </aside>
    );
  }
  return (
    <Drawer open={a.open} onOpenChange={a.setOpen}>
      <DrawerContent className="h-[88dvh] data-[vaul-drawer-direction=bottom]:max-h-[88dvh]">
        <DrawerTitle className="sr-only">Assistant</DrawerTitle>
        <DrawerDescription className="sr-only">Ask the workspace's agent to find or change tasks</DrawerDescription>
        <Chat me={me} onClose={() => a.setOpen(false)} drawer />
      </DrawerContent>
    </Drawer>
  );
}

function Chat({ me, onClose, drawer }: { me: Me; onClose: () => void; drawer?: boolean }) {
  const qc = useQueryClient();
  const status = useQuery({ queryKey: ["assistant"], queryFn: () => api.tool<Status>("get_assistant"), refetchInterval: false });
  const page = usePageContext();
  const [msgs, setMsgs] = useState<Msg[]>(load);
  const [input, setInput] = useState("");
  const [dropped, setDropped] = useState<string>(); // context the person removed from this chat
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const chatId = useRef(0);
  const withContext = dropped !== page.key;
  useEffect(() => save(msgs), [msgs]);
  useEffect(() => { if (!drawer) inputRef.current?.focus(); }, [drawer]);
  useEffect(() => endRef.current?.scrollIntoView({ block: "end" }), [msgs.length]);

  const send = useMutation({
    mutationFn: (v: { history: Msg[]; id: number }) =>
      api.tool<Reply>("assistant_chat", {
        messages: v.history.filter((m) => !m.error).map(toApi),
        task: withContext && page.kind === "task" ? page.task : undefined,
        project: withContext && page.kind === "project" ? page.project : undefined,
      }),
    onSuccess: (r, v) => {
      if (v.id !== chatId.current) return; // a new chat started meanwhile
      if (r.changed) qc.invalidateQueries();
      setMsgs((m) => [...m, { role: "assistant", content: r.reply, steps: r.steps }]);
    },
    onError: (e, v) => {
      if (v.id !== chatId.current) return;
      setMsgs((m) => [...m, { role: "assistant", content: e instanceof Error ? e.message : String(e), error: true }]);
    },
  });
  useEffect(() => endRef.current?.scrollIntoView({ block: "end" }), [send.isPending]);

  const ask = (text: string) => {
    const t = text.trim();
    if (!t || send.isPending) return;
    const history = [...msgs.filter((m) => !m.error), { role: "user" as const, content: t }];
    setMsgs([...msgs, { role: "user", content: t }]);
    setInput("");
    send.mutate({ history, id: chatId.current });
  };
  // Retry resends the conversation up to the failed answer.
  const retry = () => {
    const history = msgs.filter((m) => !m.error);
    setMsgs(history);
    send.mutate({ history, id: chatId.current });
  };
  const reset = () => {
    chatId.current++;
    send.reset();
    setMsgs([]);
    setInput("");
    setDropped(undefined);
    inputRef.current?.focus();
  };
  const prefill = (text: string) => {
    setInput(text);
    requestAnimationFrame(() => {
      const el = inputRef.current;
      el?.focus();
      el?.setSelectionRange(text.length, text.length);
    });
  };

  const agent = status.data?.agent;
  const first = me.member?.name.split(/\s+/)[0] ?? "";
  const off = status.data && !status.data.enabled;
  const lastFailed = msgs.at(-1)?.error;

  return (
    <div
      className="flex min-h-0 flex-1 flex-col"
      onKeyDown={(e) => { if (e.key === "Escape" && !drawer) { e.stopPropagation(); onClose(); } }}
    >
      <header className={cn("flex items-center gap-2 px-4", drawer ? "pt-2 pb-2" : "h-14 border-b border-border")}>
        <Sparkles className="size-4 text-violet-600" />
        <div className="min-w-0 flex-1">
          <div className="text-[15px] font-semibold leading-tight">Assistant</div>
          {agent && <div className="truncate text-xs text-muted-foreground">Acts as {agent.name} for you</div>}
        </div>
        {msgs.length > 0 && (
          <button onClick={reset} className="flex size-8 items-center justify-center rounded-full text-muted-foreground hover:bg-accent" aria-label="New chat" title="New chat">
            <SquarePen className="size-4" />
          </button>
        )}
        <button onClick={onClose} className="flex size-8 items-center justify-center rounded-full text-muted-foreground hover:bg-accent" aria-label="Close assistant" title={drawer ? "Close" : "Close (⌘J)"}>
          <X className="size-4" />
        </button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4" aria-live="polite">
        {off ? (
          <NotSetUp admin={me.member?.role === "admin"} onNavigate={drawer ? onClose : undefined} />
        ) : msgs.length === 0 ? (
          <Welcome first={first} page={page} onAsk={ask} onPrefill={prefill} />
        ) : (
          <div className="flex flex-col gap-4">
            {msgs.map((m, i) => (m.role === "user" ? <UserBubble key={i} text={m.content} /> : <AssistantMessage key={i} m={m} agent={agent} onNavigate={drawer ? onClose : undefined} />))}
            {send.isPending && <Thinking agent={agent} />}
            {lastFailed && !send.isPending && (
              <button onClick={retry} className="ml-8 flex w-fit items-center gap-1.5 rounded-full px-2 py-1 text-xs text-muted-foreground hover:bg-accent hover:text-foreground">
                <RotateCcw className="size-3.5" />Try again
              </button>
            )}
          </div>
        )}
        <div ref={endRef} />
      </div>

      {!off && (
        <form
          className={cn("mx-3 mt-1 flex flex-col gap-1.5 rounded-2xl border border-border bg-card p-1.5 shadow-sm focus-within:ring-2 focus-within:ring-input", drawer ? "mb-[calc(0.75rem+env(safe-area-inset-bottom))]" : "mb-3")}
          onSubmit={(e) => { e.preventDefault(); ask(input); }}
        >
          {withContext && page.kind !== "page" && (
            <span className="flex max-w-full items-center gap-1 self-start rounded-full bg-accent py-0.5 pr-1 pl-2 text-xs text-foreground/70">
              {page.kind === "task" ? <FileText className="size-3 shrink-0" /> : <FolderKanban className="size-3 shrink-0" />}
              <span className="truncate" title={`The assistant knows you're looking at ${page.label}`}>{page.label}</span>
              <button type="button" onClick={() => setDropped(page.key)} aria-label="Don't include this" title="Don't include this" className="flex size-4 shrink-0 items-center justify-center rounded-full text-muted-foreground hover:bg-foreground/10 hover:text-foreground">
                <X className="size-3" />
              </button>
            </span>
          )}
          <div className="flex items-end gap-2 pl-1.5">
            <textarea
              ref={inputRef}
              rows={1}
              value={input}
              placeholder={page.kind === "task" && withContext ? "Ask about this task…" : "Ask or tell the assistant…"}
              aria-label="Message the assistant"
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); ask(input); } }}
              className="max-h-40 min-h-9 flex-1 resize-none bg-transparent py-1.5 text-base outline-none [field-sizing:content] placeholder:text-muted-foreground/70 sm:text-sm"
            />
            <button disabled={!input.trim() || send.isPending || status.isLoading} aria-label="Send" className="flex size-9 shrink-0 items-center justify-center rounded-full bg-primary text-primary-foreground disabled:bg-foreground/10 disabled:text-muted-foreground/70">
              <ArrowUp className="size-4" />
            </button>
          </div>
        </form>
      )}
    </div>
  );
}

function NotSetUp({ admin, onNavigate }: { admin: boolean; onNavigate?: () => void }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-4 text-center">
      <span className="flex size-10 items-center justify-center rounded-full bg-accent"><Sparkles className="size-5 text-muted-foreground" /></span>
      <div>
        <div className="font-medium">The assistant isn't set up yet</div>
        <p className="mt-1 text-sm text-muted-foreground">It uses the workspace's triage agent and model. {admin ? "Choose them in Admin → Triage." : "Ask an admin to set up triage in Admin."}</p>
      </div>
      {admin && (
        <Link to="/admin" onClick={onNavigate} className="inline-flex h-8 items-center gap-1 rounded-full bg-primary px-3 text-sm font-medium text-primary-foreground">
          Set up triage<ChevronRight className="size-4" />
        </Link>
      )}
    </div>
  );
}

type Suggestion = { icon: typeof Search; title: string; subtitle: string; prompt: string; prefill?: boolean };

function Welcome({ first, page, onAsk, onPrefill }: { first: string; page: ReturnType<typeof usePageContext>; onAsk: (t: string) => void; onPrefill: (t: string) => void }) {
  const suggestions: Suggestion[] = [
    page.kind === "task"
      ? { icon: FileText, title: "Summarise this task", subtitle: "Where it stands and what's next", prompt: "Summarise this task: where it stands, what was discussed and what's next." }
      : { icon: AlertCircle, title: "What's overdue?", subtitle: page.kind === "project" ? "Late tasks in this project" : "Late tasks across your projects", prompt: page.kind === "project" ? "What's overdue in this project?" : "What's overdue? Group it by project." },
    { icon: CalendarDays, title: "My week", subtitle: "Your open tasks by due date", prompt: "What's on my plate this week? List my open tasks by due date." },
    { icon: ListPlus, title: "Plan some work", subtitle: "Turn a goal into tasks", prompt: page.kind === "project" ? "Create tasks in this project for: " : "Create tasks for: ", prefill: true },
  ];
  return (
    <div className="flex min-h-full flex-col justify-end gap-5 pb-2">
      <div>
        <span className="mb-3 flex size-9 items-center justify-center rounded-full bg-violet-100 text-violet-700"><Sparkles className="size-4.5" /></span>
        <h2 className="text-xl font-semibold tracking-tight">How can I help{first ? `, ${first}` : ""}?</h2>
        <p className="mt-1 text-sm text-muted-foreground">Ask about tasks, or ask me to create, move, close or comment on them.</p>
      </div>
      <div className="flex flex-col gap-2">
        {suggestions.map((s) => (
          <button key={s.title} onClick={() => (s.prefill ? onPrefill(s.prompt) : onAsk(s.prompt))} className="flex items-center gap-3 rounded-xl border border-border bg-card px-3 py-2.5 text-left transition-colors hover:bg-muted">
            <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-accent"><s.icon className="size-4 text-foreground/70" /></span>
            <span className="min-w-0">
              <span className="block text-sm font-medium">{s.title}</span>
              <span className="block truncate text-xs text-muted-foreground">{s.subtitle}</span>
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}

function UserBubble({ text }: { text: string }) {
  return <div className="ml-8 self-end rounded-2xl rounded-br-md bg-accent px-3 py-2 text-[15px] whitespace-pre-wrap sm:text-sm">{text}</div>;
}

const fallbackAgent: MemberRef = { id: "assistant", name: "Assistant", kind: "agent" };

function AssistantMessage({ m, agent, onNavigate }: { m: Msg; agent?: MemberRef; onNavigate?: () => void }) {
  return (
    <div className="flex gap-2.5">
      <Avatar m={agent ?? fallbackAgent} className="mt-0.5" />
      <div className="min-w-0 flex-1">
        {m.steps && m.steps.length > 0 && <Steps steps={m.steps} onNavigate={onNavigate} />}
        {m.error ? (
          <p className="flex items-start gap-1.5 text-sm text-red-600"><AlertCircle className="mt-0.5 size-4 shrink-0" />{m.content}</p>
        ) : (
          <Markdown text={m.content} className="text-[15px] leading-relaxed text-foreground/90 sm:text-sm" />
        )}
      </div>
    </div>
  );
}

// Steps lists the tool calls behind an answer. Changes start open so created tasks are one click away.
function Steps({ steps, onNavigate }: { steps: Step[]; onNavigate?: () => void }) {
  const wrote = steps.some((s) => !s.error && ["create_tasks", "update_task", "close_task", "comment"].includes(s.tool));
  const [open, setOpen] = useState(wrote);
  const failed = steps.filter((s) => s.error).length;
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="mb-1.5">
      <CollapsibleTrigger className="flex items-center gap-1 rounded-full py-0.5 pr-2 pl-1 text-xs text-muted-foreground hover:bg-accent hover:text-foreground">
        <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        {steps.length === 1 ? "1 step" : `${steps.length} steps`}
        {failed > 0 && <span className="text-amber-600">· {failed} failed</span>}
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-1 flex flex-col gap-1">
        {steps.map((s, i) => {
          const Icon = s.error ? AlertCircle : stepIcon[s.tool] ?? Check;
          return (
            <div key={i} className="rounded-lg bg-muted px-2 py-1.5 text-xs">
              <div className={cn("flex items-start gap-1.5", s.error ? "text-amber-700" : "text-foreground/70")}>
                <Icon className="mt-px size-3.5 shrink-0" />
                <span className="min-w-0">{s.summary}{s.error && <span className="text-muted-foreground"> · {s.error}</span>}</span>
              </div>
              {s.tasks && s.tasks.length > 0 && s.tool !== "search_tasks" && (
                <div className="mt-1 ml-5 flex flex-wrap gap-1" onClickCapture={onNavigate}>
                  {s.tasks.map((t) => (
                    <TaskLink key={t.id} id={t.id} className="max-w-full truncate rounded-full border border-border bg-card px-2 py-0.5 text-foreground/80 hover:bg-accent hover:text-foreground">
                      {t.title}
                    </TaskLink>
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </CollapsibleContent>
    </Collapsible>
  );
}

function Thinking({ agent }: { agent?: MemberRef }) {
  return (
    <div className="flex items-center gap-2.5" role="status">
      <Avatar m={agent ?? fallbackAgent} className="agent-working" />
      <span className="agent-shimmer text-sm">{agent ? `${agent.name} is working on it…` : "Thinking…"}</span>
    </div>
  );
}
