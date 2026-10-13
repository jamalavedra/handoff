import { useSyncExternalStore } from "react";
import { Link, useRouterState, useSearch } from "@tanstack/react-router";

const wide = window.matchMedia("(min-width: 1024px)");

// useWide is true where tasks open in the side panel instead of a full page.
export function useWide() {
  return useSyncExternalStore(
    (l) => {
      wide.addEventListener("change", l);
      return () => wide.removeEventListener("change", l);
    },
    () => wide.matches,
  );
}

export function usePanelTask() {
  return useSearch({ strict: false }).task;
}

export function TaskLink({ id, className, children }: { id: string; className?: string; children: React.ReactNode }) {
  const isWide = useWide();
  const onTaskPage = useRouterState({ select: (s) => s.location.pathname.startsWith("/tasks/") });
  if (!isWide || onTaskPage) return <Link to="/tasks/$id" params={{ id }} className={className}>{children}</Link>;
  return <Link to="." search={(p) => ({ ...p, task: id })} className={className}>{children}</Link>;
}
