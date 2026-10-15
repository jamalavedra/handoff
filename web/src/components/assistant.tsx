import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useMedia } from "@/hooks/use-mobile";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useRouterState } from "@tanstack/react-router";
import { AlertCircle, ArrowUp, CalendarDays, Check, ChevronDown, ChevronRight, FileText, FolderKanban, ListPlus, MessageSquare, RotateCcw, Search, Sparkles, SquarePen, X } from "lucide-react";
import { api, cn, projectName, type Me, type MemberRef, type Project, type Task } from "../lib";
import { parseStored, stored, toApi, type Msg, type Step } from "../assistant-history";
import { Markdown } from "./markdown";
import { Avatar } from "./app";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { TaskChat } from "./task-chat";
import { Blocks, type Block } from "../blocks";
import { ActivityTrail } from "./activity-trail";
import { useAgentActivity } from "../agent-activity";
import { fromSteps } from "../activity";
import { Drawer, DrawerContent, DrawerDescription, DrawerTitle } from "@/components/ui/drawer";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

// The assistant is a chat with the workspace's triage agent: it answers questions about tasks and
// makes changes as that agent, for the person asking. The conversation lives in this tab only.

type Status = { enabled: boolean; agent?: MemberRef };
type Reply = { reply: string; steps: Step[]; blocks?: Block[]; agent?: MemberRef; changed: boolean; error?: string };

// The panel shows one of two conversations: the open task's chat with the team and its agents
// ("task"), or this person's private assistant across the workspace ("global").
export type Mode = "task" | "global";
type Ctx = {
  open: boolean; setOpen: (o: boolean) => void; toggle: () => void;
  mode: Mode; setMode: (m: Mode) => void;
  focus: number; // bumped to put the cursor in the composer
  openTaskChat: () => void;
};
const AssistantContext = createContext<Ctx>({ open: false, setOpen: () => {}, toggle: () => {}, mode: "task", setMode: () => {}, focus: 0, openTaskChat: () => {} });
export const useAssistant = () => useContext(AssistantContext);

const desktop = window.matchMedia("(min-width: 768px)");
const roomy = window.matchMedia("(min-width: 1280px)");

// From the sidebar breakpoint up the assistant is a side panel; below it, a drawer.
const useDesktop = () => useMedia(desktop);

// useTaskChatShown: the task's chat is already beside it (desktop panel open on "This task").
export function useTaskChatShown() {
  const a = useAssistant();
  const isDesktop = useDesktop();
  return a.open && a.mode === "task" && isDesktop;
}

// useAssistantInset is how far the open side panel pushes things anchored to the right edge (toasts).
export function useAssistantInset() {
  const a = useAssistant();
  const isDesktop = useDesktop();
  const xl = useMedia(roomy);
  if (!a.open || !isDesktop) return undefined;
  return xl ? "calc(25rem + 24px)" : "calc(22rem + 24px)";
}

const storeKey = "handoff-assistant";

// The conversation is kept per tab, and only shown to the member (in the workspace) who had it.
function load(org: string, member: string): Msg[] {
  try {
    return parseStored(sessionStorage.getItem(storeKey), org, member);
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

function save(org: string, member: string, msgs: Msg[]) {
  try {
    sessionStorage.setItem(storeKey, stored(org, member, msgs));
  } catch {
    // private mode or full: the chat just won't survive a reload
  }
}

// AssistantProvider owns the open state and ⌘J. The panel itself is rendered by <AssistantPanel />.
const modeKey = "handoff-assistant-mode";
const savedMode = (): Mode => {
  try {
    return localStorage.getItem(modeKey) === "global" ? "global" : "task";
  } catch {
    return "task";
  }
};

export function AssistantProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const [mode, setModeState] = useState<Mode>(savedMode);
  const [focus, setFocus] = useState(0);
  const toggle = useCallback(() => setOpen((o) => !o), []);
  const setMode = useCallback((m: Mode) => {
    setModeState(m);
    try { localStorage.setItem(modeKey, m); } catch { /* private window */ }
  }, []);
  const openTaskChat = useCallback(() => { setMode("task"); setOpen(true); setFocus((f) => f + 1); }, [setMode]);
  const ctx = useMemo(() => ({ open, setOpen, toggle, mode, setMode, focus, openTaskChat }), [open, toggle, mode, setMode, focus, openTaskChat]);
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

// useFab says whether the floating assistant button shows: not while the panel is open, and not
// while a task is open (its chat bar opens the panel). Phones: above the tab bar; desktop: the corner.
export function useFab() {
  const a = useAssistant();
  const path = useRouterState({ select: (s) => s.location.pathname });
  const onTask = path.startsWith("/tasks/");
  return { shown: !a.open && !onTask, onTask };
}

// AssistantFab is the round button that opens the assistant from any page.
export function AssistantFab() {
  const a = useAssistant();
  const fab = useFab();
  if (!fab.shown) return null;
  return (
    <TooltipProvider delayDuration={300}>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            onClick={() => a.setOpen(true)}
            aria-label="Assistant"
            aria-keyshortcuts="Meta+J"
            className={cn(
              "fixed right-4 bottom-[calc(5.75rem+env(safe-area-inset-bottom))] z-30 flex size-12 items-center justify-center rounded-full border border-border bg-card text-violet-600 shadow-lg transition-transform hover:scale-105 hover:bg-[linear-gradient(var(--accent),var(--accent))] active:scale-95 md:right-6 md:bottom-6 md:size-11",
            )}
          >
            <Sparkles className="size-5" />
          </button>
        </TooltipTrigger>
        <TooltipContent side="left" sideOffset={8}>
          Assistant <kbd className="ml-1 font-sans opacity-70">⌘J</kbd>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

// usePageContext is what the person is looking at: the open task, or the current project or page.
function usePageContext() {
  const path = useRouterState({ select: (s) => s.location.pathname });
  const task = path.match(/^\/tasks\/([^/]+)/)?.[1];
  const project = task ? undefined : path.match(/^\/projects\/([^/]+)/)?.[1];
  const t = useQuery({ queryKey: ["task", task], queryFn: () => api.tool<{ task: Task }>("get_task", { id: task }), enabled: !!task, refetchInterval: false });
  const projects = useQuery({ queryKey: ["projects"], queryFn: () => api.tool<{ projects: Project[] }>("list_projects") });
  const p = projects.data?.projects.find((x) => x.id === project);
  const pages: Record<string, string> = { "/": "My tasks", "/notifications": "Notifications", "/team": "Team", "/projects": "Projects", "/automations": "Automations", "/settings/activity": "Activity", "/settings/agents": "Agents", "/settings": "Settings", "/trash": "Trash", "/admin": "Admin" };
  if (task) return { key: `task:${task}`, kind: "task" as const, task, label: t.data?.task.title ?? task };
  if (project) return { key: `project:${project}`, kind: "project" as const, project, label: p ? projectName(p) : "Project" };
  return { key: `page:${path}`, kind: "page" as const, label: pages[path] ?? "Handoff" };
}

const newTurn = () => (typeof crypto.randomUUID === "function" ? crypto.randomUUID() : `${Date.now()}-${Math.random().toString(36).slice(2)}`);

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

// Chat is the panel: a header with the conversation switcher, then the task's chat or the assistant.
function Chat({ me, onClose, drawer }: { me: Me; onClose: () => void; drawer?: boolean }) {
  const a = useAssistant();
  const page = usePageContext();
  const taskMode = a.mode === "task" && page.kind === "task";
  return (
    <div
      className="flex min-h-0 flex-1 flex-col"
      onKeyDown={(e) => { if (e.key === "Escape" && !drawer && !e.defaultPrevented) { e.stopPropagation(); onClose(); } }}
    >
      {/* Both stay mounted while the panel is open, so switching never drops a reply on its way or
          a draft; only the one in view shows. */}
      {page.kind === "task" && (
        <div className={cn("flex min-h-0 flex-1 flex-col", !taskMode && "hidden")}>
          <PanelHeader drawer={drawer} onClose={onClose} page={page} taskMode />
          <TaskChat key={page.task} id={page.task!} focus={a.focus} drawer={drawer} active={taskMode} />
        </div>
      )}
      <div className={cn("flex min-h-0 flex-1 flex-col", taskMode && "hidden")}>
        <AssistantChat me={me} onClose={onClose} drawer={drawer} page={page} active={!taskMode} />
      </div>
    </div>
  );
}

// PanelHeader names the conversation and switches between the task's chat and the assistant.
function PanelHeader({ drawer, onClose, page, taskMode, sub, actions }: { drawer?: boolean; onClose: () => void; page: ReturnType<typeof usePageContext>; taskMode: boolean; sub?: string; actions?: React.ReactNode }) {
  const a = useAssistant();
  const [open, setOpen] = useState(false);
  const pick = (m: Mode) => { a.setMode(m); setOpen(false); };
  return (
    <header className={cn("flex items-center gap-2 px-4", drawer ? "pt-2 pb-2" : "h-14 border-b border-border")}>
      {taskMode ? <MessageSquare className="size-4 shrink-0 text-muted-foreground" /> : <Sparkles className="size-4 shrink-0 text-violet-600" />}
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button className="-ml-1 flex min-w-0 flex-1 items-center gap-1 rounded-lg px-1 py-0.5 text-left hover:bg-accent" aria-label="Switch conversation">
            <span className="min-w-0">
              <span className="block truncate text-[15px] font-semibold leading-tight">{taskMode ? "Task chat" : "Assistant"}</span>
              <span className="block truncate text-xs text-muted-foreground">{taskMode ? page.label : sub ?? "Across the workspace"}</span>
            </span>
            <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" className="w-72 p-1">
          <ModeItem icon={MessageSquare} active={taskMode} disabled={page.kind !== "task"} onClick={() => pick("task")} title="This task" hint={page.kind === "task" ? `Shared with the team · ${page.label}` : "Open a task to see its chat"} />
          <ModeItem icon={Sparkles} active={!taskMode} onClick={() => pick("global")} title="Everything" hint="Your private assistant, across the workspace" />
        </PopoverContent>
      </Popover>
      {actions}
      <button onClick={onClose} className="flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground hover:bg-accent" aria-label="Close panel" title={drawer ? "Close" : "Close (⌘J)"}>
        <X className="size-4" />
      </button>
    </header>
  );
}

function ModeItem({ icon: Icon, active, disabled, onClick, title, hint }: { icon: typeof Search; active: boolean; disabled?: boolean; onClick: () => void; title: string; hint: string }) {
  return (
    <button disabled={disabled} onClick={onClick} className="flex w-full items-start gap-2.5 rounded-md px-2.5 py-2 text-left hover:bg-accent disabled:opacity-50 disabled:hover:bg-transparent">
      <Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium">{title}</span>
        <span className="block truncate text-xs text-muted-foreground">{hint}</span>
      </span>
      {active && <Check className="mt-0.5 size-4 shrink-0" />}
    </button>
  );
}

function AssistantChat({ me, onClose, drawer, page, active }: { me: Me; onClose: () => void; drawer?: boolean; page: ReturnType<typeof usePageContext>; active: boolean }) {
  const qc = useQueryClient();
  const status = useQuery({ queryKey: ["assistant"], queryFn: () => api.tool<Status>("get_assistant"), refetchInterval: false });
  const org = me.org?.slug ?? "";
  const member = me.member?.id ?? "";
  const [msgs, setMsgs] = useState<Msg[]>(() => load(org, member));
  const [input, setInput] = useState("");
  const [dropped, setDropped] = useState<string>(); // context the person removed from this chat
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const chatId = useRef(0);
  const withContext = dropped !== page.key;
  useEffect(() => save(org, member, msgs), [org, member, msgs]);
  useEffect(() => { if (!drawer && active) inputRef.current?.focus(); }, [drawer, active]);
  useEffect(() => endRef.current?.scrollIntoView({ block: "end" }), [msgs.length]);

  const send = useMutation({
    mutationFn: (v: { history: Msg[]; turn: string; id: number }) =>
      api.tool<Reply>("assistant_chat", {
        turn: v.turn,
        messages: v.history.filter((m) => !m.error).map(toApi),
        task: withContext && page.kind === "task" ? page.task : undefined,
        project: withContext && page.kind === "project" ? page.project : undefined,
      }),
    onSuccess: (r, v) => {
      if (v.id !== chatId.current) return; // a new chat started meanwhile
      if (r.changed) qc.invalidateQueries();
      // A turn that stopped early still shows what it did; Try again resumes it on the server.
      setMsgs((m) => [...m, r.error ? { role: "assistant", content: r.error, steps: r.steps, error: true } : { role: "assistant", content: r.reply, steps: r.steps, blocks: r.blocks }]);
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
    const turn = newTurn();
    const history: Msg[] = [...msgs.filter((m) => !m.error), { role: "user", content: t, turn }];
    setMsgs([...msgs, { role: "user", content: t, turn }]);
    setInput("");
    send.mutate({ history, turn, id: chatId.current });
  };
  // Retry sends the same turn again: the server carries on from what it already did instead of redoing it.
  const retry = () => {
    const history = msgs.filter((m) => !m.error);
    setMsgs(history);
    send.mutate({ history, turn: history.at(-1)?.turn ?? newTurn(), id: chatId.current });
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
    <>
      <PanelHeader
        drawer={drawer}
        onClose={onClose}
        page={page}
        taskMode={false}
        sub={agent ? `Private · acts as ${agent.name} for you` : undefined}
        actions={msgs.length > 0 && (
          <button onClick={reset} className="flex size-8 shrink-0 items-center justify-center rounded-full text-muted-foreground hover:bg-accent" aria-label="New chat" title="New chat">
            <SquarePen className="size-4" />
          </button>
        )}
      />

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4" aria-live="polite">
        {off ? (
          <NotSetUp admin={me.member?.role === "admin"} onNavigate={drawer ? onClose : undefined} />
        ) : msgs.length === 0 ? (
          <Welcome first={first} page={page} onAsk={ask} onPrefill={prefill} />
        ) : (
          <div className="flex flex-col gap-4">
            {msgs.map((m, i) => (m.role === "user" ? <UserBubble key={i} text={m.content} /> : <AssistantMessage key={i} m={m} agent={agent} onNavigate={drawer ? onClose : undefined} />))}
            {send.isPending && <Thinking agent={agent} turn={send.variables?.turn} onNavigate={drawer ? onClose : undefined} />}
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
    </>
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
        <Link to="/settings" onClick={onNavigate} className="inline-flex h-8 items-center gap-1 rounded-full bg-primary px-3 text-sm font-medium text-primary-foreground">
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
        {m.error ? (
          <p className="flex items-start gap-1.5 text-sm text-red-600"><AlertCircle className="mt-0.5 size-4 shrink-0" />{m.content}</p>
        ) : (
          <Markdown text={m.content} className="text-[15px] leading-relaxed text-foreground/90 sm:text-sm" />
        )}
        <Blocks blocks={m.blocks} />
        {m.steps && m.steps.length > 0 && <ActivityTrail steps={fromSteps(m.steps)} onNavigate={onNavigate} className="mt-1.5" />}
      </div>
    </div>
  );
}

// Thinking stands in for the reply while the turn runs, with the steps so far as they happen.
function Thinking({ agent, turn, onNavigate }: { agent?: MemberRef; turn?: string; onNavigate?: () => void }) {
  const live = useAgentActivity({ turn });
  return (
    <div className="flex gap-2.5" role="status">
      <Avatar m={agent ?? fallbackAgent} className="agent-working" />
      <div className="min-w-0 flex-1">
        <span className="agent-shimmer text-sm">{agent ? `${agent.name} is working on it…` : "Thinking…"}</span>
        <ActivityTrail steps={live.steps} live onNavigate={onNavigate} className="mt-1.5" />
      </div>
    </div>
  );
}
