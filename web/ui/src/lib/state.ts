// App-wide state: the signed-in user, their model settings and their options,
// shared by every page.
import { createContext, useContext } from "react"

import type { Config, Entry, ModelSettings, Options, User } from "@/lib/api"

export interface AppState {
  config: Config
  user: User
  setUser: (u: User) => void
  model: ModelSettings
  setModel: (m: ModelSettings) => void
  options: Options
  reloadOptions: () => Promise<void>
  /** Bumped when the table changed elsewhere (the agent), so pages reload. */
  tableVersion: number
  bumpTable: () => void
  /** Opens the New entry form (dated today). */
  quickAdd: () => void
  /** Opens an entry's form, to change or delete it. */
  editEntry: (e: Entry) => void
  /** Opens the refund dialog (R). */
  refund: () => void
}

export const AppContext = createContext<AppState | null>(null)

export function useApp(): AppState {
  const ctx = useContext(AppContext)
  if (!ctx) throw new Error("useApp outside AppContext")
  return ctx
}

/** The option's color for a type or card name ("gray" when it has none). */
export function optionColor(options: Options, kind: "type" | "card", name: string): string {
  const list = kind === "type" ? options.types : options.cards
  return list.find((o) => o.name.toLowerCase() === name.toLowerCase())?.color ?? "gray"
}

declare global {
  interface Window {
    paylistTheme?: (theme: string) => string
  }
}

export function applyTheme(theme: string) {
  document.documentElement.classList.add("theme-switching")
  window.paylistTheme?.(theme)
  requestAnimationFrame(() => requestAnimationFrame(() => document.documentElement.classList.remove("theme-switching")))
}
