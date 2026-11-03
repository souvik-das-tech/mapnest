import { useSyncExternalStore } from "react";

export type Theme = "light" | "dark";

// One shared store so every component sees the same theme. index.html
// applies the saved theme before first paint.
const listeners = new Set<() => void>();

function get(): Theme {
  return document.documentElement.classList.contains("dark") ? "dark" : "light";
}

function set(t: Theme) {
  document.documentElement.classList.toggle("dark", t === "dark");
  try {
    localStorage.setItem("theme", t);
  } catch {
    /* storage blocked */
  }
  listeners.forEach((l) => l());
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

export function useTheme() {
  const theme = useSyncExternalStore(subscribe, get);
  return { theme, toggle: () => set(theme === "dark" ? "light" : "dark") };
}
