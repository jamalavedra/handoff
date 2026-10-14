import { useSyncExternalStore } from "react";
import { Link, useRouterState, useSearch } from "@tanstack/react-router";
import { taskHref } from "../tabs";
import { useOpenInTab } from "./tabs";

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

// TaskLink opens a task in place; ⌘/Ctrl+click or middle-click opens it in a background tab on desktop.
export function TaskLink({ id, className, children }: { id: string; className?: string; children: React.ReactNode }) {
  const isWide = useWide();
  const onTaskPage = useRouterState({ select: (s) => s.location.pathname.startsWith("/tasks/") });
  const tab = useOpenInTab(taskHref(id));
  if (!isWide || onTaskPage) return <Link to="/tasks/$id" params={{ id }} className={className} {...tab}>{children}</Link>;
  return <Link to="." search={(p) => ({ ...p, task: id })} className={className} {...tab}>{children}</Link>;
}
