// The Paylist HTTP API. Every call sends the session cookie; errors throw
// ApiError with the server's message.
import type { Attachment } from "@/lib/attachments"
import { lang, locale, t, type LangPref } from "@/lib/i18n"


/** How the ledger sorts a month. */
export type LedgerOrder = "date-asc" | "date-desc" | "amount-desc" | "amount-asc" | "name"

export interface Prefs {
  currency: string
  theme: "system" | "light" | "dark"
  language: LangPref
  ledger_order: LedgerOrder
}

export interface User {
  id: number
  email: string
  name: string
  is_admin: boolean
  prefs: Prefs
  created_at: string
}

export interface ProviderConf {
  kind: "anthropic" | "openai" | "fake"
  base_url: string
  model: string
}

export interface ModelSettings {
  active: string
  providers: Record<string, ProviderConf>
  has_key: Record<string, boolean>
}

export interface Config {
  allow_fake: boolean
  needs_setup: boolean
  colors: string[]
}

export interface Entry {
  id: number
  name: string
  type: string
  amount: number
  date: string
  card: string
  created_at: string
  updated_at: string
}

/** What a refund did: deleted a purchase, added a negative amount, or found several to pick from. */
export interface RefundResult {
  refunded?: Entry
  added?: Entry
  matches?: Entry[]
}

/** A name used before, with how it was last entered (the New entry form's suggestions). */
export interface Suggestion {
  name: string
  type: string
  card: string
  amount: number
  date: string
  uses: number
}

export type EntryInput = Partial<Pick<Entry, "name" | "type" | "amount" | "date" | "card">>

export interface Option {
  id: number
  kind: "type" | "card"
  name: string
  color: string
  uses: number
}

export interface Options {
  types: Option[]
  cards: Option[]
}

export interface GroupTotal {
  name: string
  color: string
  total: number
  count: number
}

export interface Stats {
  month: string
  total: number
  count: number
  prev_total: number
  to_date: number
  prev_to_date: number
  compare_day: number
  daily_avg: number
  days: { date: string; total: number; count: number }[]
  by_type: GroupTotal[]
  by_card: GroupTotal[]
  monthly: MonthTotal[]
}

export interface MonthTotal {
  month: string
  total: number
  count: number
}

export interface AgentToken {
  id: number
  name: string
  hint: string
  created_at: string
  last_used_at: string | null
}

export interface ToolCall {
  id: string
  name: string
  input: unknown
}

export interface ToolResult {
  id: string
  name: string
  content: string
  is_error?: boolean
}

export interface ChatMessage {
  role: "user" | "assistant" | "tool"
  text?: string
  attachments?: Attachment[]
  tool_calls?: ToolCall[]
  results?: ToolResult[]
}

export interface ChatSummary {
  id: string
  title: string
  created_at: string
  updated_at: string
}

export type AgentEvent =
  | { type: "chat"; id: string; title: string }
  | { type: "text"; text: string }
  | { type: "tool_call"; call: ToolCall }
  | { type: "tool_result"; result: ToolResult }
  | { type: "notice"; text: string }
  | { type: "error"; error: string; status: number }
  | { type: "done" }

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

/** Called when a request finds the session gone, so the app can show sign-in. */
let onSignedOut: () => void = () => {}
export function setSignedOutHandler(fn: () => void) {
  onSignedOut = fn
}

async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "same-origin",
    signal,
  })
  if (!res.ok) {
    let msg = t("Request failed ({status}).", { status: res.status })
    try {
      const j = await res.json()
      if (j?.error) msg = j.error
    } catch {
      /* not JSON */
    }
    if (res.status === 401 && !path.startsWith("/api/auth/") && !path.startsWith("/api/agent/")) onSignedOut()
    throw new ApiError(t(msg), res.status)
  }
  return res.json() as Promise<T>
}

const q = (params: Record<string, string | number | undefined>) => {
  const s = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "") s.set(k, String(v))
  const str = s.toString()
  return str ? "?" + str : ""
}

export const api = {
  config: () => request<Config>("GET", "/api/config"),
  me: () => request<{ user: User; model: ModelSettings }>("GET", "/api/me"),
  login: (email: string, password: string) => request<{ user: User }>("POST", "/api/auth/login", { email, password }),
  setup: (email: string, name: string, password: string) => request<{ user: User }>("POST", "/api/auth/setup", { email, name, password }),
  logout: () => request<{ ok: boolean }>("POST", "/api/auth/logout", {}),
  savePrefs: (p: Partial<Prefs>) => request<Prefs>("PUT", "/api/me/prefs", p),
  changePassword: (current: string, next: string) => request<{ ok: boolean }>("PUT", "/api/me/password", { current, new: next }),
  saveModel: (preset: string, conf: ProviderConf, apiKey?: string | null) =>
    request<ModelSettings>("PUT", "/api/me/model", { preset, ...conf, ...(apiKey === undefined ? {} : { api_key: apiKey ?? "" }) }),

  tokens: () => request<AgentToken[]>("GET", "/api/me/tokens"),
  createToken: (name: string) => request<{ token: string; info: AgentToken; endpoint: string }>("POST", "/api/me/tokens", { name }),
  deleteToken: (id: number) => request<{ ok: boolean }>("DELETE", `/api/me/tokens/${id}`),

  users: () => request<User[]>("GET", "/api/admin/users"),
  createUser: (u: { email: string; name: string; password: string; is_admin: boolean }) => request<User>("POST", "/api/admin/users", u),
  resetPassword: (id: number, password: string) => request<{ ok: boolean }>("PUT", `/api/admin/users/${id}/password`, { password }),
  deleteUser: (id: number) => request<{ ok: boolean }>("DELETE", `/api/admin/users/${id}`),

  entries: (month: string) => request<{ month: string; entries: Entry[] }>("GET", "/api/entries" + q({ month })),
  /** The purchases a refund would undo (see store.RefundMatches), newest first. */
  refundMatches: (name: string, amount: number | null) =>
    request<Entry[]>("GET", "/api/refund/matches" + q({ name, amount: amount ?? undefined, today: today() })),
  /** Applies a refund: a picked purchase by id, or its name and amount (with what to record it under if nothing matches). */
  refund: (body: { id?: number; name?: string; amount?: number; type?: string; card?: string; date?: string }) => request<RefundResult>("POST", "/api/refund", { ...body, today: today() }),
  /** Names used before that hold the text, one per name with how it was last entered. */
  suggest: (text: string, limit?: number) => request<Suggestion[]>("GET", "/api/entries/suggest" + q({ q: text, limit })),
  createEntry: (e: EntryInput) => request<Entry>("POST", "/api/entries", e),
  updateEntry: (id: number, e: EntryInput) => request<Entry>("PUT", `/api/entries/${id}`, e),
  deleteEntries: (ids: number[]) => request<{ deleted: number }>("POST", "/api/entries/delete", { ids }),
  exportURL: (month: string) => "/api/export.csv" + q({ month }),

  options: () => request<Options>("GET", "/api/options"),
  createOption: (kind: "type" | "card", name: string, color?: string) => request<Option>("POST", "/api/options", { kind, name, color }),
  updateOption: (id: number, o: { name?: string; color?: string }) => request<Option>("PUT", `/api/options/${id}`, o),
  deleteOption: (id: number) => request<{ cleared: number }>("DELETE", `/api/options/${id}`),

  stats: (month: string, months = 12) => request<Stats>("GET", "/api/stats" + q({ month, months, today: today() })),
  /** Every month with entries up to this one, newest first. */
  months: () => request<MonthTotal[]>("GET", "/api/months" + q({ upto: thisMonth() })),

  chats: () => request<ChatSummary[]>("GET", "/api/chats"),
  chat: (id: string) => request<{ chat: ChatSummary; messages: ChatMessage[] }>("GET", `/api/chats/${id}`),
  deleteChat: (id: string) => request<{ ok: boolean }>("DELETE", `/api/chats/${id}`),

  /** Runs one agent turn, calling onEvent for each streamed event. */
  async agentTurn(body: { chat_id?: string; message: string; attachments?: Attachment[] }, onEvent: (e: AgentEvent) => void, signal: AbortSignal) {
    const res = await fetch("/api/agent/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ...body, today: today() }),
      credentials: "same-origin",
      signal,
    })
    if (!res.ok || !res.body) {
      let msg = t("Request failed ({status}).", { status: res.status })
      try {
        const j = await res.json()
        if (j?.error) msg = j.error
      } catch {
        /* not JSON */
      }
      throw new ApiError(t(msg), res.status)
    }
    const reader = res.body.getReader()
    const dec = new TextDecoder()
    let buf = ""
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      buf += dec.decode(value, { stream: true })
      let nl: number
      while ((nl = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, nl).trim()
        buf = buf.slice(nl + 1)
        if (line) onEvent(JSON.parse(line) as AgentEvent)
      }
    }
  },
}

/** The browser's date, YYYY-MM-DD. */
export function today(d = new Date()): string {
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

export function thisMonth(): string {
  return today().slice(0, 7)
}

export function shiftMonth(month: string, by: number): string {
  const [y, m] = month.split("-").map(Number)
  const d = new Date(y, m - 1 + by, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`
}

export function monthLabel(month: string, style: "long" | "short" = "long"): string {
  const [y, m] = month.split("-").map(Number)
  return new Date(y, m - 1, 1).toLocaleDateString(locale(), style === "long" ? { year: "numeric", month: "long" } : { month: "short" })
}

/** The month's name alone: "November", or "11月". */
export function monthName(month: string): string {
  const [y, m] = month.split("-").map(Number)
  return new Date(y, m - 1, 1).toLocaleDateString(locale(), { month: lang() === "zh" ? "short" : "long" })
}

/** A date as "Nov 24, 2026" or "2026年11月24日"; pass options for another shape. */
export function dateLabel(date: string, opts: Intl.DateTimeFormatOptions = { month: "short", day: "numeric", year: "numeric" }): string {
  return new Date(date + "T00:00").toLocaleDateString(locale(), opts)
}

/** An amount without the currency: 1,234.50. */
export function plainAmount(n: number): string {
  return n.toLocaleString(locale(), { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

const fmtCache = new Map<string, Intl.NumberFormat>()
export function money(amount: number, currency: string, compact = false): string {
  const key = locale() + currency + (compact ? ":c" : "")
  let f = fmtCache.get(key)
  if (!f) {
    try {
      f = new Intl.NumberFormat(locale(), compact
        ? { style: "currency", currency, notation: "compact", maximumFractionDigits: 1 }
        : { style: "currency", currency, minimumFractionDigits: 2, maximumFractionDigits: 2 })
    } catch {
      f = new Intl.NumberFormat(locale(), { minimumFractionDigits: 2, maximumFractionDigits: 2 })
    }
    fmtCache.set(key, f)
  }
  return f.format(amount)
}

/** The currency's symbol, such as ¥ or $. */
export function currencySymbol(currency: string): string {
  try {
    return new Intl.NumberFormat(locale(), { style: "currency", currency, currencyDisplay: "narrowSymbol" }).formatToParts(0).find((p) => p.type === "currency")?.value ?? currency
  } catch {
    return currency
  }
}

/** What month-over-month compares against: "Oct 1–24" (10月1日–24日), or "October" for whole months. */
export function compareLabel(st: Stats): string {
  const prevMonth = shiftMonth(st.month, -1)
  if (!st.compare_day) return monthName(prevMonth)
  const [y, m] = st.month.split("-").map(Number)
  const day = Math.min(st.compare_day, new Date(y, m - 1, 0).getDate())
  if (lang() === "zh") return `${monthName(prevMonth)}1日–${day}日`
  return `${monthLabel(prevMonth, "short")} 1–${day}`
}
