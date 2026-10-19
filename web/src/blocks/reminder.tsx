import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Bell, CalendarCheck, CalendarPlus, Download } from "lucide-react";
import { toast } from "sonner";
import { api, cn, type Task } from "../lib";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { daysLeft, googleCalendarURL, ics, localDate, when, type Reminder } from "./artifacts";

// ReminderBlock: a date the agent says to remember, as a card like a calendar page. It goes to the
// person's calendar in a tap, or becomes the task's due date.
export function ReminderBlock({ data }: { data: Reminder }) {
  const qc = useQueryClient();
  const task = data.task ? qc.getQueryData<{ task: Task }>(["task", data.task])?.task : undefined;
  const day = data.at.slice(0, 10);
  const isDue = task?.due?.slice(0, 10) === day;
  const setDue = useMutation({
    mutationFn: () => api.tool("update_task", { id: data.task, due: day }),
    onSuccess: () => { toast.success("Due date set"); qc.invalidateQueries({ queryKey: ["task"] }); qc.invalidateQueries({ queryKey: ["tasks"] }); },
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  });
  if (!data?.at) return null;
  const d = localDate(data.at);
  const left = daysLeft(data);
  const download = () => {
    const url = URL.createObjectURL(new Blob([ics(data, `${data.task ?? "reminder"}-${data.at}`)], { type: "text/calendar" }));
    const a = Object.assign(document.createElement("a"), { href: url, download: `${data.title.replace(/[^\w -]+/g, "").trim() || "reminder"}.ics` });
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  return (
    <div className="flex items-center gap-3 rounded-xl border border-border bg-card p-2.5">
      <span className="flex w-12 shrink-0 flex-col items-center overflow-hidden rounded-lg border border-border text-center" aria-hidden>
        <span className="w-full bg-violet-600 py-0.5 text-[10px] font-semibold tracking-wide text-white uppercase">{d.toLocaleDateString(undefined, { month: "short" })}</span>
        <span className="py-0.5 text-lg leading-tight font-semibold tabular-nums">{d.getDate()}</span>
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-1 text-sm font-medium"><Bell className="size-3.5 shrink-0 text-violet-600" /><span className="truncate">{data.title}</span></span>
        <span className={cn("block text-xs", left >= 0 && left <= 2 ? "text-amber-700 dark:text-amber-400" : "text-muted-foreground")}>
          {when(data)}{left > 2 && ` · in ${left} days`}
        </span>
        {data.note && <span className="mt-0.5 line-clamp-2 block text-xs text-muted-foreground">{data.note}</span>}
      </span>
      <span className="flex shrink-0 flex-col items-end gap-1">
        <Popover>
          <PopoverTrigger asChild>
            <button type="button" className="flex h-8 items-center gap-1.5 rounded-full bg-primary px-3 text-xs font-medium text-primary-foreground transition-transform active:scale-[0.97]">
              <CalendarPlus className="size-3.5" />Remind me
            </button>
          </PopoverTrigger>
          <PopoverContent align="end" className="flex w-52 flex-col gap-0.5 p-1">
            <a href={googleCalendarURL(data)} target="_blank" rel="noreferrer noopener" className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-accent">
              <CalendarPlus className="size-4 text-muted-foreground" />Google Calendar
            </a>
            <button type="button" onClick={download} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent">
              <Download className="size-4 text-muted-foreground" />Apple / Outlook (.ics)
            </button>
            {data.task && (
              <button type="button" disabled={isDue || setDue.isPending} onClick={() => setDue.mutate()} className="flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent disabled:opacity-60 disabled:hover:bg-transparent">
                <CalendarCheck className="size-4 text-muted-foreground" />{isDue ? "It's the due date" : "Make it the due date"}
              </button>
            )}
          </PopoverContent>
        </Popover>
      </span>
    </div>
  );
}
