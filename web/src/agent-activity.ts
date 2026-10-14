import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type MemberRef } from "./lib";
import { onProgress } from "./live";
import { advance, follows, idle, line, type Trail } from "./activity";

// A turn that goes quiet this long without ending (a lost connection, a server restart) stops showing.
const quietFor = 90_000;

// useAgentActivity follows the agent at work on one assistant turn or one task: its steps so far, and
// whether it's still working. It empties when the turn ends; the reply carries its own trail.
export function useAgentActivity({ task, turn }: { task?: string; turn?: string }) {
  const key = `${task ?? ""}|${turn ?? ""}`;
  const [state, setState] = useState<{ key: string; trail: Trail }>({ key, trail: idle });
  useEffect(() => {
    if (!task && !turn) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const off = onProgress((m) => {
      if (!follows(m, { task, turn })) return;
      setState((s) => ({ key, trail: advance(s.key === key ? s.trail : idle, m) }));
      clearTimeout(timer);
      timer = setTimeout(() => setState({ key, trail: idle }), quietFor);
    });
    return () => { off(); clearTimeout(timer); };
  }, [key, task, turn]);
  const trail = state.key === key ? state.trail : idle;
  return { steps: trail.steps, working: trail.working };
}

// useAgentLine is one line for a task while the agent works on it, e.g. "Acme Bot · Web · “venues”".
export function useAgentLine(task?: string) {
  const live = useAgentActivity({ task });
  const status = useQuery({ queryKey: ["assistant"], queryFn: () => api.tool<{ enabled: boolean; agent?: MemberRef }>("get_assistant"), refetchInterval: false, enabled: !!task });
  if (!live.working) return undefined;
  const last = live.steps.at(-1);
  const who = status.data?.agent?.name ?? "The agent";
  return last ? `${who} · ${line(last)}` : `${who} is working…`;
}
