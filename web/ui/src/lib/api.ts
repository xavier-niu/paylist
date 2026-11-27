// The Paylist HTTP API. Every call sends the session cookie; errors throw
// ApiError with the server's message.
import type { Attachment } from "@/lib/attachments"
import { lang, locale, t, type LangPref } from "@/lib/i18n"


/** How the ledger sorts a month. */
export type LedgerOrder = "date-asc" | "date-desc" | "amount-desc" | "amount-asc" | "name"

/** A ledger as the signed-in user sees it: their role in it decides what they can do. */
export interface Ledger {
  id: number
  name: string
  currency: string
  role: "owner" | "editor" | "viewer"
  owner: string
  members: number
  created_at: string
}

export interface FxRate {
  from: string
  to: string
  rate: number
  /** The day the rate was published. */
  date: string
  source: string
}

export interface LedgerMember {
  user_id: number
  email: string
  name: string
  role: "owner" | "editor" | "viewer"
}

// The ledger requests work on, sent with each one (X-Paylist-Ledger).
let currentLedger = 0
export function setCurrentLedger(id: number) {
  currentLedger = id
}
const ledgerHeaders = (): Record<string, string> => (currentLedger ? { "X-Paylist-Ledger": String(currentLedger) } : {})

export interface Prefs {
  currency: string
  theme: "system" | "light" | "dark"
  language: LangPref
  ledger_order: LedgerOrder
  ledger_id?: number
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
  /** Paid in another currency: how much there, and which (amount is the ledger's). */
  orig_amount?: number
  orig_currency?: string
}

/** A step in an entry's history: created, changed, or part refunded; changes holds each field's before and after. */
export interface EntryEvent {
  id: number
  action: "created" | "changed" | "refunded"
  changes?: Record<string, [unknown, unknown]>
  user_id?: number
  /** Their name or email; "" when unknown. */
  by: string
  /** Their name (names are unique), and email. */
  by_name?: string
  by_email?: string
  /** "" for the app, or "agent", "mcp". */
  via?: string
  at: string
}

/** What a refund did: deleted a purchase, added a negative amount, or found several to pick from. */
export interface RefundResult {
  refunded?: Entry
  /** Part refunded: the purchase as it now is. */
  reduced?: Entry
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
  /** Paid in another currency last time: what it was there. */
  orig_amount?: number
  orig_currency?: string
}

/** An entry's fields to set; an empty orig_currency clears what was paid elsewhere. */
export type EntryInput = Partial<Pick<Entry, "name" | "type" | "amount" | "date" | "card" | "orig_amount" | "orig_currency">>

export interface Option {
  id: number
  kind: "type" | "card"
  name: string
  color: string
  uses: number
  /** The latest date an entry has it (YYYY-MM-DD); none when unused. */
  last_used?: string
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
  /** Each type over the same months as `monthly`. */
  type_monthly: { name: string; color: string; totals: number[] }[]
  /** The usual: the average month over the `avg_months` months before, counted like `to_date`. */
  avg_months: number
  avg_to_date: number
  type_avg: GroupTotal[]
  /** `by_type` counted like `to_date`. */
  by_type_to_date: GroupTotal[]
  year: YearStats
}

/** A year, against the year before: up to `compare_date` in the current year, whole years otherwise. */
export interface YearStats {
  year: number
  total: number
  count: number
  to_date: number
  prev_to_date: number
  compare_date: string
  months: MonthTotal[]
  prev_months: MonthTotal[]
  by_type: GroupTotal[]
}

export interface MonthTotal {
  month: string
  total: number
  count: number
}

/** Somewhere Paylist sends messages: a webhook, or a chat or push service. */
export interface Channel {
  id: number
  kind: string
  name: string
  url: string
  /** The names of the headers it sends; their values are never shown. */
  headers: string[]
  last_at?: string
  last_error?: string
  created_at: string
}

/** A new channel, or what to change of one; a header without a value keeps it. */
export interface ChannelInput {
  kind?: string
  name?: string
  url?: string
  headers?: { name: string; value: string }[]
}

export type ReportPeriod = "daily" | "weekly" | "monthly"

/** Which of a ledger's reports you get, when, in which language, on which channels. */
export interface Reports {
  daily: boolean
  weekly: boolean
  monthly: boolean
  time: string
  timezone: string
  language: string
  /** Your AI model writes the note, when you have one (ai_ready). */
  ai: boolean
  ai_ready: boolean
  channels: number[]
}

/** A report, as it would be sent. */
export interface Report {
  period: ReportPeriod
  from: string
  to: string
  title: string
  text: string
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
  /** A turn is underway (it runs on when the page goes). */
  running?: boolean
}

export type AgentEvent =
  | { type: "chat"; id: string; title: string }
  | { type: "text"; text: string }
  | { type: "tool_call"; call: ToolCall }
  | { type: "tool_result"; result: ToolResult }
  | { type: "notice"; text: string }
  | { type: "error"; error: string; status: number }
  | { type: "stopped" }
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

/** A request to the server, on the ledger open unless it names another. */
async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal, ledger?: number): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: { ...(ledger ? { "X-Paylist-Ledger": String(ledger) } : ledgerHeaders()), ...(body === undefined ? {} : { "Content-Type": "application/json" }) },
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
  /** Your name, as others see you (unique; empty shows your email). */
  setName: (name: string) => request<User>("PUT", "/api/me/name", { name }),
  changePassword: (current: string, next: string) => request<{ ok: boolean }>("PUT", "/api/me/password", { current, new: next }),
  saveModel: (preset: string, conf: ProviderConf, apiKey?: string | null) =>
    request<ModelSettings>("PUT", "/api/me/model", { preset, ...conf, ...(apiKey === undefined ? {} : { api_key: apiKey ?? "" }) }),

  channels: () => request<Channel[]>("GET", "/api/channels"),
  addChannel: (c: ChannelInput) => request<Channel>("POST", "/api/channels", c),
  updateChannel: (id: number, c: ChannelInput) => request<Channel>("PUT", `/api/channels/${id}`, c),
  deleteChannel: (id: number) => request<{ ok: boolean }>("DELETE", `/api/channels/${id}`),
  /** Sends a channel a short message; it comes back with how that went. */
  testChannel: (id: number, language: string) => request<Channel>("POST", `/api/channels/${id}/test`, { language }),
  /** Your reports of a ledger. */
  reports: (ledger: number) => request<Reports>("GET", "/api/reports", undefined, undefined, ledger),
  saveReports: (ledger: number, r: Reports) => request<Reports>("PUT", "/api/reports", r, undefined, ledger),
  sendReport: (ledger: number, period: ReportPeriod, timezone: string, language: string) =>
    request<{ report: Report; channels: Channel[] }>("POST", "/api/reports/send", { period, timezone, language }, undefined, ledger),
  tokens: () => request<AgentToken[]>("GET", "/api/me/tokens"),
  createToken: (name: string) => request<{ token: string; info: AgentToken; endpoint: string }>("POST", "/api/me/tokens", { name }),
  deleteToken: (id: number) => request<{ ok: boolean }>("DELETE", `/api/me/tokens/${id}`),

  users: () => request<User[]>("GET", "/api/admin/users"),
  createUser: (u: { email: string; name: string; password: string; is_admin: boolean }) => request<User>("POST", "/api/admin/users", u),
  /** An admin naming an account (unique; empty shows the email). */
  setUserName: (id: number, name: string) => request<User>("PUT", `/api/admin/users/${id}/name`, { name }),
  resetPassword: (id: number, password: string) => request<{ ok: boolean }>("PUT", `/api/admin/users/${id}/password`, { password }),
  deleteUser: (id: number) => request<{ ok: boolean }>("DELETE", `/api/admin/users/${id}`),

  entries: (month: string) => request<{ month: string; entries: Entry[] }>("GET", "/api/entries" + q({ month })),
  /** The purchases a refund would undo (see store.RefundMatches), newest first. */
  refundMatches: (name: string, amount: number | null) =>
    request<Entry[]>("GET", "/api/refund/matches" + q({ name, amount: amount ?? undefined, today: today() })),
  /** Applies a refund: a picked purchase by id, or its name and amount (with what to record it under if nothing matches). */
  refund: (body: { id?: number; name?: string; amount?: number; type?: string; card?: string; date?: string; orig_amount?: number; orig_currency?: string; new?: boolean }) => request<RefundResult>("POST", "/api/refund", { ...body, today: today() }),
  /** Names used before that hold the text, one per name with how it was last entered. */
  suggest: (text: string, limit?: number) => request<Suggestion[]>("GET", "/api/entries/suggest" + q({ q: text, limit })),
  createEntry: (e: EntryInput) => request<Entry>("POST", "/api/entries", e),
  updateEntry: (id: number, e: EntryInput) => request<Entry>("PUT", `/api/entries/${id}`, e),
  /** An entry's history, oldest first. */
  entryHistory: (id: number) => request<EntryEvent[]>("GET", `/api/entries/${id}/history`),
  deleteEntries: (ids: number[]) => request<{ deleted: number }>("POST", "/api/entries/delete", { ids }),
  exportURL: (month: string) => "/api/export.csv" + q({ month, ledger: currentLedger || undefined }),

  ledgers: () => request<{ ledgers: Ledger[]; current: number }>("GET", "/api/ledgers"),
  createLedger: (name: string, currency: string) => request<Ledger>("POST", "/api/ledgers", { name, currency }),
  updateLedger: (id: number, change: { name?: string; currency?: string }) => request<Ledger>("PUT", `/api/ledgers/${id}`, change),
  deleteLedger: (id: number) => request<{ ok: boolean }>("DELETE", `/api/ledgers/${id}`),
  ledgerMembers: (id: number) => request<LedgerMember[]>("GET", `/api/ledgers/${id}/members`),
  addLedgerMember: (id: number, email: string, role: "editor" | "viewer") => request<LedgerMember>("POST", `/api/ledgers/${id}/members`, { email, role }),
  setLedgerMemberRole: (id: number, userID: number, role: "editor" | "viewer") => request<{ ok: boolean }>("PUT", `/api/ledgers/${id}/members/${userID}`, { role }),
  removeLedgerMember: (id: number, userID: number) => request<{ ok: boolean }>("DELETE", `/api/ledgers/${id}/members/${userID}`),

  // Types and cards, of the ledger open or the one given.
  options: (ledger?: number) => request<Options>("GET", "/api/options", undefined, undefined, ledger),
  createOption: (kind: "type" | "card", name: string, color?: string, ledger?: number) => request<Option>("POST", "/api/options", { kind, name, color }, undefined, ledger),
  updateOption: (id: number, o: { name?: string; color?: string }, ledger?: number) => request<Option>("PUT", `/api/options/${id}`, o, undefined, ledger),
  /** Puts the ledger's types (or cards) in this order; the options as they now are come back. */
  reorderOptions: (kind: "type" | "card", ids: number[], ledger?: number) => request<Options>("PUT", "/api/options/order", { kind, ids }, undefined, ledger),
  deleteOption: (id: number, ledger?: number) => request<{ cleared: number }>("DELETE", `/api/options/${id}`, undefined, undefined, ledger),

  /** How much one `from` is worth in `to` on a day: to convert an amount paid in another currency. */
  fx: (from: string, to: string, date: string) => request<FxRate>("GET", "/api/fx" + q({ from, to, date })),
  stats: (month: string, months = 12) => request<Stats>("GET", "/api/stats" + q({ month, months, today: today() })),
  /** Every month with entries up to this one, newest first. */
  months: () => request<MonthTotal[]>("GET", "/api/months" + q({ upto: thisMonth() })),

  chats: () => request<ChatSummary[]>("GET", "/api/chats"),
  /** A conversation; while a turn is underway, `turn_start` is where its replies begin (follow them with chatStream). */
  chat: (id: string) => request<{ chat: ChatSummary; messages: ChatMessage[]; running?: boolean; turn_start?: number }>("GET", `/api/chats/${id}`),
  deleteChat: (id: string) => request<{ ok: boolean }>("DELETE", `/api/chats/${id}`),
  /** Stops the turn underway in a conversation. */
  stopChat: (id: string) => request<{ ok: boolean }>("POST", `/api/chats/${id}/stop`, {}),

  /** Starts an agent turn and follows it, calling onEvent for each event. It runs on if this stops following (signal). */
  agentTurn(body: { chat_id?: string; message: string; attachments?: Attachment[] }, onEvent: (e: AgentEvent) => void, signal: AbortSignal) {
    return readEvents(
      fetch("/api/agent/chat", {
        method: "POST",
        headers: { ...ledgerHeaders(), "Content-Type": "application/json" },
        body: JSON.stringify({ ...body, today: today() }),
        credentials: "same-origin",
        signal,
      }),
      onEvent,
    )
  },
  /** Follows the turn underway in a conversation, from its start. */
  chatStream(id: string, onEvent: (e: AgentEvent) => void, signal: AbortSignal) {
    return readEvents(fetch(`/api/chats/${id}/stream`, { credentials: "same-origin", signal }), onEvent)
  },
}

/** Reads a stream of agent events (NDJSON), one call each. */
async function readEvents(response: Promise<Response>, onEvent: (e: AgentEvent) => void) {
  const res = await response
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
