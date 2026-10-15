import { useSyncExternalStore } from "react";

export function useMedia(mq: MediaQueryList) {
  return useSyncExternalStore(
    (l) => {
      mq.addEventListener("change", l);
      return () => mq.removeEventListener("change", l);
    },
    () => mq.matches,
  );
}

// Below Tailwind's sm breakpoint, overlays open as bottom drawers instead of centred dialogs.
const mobile = window.matchMedia("(max-width: 639px)");
export const useIsMobile = () => useMedia(mobile);
