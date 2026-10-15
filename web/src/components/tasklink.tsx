import { Link } from "@tanstack/react-router";
import { taskHref } from "../tabs";
import { useOpenInTab } from "./tabs";

// TaskLink opens a task's full page; ⌘/Ctrl+click or middle-click opens it in a background tab on desktop.
export function TaskLink({ id, className, children }: { id: string; className?: string; children: React.ReactNode }) {
  const tab = useOpenInTab(taskHref(id));
  return <Link to="/tasks/$id" params={{ id }} className={className} {...tab}>{children}</Link>;
}
