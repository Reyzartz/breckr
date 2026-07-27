import { useCallback, useEffect, useState } from "react";
import {
  THEME_ATTRIBUTE,
  THEME_STORAGE_KEY,
  type Theme,
} from "../constants/index.ts";

function readStoredTheme(): Theme | null {
  const stored = localStorage.getItem(THEME_STORAGE_KEY);
  return stored === "light" || stored === "dark" ? stored : null;
}

function systemTheme(): Theme {
  return window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

/**
 * Theme state, persisted and mirrored onto the document element.
 *
 * brake-ui carries no `dark:` classes — it swaps CSS variables off a
 * `data-theme` attribute on any ancestor — so setting the attribute is all
 * that's required to restyle the whole tree.
 */
export function useTheme(): { theme: Theme; toggleTheme: () => void } {
  const [theme, setTheme] = useState<Theme>(
    () => readStoredTheme() ?? systemTheme()
  );

  useEffect(() => {
    document.documentElement.setAttribute(THEME_ATTRIBUTE, theme);
    localStorage.setItem(THEME_STORAGE_KEY, theme);
  }, [theme]);

  const toggleTheme = useCallback(() => {
    setTheme((current) => (current === "dark" ? "light" : "dark"));
  }, []);

  return { theme, toggleTheme };
}
