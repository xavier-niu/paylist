import { useSyncExternalStore } from "react"

import { api, type ChatSummary } from "@/lib/api"

// Agent turns underway. A turn runs on the server on its own, so it goes on
// when the agent page is left; this keeps track of them for the rest of the
// app: the sidebar shows the agent at work, and when a turn ends, the app
// reloads what it may have changed and says so, unless the page showing that
// conversation is open.

const running = new Map<string, string>() // conversation id → title
const listeners = new Set<() => void>()
let viewing = "" // the conversation the agent page shows
let finished: ((id: string, title: string, seen: boolean) => void) | null = null
let timer: ReturnType<typeof setTimeout> | null = null

const notify = () => listeners.forEach((l) => l())

function schedule() {
  if (timer || running.size === 0) return
  timer = setTimeout(() => {
    timer = null
    api
      .chats()
      .then(sync)
      .catch(() => {})
      .finally(schedule)
  }, 3000)
}

/** Takes the conversations' list as it is now: turns started elsewhere, and turns that ended. */
export function sync(chats: ChatSummary[]) {
  const now = new Map(chats.filter((c) => c.running).map((c) => [c.id, c.title]))
  for (const [id, title] of running) if (!now.has(id)) end(id, title)
  for (const [id, title] of now) running.set(id, title)
  notify()
  schedule()
}

/** A turn started (or was found) in a conversation. */
export function started(id: string, title: string) {
  running.set(id, title)
  notify()
  schedule()
}

/** A turn the agent page followed to its end. */
export function ended(id: string) {
  const title = running.get(id)
  if (title !== undefined) end(id, title)
  notify()
}

function end(id: string, title: string) {
  running.delete(id)
  finished?.(id, title, viewing === id)
}

/** The conversation the agent page shows ("" when it isn't open). */
export function setViewing(id: string) {
  viewing = id
}

/** What to do when a turn ends; seen: its conversation was on screen. */
export function onFinished(fn: (id: string, title: string, seen: boolean) => void) {
  finished = fn
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

/** Whether the agent is at work in any conversation. */
export function useAgentWorking(): boolean {
  return useSyncExternalStore(subscribe, () => running.size > 0)
}

/** Whether a turn is underway in this conversation. */
export function isRunning(id: string): boolean {
  return running.has(id)
}

// The conversation last open on the agent page, so coming back to the page
// (from the sidebar) shows it again. Kept in memory only: a reload, a closed
// browser or signing out starts afresh.
let lastChat = ""
const lastListeners = new Set<() => void>()

export function setLastChat(id: string) {
  if (id === lastChat) return
  lastChat = id
  lastListeners.forEach((l) => l())
}

/** The conversation to go back to on the agent page ("" for a new one). */
export function useLastChat(): string {
  return useSyncExternalStore(
    (l) => {
      lastListeners.add(l)
      return () => lastListeners.delete(l)
    },
    () => lastChat,
  )
}
