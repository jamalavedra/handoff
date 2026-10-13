import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

// useLive keeps every open query fresh: the server pushes "task X changed" and we refetch.
export function useLive(enabled: boolean) {
  const qc = useQueryClient();
  useEffect(() => {
    if (!enabled || typeof EventSource === "undefined") return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const refresh = () => {
      clearTimeout(timer);
      timer = setTimeout(() => qc.invalidateQueries(), 150);
    };
    const es = new EventSource("/api/live");
    let lost = false;
    es.onmessage = (e) => {
      try {
        const m = JSON.parse(e.data) as { kind: string; task?: string };
        if (m.kind === "presence" && m.task) return void qc.invalidateQueries({ queryKey: ["task", m.task] });
      } catch {
        // not JSON: refresh everything
      }
      refresh();
    };
    es.onerror = () => { lost = true; };
    es.onopen = () => { if (lost) refresh(); lost = false; };
    return () => { clearTimeout(timer); es.close(); };
  }, [enabled, qc]);
}
