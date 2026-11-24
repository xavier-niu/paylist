import { useCallback, useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { ArrowUpIcon, CheckIcon, ChevronRightIcon, HistoryIcon, SparklesIcon, SquareIcon, SquarePenIcon, Trash2Icon, XIcon } from "lucide-react"
import { toast } from "sonner"

import { useConfirm } from "@/components/confirm"
import { PageHeader } from "@/components/month-picker"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Loading, Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { api, ApiError, type ChatMessage, type ChatSummary, type ToolCall, type ToolResult } from "@/lib/api"
import { renderMarkdown } from "@/lib/markdown"
import { PRESETS } from "@/lib/presets"
import { useApp } from "@/lib/state"
import { cn } from "@/lib/utils"
import { locale, t, tParts } from "@/lib/i18n"

type Item =
  | { kind: "user"; text: string }
  | { kind: "assistant"; text: string }
  | { kind: "tool"; call: ToolCall; result?: ToolResult }
  | { kind: "notice"; text: string }
  | { kind: "error"; text: string }

const TOOLS: Record<string, [running: string, done: string]> = {
  list_entries: ["looking up entries", "looked up entries"],
  add_entries: ["adding entries", "added entries"],
  update_entry: ["changing an entry", "changed an entry"],
  delete_entries: ["deleting entries", "deleted entries"],
  list_options: ["reading the types and cards", "read the types and cards"],
  manage_option: ["changing a type or card", "changed a type or card"],
  get_stats: ["reading the statistics", "read the statistics"],
  list_months: ["reading the months", "read the months"],
  suggest_entries: ["looking up names used before", "looked up names used before"],
  refund: ["recording a refund", "recorded a refund"],
  get_profile: ["reading the profile", "read the profile"],
}
const CHANGES = new Set(["add_entries", "update_entry", "delete_entries", "manage_option", "refund"])

const SUGGESTIONS = () => [
  t("Add lunch 35 and a metro ride 4.50 today, paid by card"),
  t("How much did I spend on food this month?"),
  t("Compare this month with last month, type by type"),
  t("What were my three biggest expenses this month?"),
]

/** Saved messages as the items the page shows. */
function toItems(msgs: ChatMessage[]): Item[] {
  const items: Item[] = []
  const tools = new Map<string, Extract<Item, { kind: "tool" }>>()
  for (const m of msgs) {
    if (m.role === "user") items.push({ kind: "user", text: m.text ?? "" })
    else if (m.role === "assistant") {
      if (m.text) items.push({ kind: "assistant", text: m.text })
      for (const c of m.tool_calls ?? []) {
        const it = { kind: "tool" as const, call: c }
        tools.set(c.id, it)
        items.push(it)
      }
    } else if (m.role === "tool") {
      for (const r of m.results ?? []) {
        const it = tools.get(r.id)
        if (it) it.result = r
      }
    }
  }
  return items
}

/** A tool call the agent made, as a small pill; open it for what went in and came back. */
function ToolLine({ call, result }: { call: ToolCall; result?: ToolResult }) {
  const [open, setOpen] = useState(false)
  const [running, done] = TOOLS[call.name]?.map((s) => t(s)) ?? [t("running {tool}", { tool: call.name }), t("ran {tool}", { tool: call.name })]
  const failed = result?.is_error
  let pretty = result?.content ?? ""
  try {
    pretty = JSON.stringify(JSON.parse(pretty), null, 2)
  } catch {
    /* plain text */
  }
  return (
    <div className="min-w-0">
      <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open} className={cn("pill soft !h-7 max-w-full !px-3 text-[13px]", failed && "!text-red")}>
        {!result ? <Spinner className="size-3.5" /> : failed ? <XIcon className="!size-3.5" /> : <CheckIcon className="!size-3.5" />}
        <span className="truncate">{!result ? running : failed ? t("{step}: failed", { step: done }) : done}</span>
        <ChevronRightIcon className={cn("!size-3.5 text-ink-2 transition-transform", open && "rotate-90")} />
      </button>
      {open && (
        <div className="mt-2 grid gap-2 rounded-[14px] bg-side p-3 text-[12px] shadow-[inset_0_0_0_1px_var(--rule)]">
          <div className="text-ink-2">
            {tParts("{tool} through MCP", { tool: <code className="font-mono">{call.name}</code> })}
          </div>
          <div>
            <div className="label mb-1">{t("Sent")}</div>
            <pre className="max-h-48 overflow-auto font-mono text-[11.5px] leading-relaxed">{JSON.stringify(call.input ?? {}, null, 2)}</pre>
          </div>
          {result && (
            <div>
              <div className="label mb-1">{t("Returned")}</div>
              <pre className="max-h-64 overflow-auto font-mono text-[11.5px] leading-relaxed">{pretty}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export function AgentView({ chatId }: { chatId: string }) {
  const { model, bumpTable, reloadOptions } = useApp()
  const [chats, setChats] = useState<ChatSummary[]>([])
  const [items, setItems] = useState<Item[]>([])
  const [loading, setLoading] = useState(false)
  const [running, setRunning] = useState(false)
  const [input, setInput] = useState("")
  const abortRef = useRef<AbortController | null>(null)
  const currentRef = useRef(chatId) // the conversation the page shows
  const scrollRef = useRef<HTMLDivElement>(null)
  const stickRef = useRef(true)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const [ask, confirmEl] = useConfirm()

  const loadChats = useCallback(() => {
    api.chats().then(setChats).catch(() => {})
  }, [])
  useEffect(loadChats, [loadChats])

  // Open the conversation in the address (or a new one).
  useEffect(() => {
    if (chatId === currentRef.current && (items.length > 0 || !chatId)) return
    currentRef.current = chatId
    abortRef.current?.abort()
    setItems([])
    if (!chatId) return
    setLoading(true)
    api
      .chat(chatId)
      .then((c) => {
        if (currentRef.current === chatId) setItems(toItems(c.messages))
      })
      .catch((e) => {
        toast.error((e as Error).message)
        location.hash = "#/agent"
      })
      .finally(() => setLoading(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [chatId])

  // Follow the reply as it streams, unless the reader scrolled up.
  useEffect(() => {
    const el = scrollRef.current
    if (el && stickRef.current) el.scrollTop = el.scrollHeight
  }, [items])

  const send = async (text: string) => {
    text = text.trim()
    if (!text || running) return
    setInput("")
    stickRef.current = true
    setItems((l) => [...l, { kind: "user", text }])
    setRunning(true)
    const ctrl = new AbortController()
    abortRef.current = ctrl
    let changed = false
    const push = (fn: (l: Item[]) => Item[]) => setItems(fn)
    try {
      await api.agentTurn(
        { chat_id: currentRef.current || undefined, message: text },
        (ev) => {
          switch (ev.type) {
            case "chat":
              if (currentRef.current !== ev.id) {
                currentRef.current = ev.id
                history.replaceState(null, "", `#/agent/${ev.id}`)
              }
              loadChats()
              break
            case "text":
              push((l) => {
                const last = l[l.length - 1]
                if (last?.kind === "assistant") return [...l.slice(0, -1), { ...last, text: last.text + ev.text }]
                return [...l, { kind: "assistant", text: ev.text }]
              })
              break
            case "tool_call":
              if (CHANGES.has(ev.call.name)) changed = true
              push((l) => [...l, { kind: "tool", call: ev.call }])
              break
            case "tool_result":
              push((l) => l.map((it) => (it.kind === "tool" && it.call.id === ev.result.id ? { ...it, result: ev.result } : it)))
              break
            case "notice":
              push((l) => [...l, { kind: "notice", text: ev.text }])
              break
            case "error":
              push((l) => [...l, { kind: "error", text: ev.error }])
              break
          }
        },
        ctrl.signal,
      )
    } catch (e) {
      if (!ctrl.signal.aborted) {
        const msg = (e as Error).message
        push((l) => [...l, { kind: "error", text: e instanceof ApiError && e.status === 401 && !model.active ? t("Choose a model in Settings first.") : msg }])
      } else {
        push((l) => [...l, { kind: "notice", text: t("Stopped.") }])
      }
    } finally {
      setRunning(false)
      abortRef.current = null
      if (changed) {
        bumpTable()
        void reloadOptions()
      }
      loadChats()
      inputRef.current?.focus()
    }
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    void send(input)
  }
  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      void send(input)
    }
  }

  const newChat = () => {
    abortRef.current?.abort()
    currentRef.current = ""
    setItems([])
    location.hash = "#/agent"
    inputRef.current?.focus()
  }

  const removeChat = (c: ChatSummary) =>
    ask({
      title: t("Delete “{name}”?", { name: c.title }),
      body: t("The conversation is deleted. What it changed in your ledger stays."),
      action: t("Delete"),
      run: async () => {
        await api.deleteChat(c.id).catch((e) => toast.error((e as Error).message))
        if (c.id === currentRef.current) newChat()
        loadChats()
      },
    })

  const provider = model.active ? model.providers[model.active] : undefined
  const providerLabel = provider ? (provider.kind === "fake" ? t("Demo model") : provider.model || t(PRESETS[model.active]?.label ?? "") || provider.kind) : ""
  const last = items[items.length - 1]
  const thinking = running && (!last || last.kind === "user" || (last.kind === "tool" && !!last.result))
  const active = currentRef.current || chatId
  const empty = !loading && items.length === 0

  return (
    <div className="mx-auto flex h-[calc(100dvh-var(--bar,0px))] w-full max-w-[1200px] flex-col px-4 pt-5 sm:px-8">
      <PageHeader title={t("Agent")}>
        <Popover>
          <PopoverTrigger asChild>
            <button type="button" className="pill">
              <HistoryIcon /> {t("History")}
            </button>
          </PopoverTrigger>
          <PopoverContent align="end" sideOffset={8} className="w-80 gap-0 rounded-[14px] border-0 p-1.5 shadow-[0_16px_48px_rgb(0_0_0/.18),0_0_0_1px_var(--rule-2)] ring-0">
            {chats.length === 0 && <p className="px-2.5 py-2 text-[13.5px] text-ink-2">{t("No conversations yet.")}</p>}
            <div className="max-h-96 overflow-y-auto">
              {chats.map((c) => (
                <div key={c.id} className="group relative">
                  <a href={`#/agent/${c.id}`} className={cn("block rounded-[10px] py-2 pr-9 pl-2.5 hover:bg-wash", c.id === active && "bg-wash")} title={c.title}>
                    <span className="block truncate text-[14px]">{c.title}</span>
                    <span className="block text-[12px] text-ink-2">{new Date(c.updated_at).toLocaleDateString(locale(), { day: "numeric", month: "short" })}</span>
                  </a>
                  <button type="button" aria-label={t("Delete {name}", { name: c.title })} onClick={() => removeChat(c)} className="absolute top-1/2 right-2 hidden -translate-y-1/2 rounded-md p-1.5 text-ink-2 group-hover:block hover:text-red focus-visible:block">
                    <Trash2Icon className="size-3.5" />
                  </button>
                </div>
              ))}
            </div>
          </PopoverContent>
        </Popover>
        <button type="button" className="pill" onClick={newChat}>
          <SquarePenIcon /> {t("New chat")}
        </button>
      </PageHeader>

      <div
        ref={scrollRef}
        onScroll={(e) => {
          const el = e.currentTarget
          stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 60
        }}
        className="min-h-0 flex-1 overflow-y-auto"
      >
        <div className="mx-auto grid max-w-[760px] grid-cols-[minmax(0,1fr)] gap-4 py-6">
          {loading && (
            // A conversation's shape: a question on the right, an answer's lines on the left.
            <Loading className="grid gap-4">
              <Skeleton className="ml-auto h-10 w-2/5 rounded-[18px]" />
              <span className="grid gap-2.5">
                <Skeleton className="w-4/5" />
                <Skeleton className="w-3/5" />
                <Skeleton className="w-2/3" />
              </span>
              <Skeleton className="h-7 w-44 rounded-full" />
            </Loading>
          )}
          {empty && (
            <div className="grid justify-items-center pt-[12vh] text-center">
              <span className="grid size-12 place-items-center rounded-[14px] bg-sheet shadow-[inset_0_0_0_1px_var(--rule-2)]">
                <SparklesIcon className="size-5" />
              </span>
              <h2 className="mt-4 text-[15px] font-semibold">{t("Ask about your spending, or tell me what to write down")}</h2>
              {!model.active && (
                <a href="#/settings" className="mt-1.5 text-[13px] text-ink-2 underline underline-offset-3 hover:text-ink">
                  {t("Choose a model in Settings")}
                </a>
              )}
            </div>
          )}
          {items.map((it, i) => {
            switch (it.kind) {
              case "user":
                return (
                  <div key={i} className="flex justify-end">
                    <div className="max-w-[85%] rounded-[18px] bg-wash px-4 py-2.5 text-[14.5px] whitespace-pre-wrap">{it.text}</div>
                  </div>
                )
              case "assistant":
                return <div key={i} className={cn("prose-chat text-[14.5px]", running && i === items.length - 1 && "caret")} dangerouslySetInnerHTML={{ __html: renderMarkdown(it.text) }} />
              case "tool":
                return <ToolLine key={i} call={it.call} result={it.result} />
              case "notice":
                return (
                  <p key={i} className="text-[13px] text-ink-2">
                    {it.text}
                  </p>
                )
              case "error":
                return (
                  <p key={i} role="alert" className="rounded-[12px] bg-red-wash px-3.5 py-2.5 text-[13.5px] text-red">
                    {it.text}
                  </p>
                )
            }
          })}
          {thinking && <p className="caret text-[13px] text-ink-2">{t("Working")}</p>}
        </div>
      </div>

      <form onSubmit={submit} className="mx-auto w-full max-w-[760px] pt-2 pb-4">
        {empty && model.active && (
          <div className="mb-3 flex flex-wrap gap-2">
            {SUGGESTIONS().map((s) => (
              <button key={s} type="button" className="pill !h-8 text-[13.5px]" onClick={() => void send(s)} disabled={running}>
                {s}
              </button>
            ))}
          </div>
        )}
        <div className="rounded-[16px] bg-sheet p-2 shadow-[inset_0_0_0_1px_var(--rule-2)] focus-within:shadow-[inset_0_0_0_1.5px_var(--ink)]">
          <textarea
            ref={inputRef}
            id="agent-message"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={onKey}
            rows={1}
            placeholder={model.active ? t("Ask anything, or tell me what to add…") : t("Choose a model in Settings first")}
            aria-label={t("Message")}
            maxLength={8000}
            className="max-h-40 min-h-10 w-full resize-none bg-transparent px-2.5 py-2 text-[14.5px] outline-none placeholder:text-muted-foreground [field-sizing:content]"
            autoFocus
          />
          <div className="flex items-center justify-between gap-2 px-1">
            <a href="#/settings" className="pill soft !h-7 max-w-[60%] !px-3 text-[12.5px] text-ink-2">
              <span className="truncate">{providerLabel || t("No model")}</span>
            </a>
            {running ? (
              <button type="button" aria-label={t("Stop")} onClick={() => abortRef.current?.abort()} className="grid size-8 place-items-center rounded-full bg-ink text-paper">
                <SquareIcon className="size-3 fill-current" />
              </button>
            ) : (
              <button type="submit" aria-label={t("Send")} disabled={!input.trim() || !model.active} className="grid size-8 place-items-center rounded-full bg-ink text-paper disabled:opacity-25">
                <ArrowUpIcon className="size-4" />
              </button>
            )}
          </div>
        </div>
      </form>
      {confirmEl}
    </div>
  )
}
