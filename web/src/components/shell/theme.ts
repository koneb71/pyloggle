import { useCallback, useEffect, useState } from "react";

export type Theme = "light" | "dark" | "system";
const KEY = "pyloggle.theme";

function read(): Theme {
  try {
    const t = localStorage.getItem(KEY);
    return t === "light" || t === "dark" ? t : "system";
  } catch {
    return "system";
  }
}

export function applyTheme(t: Theme) {
  const dark = t === "dark" || (t === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
}

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(read);
  const [isDark, setIsDark] = useState(() => document.documentElement.classList.contains("dark"));
  useEffect(() => {
    applyTheme(theme);
    setIsDark(document.documentElement.classList.contains("dark"));
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => {
      if (theme === "system") {
        applyTheme(theme);
        setIsDark(document.documentElement.classList.contains("dark"));
      }
    };
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [theme]);
  const setTheme = useCallback((t: Theme) => {
    try {
      if (t === "system") localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, t);
    } catch {
      /* ignore */
    }
    setThemeState(t);
  }, []);
  return { theme, isDark, setTheme };
}
