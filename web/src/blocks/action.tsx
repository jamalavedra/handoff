import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Loader2, Send, X } from "lucide-react";
import { toast } from "sonner";
import { api, cn } from "../lib";

export type Action = {
  id: string; app: string; title: string; verb?: string; status: "pending" | "sending" | "sent" | "failed" | "cancelled";
  fields?: { label: string; value: string }[]; result?: string; mine: boolean;
};

const statusText: Record<Action["status"], string> = { pending: "", sending: "Sending…", sent: "Sent", failed: "Didn't go through", cancelled: "Cancelled" };

// ActionBlock: something the agent prepared in someone's app (an email, a DM, a post, an event).
// Every field that would go out is shown, and it runs only when its owner approves it.
export function ActionBlock({ data }: { data: Action }) {
  const qc = useQueryClient();
  const key = ["app_action", data.id];
  // The message only says what the card is; its contents (for its owner), and how it went, come from
  // the server, checked again while it waits so another tab's tap shows here too.
  const q = useQuery({
    queryKey: key,
    queryFn: () => api.tool<Action>("get_app_action", { id: data.id }),
    refetchInterval: (s) => (s.state.data?.status === "sending" ? 2000 : s.state.data?.status === "pending" ? 15000 : false),
  });
  const a = q.data ?? { ...data, mine: false };
  const decide = useMutation({
    mutationFn: (approve: boolean) => api.tool<Action>("decide_app_action", { id: data.id, approve }),
    onSuccess: (r) => {
      qc.setQueryData(key, r);
      if (r.status === "sent") toast.success(`${r.title}: done`);
      else if (r.status === "failed") toast.error(r.result || "It didn't go through");
    },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  });
  const pending = a.status === "pending";
  return (
    <div className="rounded-xl border border-border bg-card p-3">
      <div className="flex items-center gap-2 text-sm">
        <Send className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="font-medium">{a.title}</span>
        <span className="text-muted-foreground">· {a.app}</span>
        {!pending && (
          <span className={cn("ml-auto flex items-center gap-1 text-xs", a.status === "sent" ? "text-green-700 dark:text-green-500" : a.status === "failed" ? "text-amber-700 dark:text-amber-500" : "text-muted-foreground")}>
            {a.status === "sent" && <Check className="size-3.5" />}{a.status === "sending" && <Loader2 className="size-3.5 animate-spin" />}{statusText[a.status]}
          </span>
        )}
      </div>
      {!!a.fields?.length && <dl className="mt-2 flex flex-col gap-1.5 border-l-2 border-border pl-3">
        {a.fields.map((f) => (
          <div key={f.label} className="min-w-0">
            <dt className="text-[11px] tracking-wide text-muted-foreground uppercase">{f.label}</dt>
            <dd className="text-sm break-words whitespace-pre-wrap">{f.value}</dd>
          </div>
        ))}
      </dl>}
      {a.status === "failed" && a.result && <p className="mt-2 text-xs text-amber-700 dark:text-amber-500">{a.result}</p>}
      {pending && (a.mine ? (
        <div className="mt-3 flex gap-2">
          <button type="button" disabled={decide.isPending} onClick={() => decide.mutate(true)} className="flex h-8 items-center gap-1.5 rounded-full bg-primary px-3.5 text-xs font-medium text-primary-foreground transition-transform active:scale-[0.97] disabled:opacity-60">
            {decide.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Send className="size-3.5" />}{a.verb || "Send"}
          </button>
          <button type="button" disabled={decide.isPending} onClick={() => decide.mutate(false)} className="flex h-8 items-center gap-1.5 rounded-full px-3 text-xs font-medium text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-60">
            <X className="size-3.5" />Cancel
          </button>
        </div>
      ) : (
        <p className="mt-2 text-xs text-muted-foreground">{q.isLoading ? "…" : "Waiting for its owner to send it."}</p>
      ))}
    </div>
  );
}
