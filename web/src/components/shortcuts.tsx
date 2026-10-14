import { useEffect } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";

export function typing(t: EventTarget | null) {
  const el = t as HTMLElement | null;
  return !!el && (el.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName));
}

const jumps: Record<string, string> = { t: "/", n: "/notifications", p: "/projects", a: "/agents" };

// When G was last pressed. Other single-key handlers (N for new task) stand down during a jump,
// whichever listener happens to run first.
let gAt = 0;
export const inJump = () => Date.now() - gAt < 1000;

// Rows opt in with data-task-row; the focused one carries data-kbd-focus.
function rows() {
  return [...document.querySelectorAll<HTMLElement>("[data-task-row]")].filter((el) => el.offsetParent !== null);
}

function focusRow(next: HTMLElement | undefined) {
  document.querySelectorAll("[data-kbd-focus]").forEach((el) => el.removeAttribute("data-kbd-focus"));
  if (!next) return;
  next.setAttribute("data-kbd-focus", "");
  next.scrollIntoView({ block: "nearest" });
}

// useShortcuts wires list navigation (j/k, Enter, x), "g then a letter" jumps, and ? for help.
export function useShortcuts(onHelp: () => void) {
  const navigate = useNavigate();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey || typing(e.target)) return;
      if (document.querySelector("[role=dialog], [role=menu]")) return;
      if (inJump() && jumps[e.key]) {
        gAt = 0;
        e.preventDefault();
        return void navigate({ to: jumps[e.key] });
      }
      const list = rows();
      const current = list.findIndex((el) => el.hasAttribute("data-kbd-focus"));
      switch (e.key) {
        case "g":
          gAt = Date.now();
          return;
        case "?":
          e.preventDefault();
          return onHelp();
        case "j":
        case "ArrowDown":
          if (!list.length) return;
          e.preventDefault();
          return focusRow(list[Math.min(current + 1, list.length - 1)]);
        case "k":
        case "ArrowUp":
          if (!list.length) return;
          e.preventDefault();
          return focusRow(list[Math.max(current - 1, 0)]);
        case "Enter":
          if (current < 0) return;
          e.preventDefault();
          return list[current].querySelector<HTMLElement>("a")?.click();
        case "x":
          if (current < 0) return;
          e.preventDefault();
          return list[current].querySelector<HTMLElement>("[data-task-check]")?.click();
      }
    };
    // A mouse click hands control back to the pointer.
    const onPointer = () => focusRow(undefined);
    window.addEventListener("keydown", onKey);
    window.addEventListener("pointerdown", onPointer);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("pointerdown", onPointer);
    };
  }, [navigate, onHelp]);
}

const groups: { title: string; keys: [string, string][] }[] = [
  { title: "Anywhere", keys: [["⌘ K  or  /", "Command menu"], ["N", "New task"], ["?", "This list"]] },
  { title: "Lists", keys: [["J / ↓", "Next task"], ["K / ↑", "Previous task"], ["Enter", "Open task"], ["X", "Complete or reopen"], ["Esc", "Close the task panel"]] },
  { title: "Go to", keys: [["G T", "My tasks"], ["G N", "Notifications"], ["G P", "Projects"], ["G A", "Agents"]] },
  // Browsers keep ⌘W, ⌘T and Ctrl+Tab for their own tabs, so ours sit on ⌥ (Alt elsewhere).
  {
    title: "Tabs (desktop)",
    keys: [["⌘ Click  or  middle-click", "Open in a background tab"], ["⌘ Enter", "Same, from the command menu"], ["⌥ T", "New tab"], ["⌥ W", "Close tab"], ["⌥ 1–9", "Go to tab"]],
  },
];

export function ShortcutsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent aria-describedby={undefined} className="rounded-2xl sm:max-w-md">
        <DialogTitle>Keyboard shortcuts</DialogTitle>
        <div className="flex flex-col gap-4">
          {groups.map((g) => (
            <section key={g.title}>
              <h3 className="mb-1.5 text-[13px] font-medium text-muted-foreground">{g.title}</h3>
              <dl className="flex flex-col gap-1">
                {g.keys.map(([k, what]) => (
                  <div key={k} className="flex items-center justify-between text-sm">
                    <dt>{what}</dt>
                    <dd><kbd className="rounded-md border border-border bg-muted px-1.5 py-0.5 font-sans text-xs text-muted-foreground">{k}</kbd></dd>
                  </div>
                ))}
              </dl>
            </section>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
