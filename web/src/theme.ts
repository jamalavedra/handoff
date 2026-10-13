import { useSyncExternalStore } from "react";

export type Theme = "system" | "light" | "dark";

const key = "handoff-theme";
const media = window.matchMedia("(prefers-color-scheme: dark)");
const listeners = new Set<() => void>();

function read(): Theme {
  try {
    const t = localStorage.getItem(key);
    return t === "light" || t === "dark" ? t : "system";
  } catch {
    return "system";
  }
}

let current = read();

function apply() {
  const dark = current === "dark" || (current === "system" && media.matches);
  document.documentElement.classList.toggle("dark", dark);
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", dark ? "#09090b" : "#fafafa");
}

media.addEventListener("change", apply);

export function setTheme(t: Theme) {
  current = t;
  try {
    if (t === "system") localStorage.removeItem(key);
    else localStorage.setItem(key, t);
  } catch {
    // private mode: keep the choice for this tab only
  }
  apply();
  listeners.forEach((l) => l());
}

export function useTheme() {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => current,
  );
}
