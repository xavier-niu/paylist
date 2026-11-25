import { useEffect, useState, type ReactNode } from "react"
import { ChevronRightIcon, HistoryIcon } from "lucide-react"

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"

import { api, money, type Entry, type EntryEvent } from "@/lib/api"
import { locale, t, tParts } from "@/lib/i18n"
import { cn } from "@/lib/utils"

/** "Nov 25, 10:02", or with the year when it isn't this one. */
function when(at: string): string {
  const d = new Date(at)
  const sameYear = d.getFullYear() === new Date().getFullYear()
  return d.toLocaleString(locale(), { month: "short", day: "numeric", ...(sameYear ? {} : { year: "numeric" }), hour: "2-digit", minute: "2-digit" })
}

const FIELDS: Record<string, () => string> = {
  name: () => t("Name"),
  type: () => t("Type"),
  amount: () => t("Amount"),
  date: () => t("Date"),
  card: () => t("Card"),
  orig: () => t("Paid in another currency"),
}

/** A field's value as read: amounts in the ledger's currency, nothing as —. */
function value(field: string, v: unknown, currency: string): string {
  if (v === null || v === undefined || v === "") return "—"
  if (field === "amount" && typeof v === "number") return money(v, currency)
  if (field === "orig" && typeof v === "string") {
    const [n, cur] = v.split(" ")
    return money(Number(n), cur)
  }
  return String(v)
}

/** Someone by name, underlined with dots: a click shows their email beside it. Without a name, the email. */
function Person({ ev }: { ev: EntryEvent }) {
  const [shown, setShown] = useState(false)
  if (!ev.by_name) return <span>{ev.by_email || ev.by || t("Someone")}</span>
  return (
    <button
      type="button"
      onClick={() => setShown((s) => !s)}
      aria-expanded={shown}
      title={shown ? undefined : t("Show their email")}
      className="cursor-pointer font-medium underline decoration-dotted decoration-1 underline-offset-3 hover:decoration-solid"
    >
      {ev.by_name}
      {shown && ev.by_email && <span className="font-normal text-ink-2 no-underline"> ({ev.by_email})</span>}
    </button>
  )
}

/** Who did it, and through what: "Sam", "Sam, through the agent". */
function who(ev: EntryEvent): ReactNode {
  const name = <Person ev={ev} />
  if (ev.via === "agent") return tParts("{name}, through the agent", { name })
  if (ev.via === "mcp") return tParts("{name}, through MCP", { name })
  return name
}

/** An entry's history, loaded when shown, and again when the entry changes. */
function useHistory(entry: Entry): EntryEvent[] | null {
  const [events, setEvents] = useState<EntryEvent[] | null>(null)
  useEffect(() => {
    setEvents(null)
    let live = true
    api
      .entryHistory(entry.id)
      .then((h) => live && setEvents(h))
      .catch(() => live && setEvents([]))
    return () => {
      live = false
    }
  }, [entry.id, entry.updated_at])
  return events
}

/**
 * The steps of an entry's history, oldest first: who created it, who changed
 * what (from what to what) and when, and part refunds. An entry from before
 * history was kept starts with when it was created.
 */
function HistoryList({ entry, events, currency, className }: { entry: Entry; events: EntryEvent[] | null; currency: string; className?: string }) {
  const created = events?.some((e) => e.action === "created")
  if (!events) return <p className={cn("text-[12.5px] text-muted-foreground", className)}>{t("Loading")}</p>
  return (
    <ol className={cn("grid gap-2.5 text-[12.5px]", className)} aria-label={t("History")}>
      {!created && (
        <li className="grid gap-0.5">
          <span>{t("Created")}</span>
          <span className="text-muted-foreground">{when(entry.created_at)}</span>
        </li>
      )}
      {events.map((ev) => {
        const changes = Object.entries(ev.changes ?? {})
        const amount = ev.changes?.amount
        const head =
          ev.action === "created"
            ? tParts("{who} created it", { who: who(ev) })
            : ev.action === "refunded"
              ? tParts("{who} refunded {amount}", { who: who(ev), amount: amount ? money(Number(amount[0]) - Number(amount[1]), currency) : "" })
              : tParts("{who} changed it", { who: who(ev) })
        return (
          <li key={ev.id} className="grid gap-0.5">
            <span className="text-ink">{head}</span>
            {ev.action === "created" ? (
              amount && <span className="text-ink-2 tabular-nums">{value("amount", amount[1], currency)}{ev.changes?.orig ? ` · ${value("orig", ev.changes.orig[1], currency)}` : ""}</span>
            ) : (
              changes.map(([field, [before, after]]) => (
                <span key={field} className="text-ink-2 tabular-nums">
                  {(FIELDS[field]?.() ?? field) + ": "}
                  {value(field, before, currency)} → {value(field, after, currency)}
                </span>
              ))
            )}
            <span className="text-muted-foreground">{when(ev.at)}</span>
          </li>
        )
      })}
    </ol>
  )
}

/** An entry's history, folded under its form. */
export function EntryHistory({ entry, currency }: { entry: Entry; currency: string }) {
  const [open, setOpen] = useState(false)
  const events = useHistory(entry)
  const count = (events?.length ?? 0) + (events?.some((e) => e.action === "created") ? 0 : 1)
  return (
    <div className="mt-4 border-t border-rule pt-3">
      <button type="button" onClick={() => setOpen((o) => !o)} aria-expanded={open} className="flex items-center gap-1 rounded-md text-[12.5px] text-ink-2 hover:text-ink">
        <ChevronRightIcon className={cn("size-3.5 transition-transform", open && "rotate-90")} />
        {t("History")}
        {events && <span className="text-muted-foreground tabular-nums">· {count}</span>}
      </button>
      {open && <HistoryList entry={entry} events={events} currency={currency} className="mt-2 max-h-56 overflow-y-auto pr-1" />}
    </div>
  )
}

/**
 * A row's history button: a clock at the end of its name, shown when the row
 * is pointed at or the button has focus (and always, faintly, on a touch
 * screen); the history opens under it.
 */
export function HistoryButton({ entry, currency }: { entry: Entry; currency: string }) {
  const [open, setOpen] = useState(false)
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={t("History of {name}", { name: entry.name })}
          title={t("History")}
          className={cn(
            "grid size-7 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-wash hover:text-ink focus-visible:opacity-100",
            "opacity-0 group-hover/row:opacity-100 data-[state=open]:opacity-100 [@media(hover:none)]:opacity-60",
          )}
        >
          <HistoryIcon className="size-3.5" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 gap-0 rounded-[14px] border border-rule-2 p-0 shadow-[0_16px_48px_rgb(0_0_0/.18)] ring-0">
        <div className="border-b border-rule px-3.5 py-2.5 text-[13px] font-medium">{t("History of {name}", { name: entry.name })}</div>
        {open && <PopupHistory entry={entry} currency={currency} />}
      </PopoverContent>
    </Popover>
  )
}

function PopupHistory({ entry, currency }: { entry: Entry; currency: string }) {
  const events = useHistory(entry)
  return <HistoryList entry={entry} events={events} currency={currency} className="max-h-80 overflow-y-auto px-3.5 py-3" />
}
