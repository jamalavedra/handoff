import { useMutation, useQuery } from "@tanstack/react-query";
import { Pin, PinOff, Trash2 } from "lucide-react";
import { ago, api, cn } from "../lib";
import { toast } from "sonner";
import { Card, Empty, ErrorText } from "../components/app";
import { ConfirmButton, SettingsSection } from "../components/settings";
import { Button } from "@/components/ui/button";
import { useMe } from "../main";
import { SettingsHeader } from "./settings";
import { useInvalidate } from "./tasks";

// A fact the agent keeps between jobs. workspace facts load for everyone, project facts for
// whoever can see the project, person facts only for that person.
type MemoryFact = {
  id: string;
  scope: "workspace" | "project" | "person";
  text: string;
  project?: string;
  member?: { id: string; name: string };
  pinned?: boolean;
  until?: string;
  noticed?: boolean;
  created_at: string;
};

function FactRow({ f, canManage }: { f: MemoryFact; canManage: boolean }) {
  const invalidate = useInvalidate();
  const pin = useMutation({
    mutationFn: () => api.tool("pin_memory", { id: f.id, pinned: !f.pinned }),
    onSuccess: invalidate,
    onError: (e) => toast.error(e instanceof Error ? e.message : String(e)),
  });
  return (
    <div className="flex items-center gap-3 border-b border-border px-4 py-3 text-sm last:border-0">
      {canManage && (
        <Button
          size="icon-xs"
          variant="ghost"
          aria-label={f.pinned ? "Unpin" : "Pin"}
          title={f.pinned ? "Unpin: it loads with the rest" : "Pin: it always loads first"}
          className="size-7 text-foreground/50"
          onClick={() => pin.mutate()}
        >
          {f.pinned ? <PinOff className="size-3.5" /> : <Pin className="size-3.5" />}
        </Button>
      )}
      <div className="min-w-0 flex-1">
        <span className={cn(f.pinned && "font-medium")}>{f.text}</span>
        <div className="mt-0.5 text-xs text-muted-foreground">
          {f.until && <>until {f.until} · </>}
          {f.noticed && <>noticed by the agent · </>}
          {ago(f.created_at)}
        </div>
      </div>
      {canManage && (
        <ConfirmButton
          size="xs"
          icon={Trash2}
          label="Remove"
          title="Remove this fact?"
          description="The agent stops remembering it right away. It stays on the record but never loads again."
          confirmLabel="Remove"
          onConfirm={async () => { await api.tool("remove_memory", { id: f.id }); invalidate(); }}
        />
      )}
    </div>
  );
}

function FactGroup({ title, facts, canManage }: { title: string; facts: MemoryFact[]; canManage: (f: MemoryFact) => boolean }) {
  if (!facts.length) return null;
  return (
    <>
      <div className="border-b border-border bg-accent/40 px-4 py-1.5 text-[11px] font-semibold uppercase tracking-wide text-foreground/50">{title}</div>
      {facts.map((f) => <FactRow key={f.id} f={f} canManage={canManage(f)} />)}
    </>
  );
}

export function MemoryPage() {
  const me = useMe().data?.member;
  const admin = me?.role === "admin";
  const q = useQuery({ queryKey: ["memory"], queryFn: () => api.tool<{ facts: MemoryFact[] }>("list_memory") });
  const facts = q.data?.facts ?? [];
  // People manage their own person facts; admins manage workspace and project facts.
  const canManage = (f: MemoryFact) => (f.scope === "person" ? f.member?.id === me?.id : admin);
  const projects = [...new Set(facts.filter((f) => f.scope === "project").map((f) => f.project ?? ""))];
  return (
    <>
      <SettingsHeader active="memory" />
      <SettingsSection
        title="What the agent knows"
        description="Facts the workspace's agent remembers between jobs. They load into its turns — pinned ones first. Remove a fact and it never loads again."
      >
        <ErrorText error={q.error} />
        <Card className="overflow-hidden">
          {facts.length === 0 && !q.isLoading ? (
            <Empty>
              Nothing remembered yet. Ask the assistant to remember something ("remember that I prefer…"), or it'll note things as it works.
            </Empty>
          ) : (
            <>
              <FactGroup title="Workspace" facts={facts.filter((f) => f.scope === "workspace")} canManage={canManage} />
              {projects.map((p) => (
                <FactGroup key={p} title={p} facts={facts.filter((f) => f.scope === "project" && (f.project ?? "") === p)} canManage={canManage} />
              ))}
              <FactGroup title="About you" facts={facts.filter((f) => f.scope === "person")} canManage={canManage} />
            </>
          )}
        </Card>
      </SettingsSection>
    </>
  );
}
