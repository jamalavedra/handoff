import { useSyncExternalStore } from "react";

// Below Tailwind's sm breakpoint, overlays open as bottom drawers instead of centred dialogs.
const mobile = window.matchMedia("(max-width: 639px)");

export function useIsMobile() {
  return useSyncExternalStore(
    (l) => {
      mobile.addEventListener("change", l);
      return () => mobile.removeEventListener("change", l);
    },
    () => mobile.matches,
  );
}
