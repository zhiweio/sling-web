// src/store/ui.ts — UI-only state (all server state goes through TanStack Query)
import { create } from "zustand"
import { persist } from "zustand/middleware"

type Theme = "light" | "dark" | "system"

interface UiState {
  theme: Theme
  setTheme: (t: Theme) => void
  // wizard draft (survives page reloads)
  wizardDraft: unknown
  setWizardDraft: (d: unknown) => void
  clearWizardDraft: () => void
}

export const useUi = create<UiState>()(
  persist(
    (set) => ({
      theme: "system",
      setTheme: (theme) => {
        applyTheme(theme)
        set({ theme })
      },
      wizardDraft: null,
      setWizardDraft: (wizardDraft) => set({ wizardDraft }),
      clearWizardDraft: () => set({ wizardDraft: null }),
    }),
    { name: "ui" },
  ),
)

export function applyTheme(theme: Theme) {
  const root = document.documentElement
  const dark =
    theme === "dark" ||
    (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches)
  root.classList.toggle("dark", dark)
  root.style.colorScheme = dark ? "dark" : "light"
}

// init immediately
applyTheme(useUi.getState().theme)
window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
  if (useUi.getState().theme === "system") applyTheme("system")
})
