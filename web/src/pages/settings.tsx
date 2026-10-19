import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { api, cn } from "../lib";
import { PageHeader } from "../components/app";
import { useMe } from "../main";

// SettingsHeader is the top of every settings page: the title and its sections.
// General (workspace, people, triage) is for admins; Agents and Activity are for everyone.
export function SettingsHeader({ active }: { active: "general" | "agents" | "memory" | "apps" | "devices" | "connections" | "activity" }) {
  const admin = useMe().data?.member?.role === "admin";
  // Same query as the sidebar badge, so every section shows pending agent requests.
  const approvals = useQuery({ queryKey: ["approvals"], queryFn: () => api.tool<{ approvals: unknown[] }>("list_approvals") });
  const pending = approvals.data?.approvals.length ?? 0;
  const tabs = [
    ...(admin ? [{ key: "general", to: "/settings", label: "General" }] : []),
    { key: "agents", to: "/settings/agents", label: "Agents" },
    { key: "memory", to: "/settings/memory", label: "Memory" },
    { key: "apps", to: "/settings/apps", label: "Apps" },
    { key: "devices", to: "/settings/devices", label: "Devices" },
    ...(admin ? [{ key: "connections", to: "/settings/connections", label: "Connections" }] : []),
    { key: "activity", to: "/settings/activity", label: "Activity" },
  ];
  return (
    <>
      <PageHeader title="Settings" />
      <nav aria-label="Settings" className="-mt-1 mb-6 flex gap-1.5">
        {tabs.map((t) => (
          <Link
            key={t.key}
            to={t.to}
            aria-current={active === t.key ? "page" : undefined}
            className={cn("flex h-8 items-center gap-1.5 rounded-full px-3 text-[13px] font-medium transition-colors", active === t.key ? "bg-accent text-foreground" : "text-foreground/50 hover:text-foreground/80")}
          >
            {t.label}
            {t.key === "agents" && pending > 0 && <span className="rounded-full bg-amber-500 px-1.5 text-[11px] leading-4 text-white">{pending}</span>}
          </Link>
        ))}
      </nav>
    </>
  );
}
