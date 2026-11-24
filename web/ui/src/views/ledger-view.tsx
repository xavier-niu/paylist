import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
import { ChevronsUpDownIcon, DownloadIcon, EllipsisIcon, PlusIcon, SearchIcon, Trash2Icon, Undo2Icon } from "lucide-react"
import { toast } from "sonner"

import { useConfirm } from "@/components/confirm"
import { parseAmount } from "@/components/entry-dialog"
import { MonthHeading } from "@/components/month-picker"
import { OptionSelect, Tag } from "@/components/option-select"
import { Checkbox } from "@/components/ui/checkbox"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Loading, Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { api, compareLabel, currencySymbol, dateLabel, money, monthLabel, monthName, plainAmount, shiftMonth, thisMonth, today, type Entry, type EntryInput, type Stats } from "@/lib/api"
import { Money } from "@/lib/money"
import { optionColor, useApp } from "@/lib/state"
import { cn } from "@/lib/utils"
import { t, tn } from "@/lib/i18n"

const plain = new Intl.NumberFormat("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2, useGrouping: false })

/** A text cell that saves when it loses focus or on Enter; Escape puts it back. */
function TextCell({ value, onSave, label, className }: { value: string; onSave: (v: string) => Promise<boolean>; label: string; className?: string }) {
  const [text, setText] = useState(value)
  useEffect(() => setText(value), [value])
  return (
    <input
      className={cn("cell", className)}
      value={text}
      aria-label={label}
      onChange={(e) => setText(e.target.value)}
      onBlur={async () => {
        if (text === value) return
        if (!text.trim() || !(await onSave(text))) setText(value)
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter") (e.target as HTMLInputElement).blur()
        if (e.key === "Escape") {
          setText(value)
          requestAnimationFrame(() => (e.target as HTMLInputElement).blur())
        }
      }}
    />
  )
}

/** An amount, as a plain number (the column header names the currency). */
function AmountCell({ value, onSave, label = t("Amount") }: { value: number; onSave: (n: number) => Promise<boolean>; label?: string }) {
  const [editing, setEditing] = useState(false)
  const [text, setText] = useState("")
  const commit = async () => {
    setEditing(false)
    if (text.trim() === "" || text === plain.format(value)) return
    const n = parseAmount(text)
    if (!Number.isFinite(n)) {
      toast.error(t("Write the amount as a number, such as 12.50."))
      return
    }
    await onSave(n)
  }
  return (
    <input
      className={cn("cell amount", value < 0 && !editing && "neg")}
      aria-label={label}
      inputMode="decimal"
      value={editing ? text : plainAmount(value)}
      onFocus={(e) => {
        setText(plain.format(value))
        setEditing(true)
        const el = e.target
        requestAnimationFrame(() => el.select())
      }}
      onChange={(e) => setText(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") (e.target as HTMLInputElement).blur()
        if (e.key === "Escape") {
          setText(plain.format(value))
          requestAnimationFrame(() => (e.target as HTMLInputElement).blur())
        }
      }}
    />
  )
}

/** A date, "Nov 3 Tue" (faded when the row above has the same day); clicking opens the browser's date picker. */
function DateCell({ value, onChange, label = t("Date"), faded = false }: { value: string; onChange: (d: string) => void; label?: string; faded?: boolean }) {
  const ref = useRef<HTMLInputElement>(null)
  return (
    <label
      className={cn("cell relative cursor-pointer gap-1.5 tabular-nums", faded && "opacity-45")}
      onClick={() => {
        try {
          ref.current?.showPicker?.()
        } catch {
          /* the field still takes typing */
        }
      }}
    >
      <span>{dateLabel(value, { month: "short", day: "numeric" })}</span>
      <span className="text-[12px] text-muted-foreground">{dateLabel(value, { weekday: "short" })}</span>
      <input ref={ref} type="date" aria-label={label} value={value} onChange={(e) => e.target.value && onChange(e.target.value)} className="absolute inset-0 cursor-pointer opacity-0" />
    </label>
  )
}

type Order = "date-asc" | "date-desc" | "amount-desc" | "amount-asc" | "name"

/** One of the header's figures: a small label over its value. */
function HeaderFigure({ label, title, children }: { label: string; title?: string; children: ReactNode }) {
  return (
    <div className="min-w-0" title={title}>
      <dt className="text-[12px] text-ink-2">{label}</dt>
      <dd className="mt-0.5 flex items-center gap-2 text-[18px] leading-tight font-semibold whitespace-nowrap tabular-nums">{children}</dd>
    </div>
  )
}

/** A pill holding a native select: "Type: All". */
function PillSelect({ id, label, value, onChange, options }: { id: string; label: string; value: string; onChange: (v: string) => void; options: [string, string][] }) {
  const current = options.find(([v]) => v === value)?.[1] ?? options[0]?.[1]
  const active = value !== options[0]?.[0]
  return (
    <label htmlFor={id} className={cn("pill relative cursor-pointer !pr-8", active && "!bg-wash !shadow-none")}>
      <span className="text-ink-2">{label}</span>
      <span className="max-w-40 truncate">{current}</span>
      <ChevronsUpDownIcon className="pointer-events-none absolute top-1/2 right-3 !size-3.5 -translate-y-1/2 text-ink-2" />
      <select id={id} value={value} onChange={(e) => onChange(e.target.value)} className="absolute inset-0 cursor-pointer opacity-0" aria-label={label}>
        {options.map(([v, l]) => (
          <option key={v} value={v}>{l}</option>
        ))}
      </select>
    </label>
  )
}

/** The ledger for one month; the month lives in the address (#/ledger/2026-11). */
export function LedgerView({ month, setMonth, typeParam = "" }: { month: string; setMonth: (m: string) => void; typeParam?: string }) {
  const { user, options, reloadOptions, tableVersion, quickAdd, editEntry, refund } = useApp()
  const currency = user.prefs.currency
  const [entries, setEntries] = useState<Entry[] | null>(null)
  const [stats, setStats] = useState<Stats | null>(null)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [query, setQuery] = useState("")
  const [typeFilter, setTypeFilter] = useState(typeParam)
  // A type picked in the sidebar ("This month") filters the table.
  useEffect(() => setTypeFilter(typeParam), [typeParam])
  const [cardFilter, setCardFilter] = useState("")
  const [order, setOrder] = useState<Order>("date-asc")
  const [drafting, setDrafting] = useState(false)
  const [ask, confirmEl] = useConfirm()
  const isThisMonth = month === thisMonth()

  const loadStats = useCallback(() => {
    api.stats(month, 2).then(setStats).catch(() => {})
  }, [month])

  const load = useCallback(async () => {
    try {
      const res = await api.entries(month)
      setEntries(res.entries)
    } catch (e) {
      toast.error((e as Error).message)
      setEntries([])
    }
  }, [month])

  useEffect(() => {
    setEntries(null)
    setSelected(new Set())
    void load()
    loadStats()
  }, [load, loadStats, tableVersion])

  const shown = useMemo(() => {
    if (!entries) return []
    const q = query.trim().toLowerCase()
    const by: Record<Order, (a: Entry, b: Entry) => number> = {
      "date-asc": (a, b) => a.date.localeCompare(b.date) || a.id - b.id,
      "date-desc": (a, b) => b.date.localeCompare(a.date) || b.id - a.id,
      "amount-desc": (a, b) => b.amount - a.amount || a.id - b.id,
      "amount-asc": (a, b) => a.amount - b.amount || a.id - b.id,
      name: (a, b) => a.name.localeCompare(b.name) || a.date.localeCompare(b.date),
    }
    return [...entries]
      .sort(by[order])
      .filter(
        (e) =>
          (!typeFilter || e.type === typeFilter) &&
          (!cardFilter || e.card === cardFilter) &&
          (!q || [e.name, e.type, e.card].some((f) => f.toLowerCase().includes(q))),
      )
  }, [entries, query, typeFilter, cardFilter, order])
  const total = shown.reduce((s, e) => s + Math.round(e.amount * 100), 0) / 100

  const update = async (entry: Entry, change: EntryInput): Promise<boolean> => {
    try {
      const next = await api.updateEntry(entry.id, change)
      if (!next.date.startsWith(month)) {
        setEntries((l) => (l ?? []).filter((e) => e.id !== entry.id))
        toast.success(t("Moved {name} to {month}.", { name: next.name, month: monthLabel(next.date.slice(0, 7)) }), {
          action: next.date.slice(0, 7) <= thisMonth() ? { label: t("Go there"), onClick: () => setMonth(next.date.slice(0, 7)) } : undefined,
        })
      } else {
        setEntries((l) => (l ?? []).map((e) => (e.id === entry.id ? next : e)))
      }
      if (change.type !== undefined || change.card !== undefined) void reloadOptions()
      loadStats()
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    }
  }

  const remove = (ids: number[]) => {
    ask({
      title: tn(ids.length, "Delete this entry?", "Delete {n} entries?"),
      body: t("Deleted entries can't be brought back."),
      action: t("Delete"),
      run: async () => {
        try {
          await api.deleteEntries(ids)
          setEntries((l) => (l ?? []).filter((e) => !ids.includes(e.id)))
          setSelected(new Set())
          void reloadOptions()
          loadStats()
        } catch (e) {
          toast.error((e as Error).message)
        }
      },
    })
  }

  const allSelected = shown.length > 0 && shown.every((e) => selected.has(e.id))
  const filtered = !!query.trim() || !!typeFilter || !!cardFilter
  const pct = stats && stats.prev_to_date ? ((stats.to_date - stats.prev_to_date) / Math.abs(stats.prev_to_date)) * 100 : null
  const prevName = monthName(shiftMonth(month, -1))

  return (
    <div className="mx-auto w-full max-w-[1200px] px-4 pt-7 pb-16 sm:px-8 sm:pt-11">
      {/* The month and its two figures: one row on wide screens; the figures drop below on phones. */}
      <header className="flex flex-wrap items-center justify-between gap-x-8 gap-y-3">
        <MonthHeading month={month} onChange={setMonth} />
        <dl className="flex items-stretch gap-6">
          {/* MTD while the month is still running; a past month shows its whole total. */}
          <HeaderFigure label={isThisMonth ? t("MTD") : t("Total")} title={isThisMonth ? t("Month to date") : undefined}>
            {stats ? money(stats.total, currency) : <Skeleton className="loading-in my-[3px] h-4 w-28" />}
          </HeaderFigure>
          <div aria-hidden="true" className="w-px bg-rule" />
          <HeaderFigure
            label={t("Last month")}
            title={
              stats?.compare_day
                ? t("{month} 1–{day} against the same days of {prev}", { month: monthName(month), day: stats.compare_day, prev: prevName })
                : stats
                  ? t("vs. {when}", { when: compareLabel(stats) })
                  : undefined
            }
          >
            {!stats ? (
              <Skeleton className="loading-in my-[3px] h-4 w-36" />
            ) : stats.prev_to_date ? (
              <>
                {stats.to_date >= stats.prev_to_date ? "+" : "−"}
                {money(Math.abs(stats.to_date - stats.prev_to_date), currency)}
                <span className={cn("delta !text-[12px]", stats.to_date > stats.prev_to_date ? "up" : "down")}>
                  {stats.to_date > stats.prev_to_date ? "↑" : "↓"} {Math.abs(pct ?? 0).toFixed(1)}%
                </span>
              </>
            ) : (
              <span className="text-[14px] font-normal text-muted-foreground">{t("no entries")}</span>
            )}
          </HeaderFigure>
        </dl>
      </header>

      {/* Filter, order, search, more, New; or what to do with the selected rows */}
      <div className="mt-5 flex min-h-8 flex-wrap items-center gap-2">
        {selected.size > 0 ? (
          <>
            <span className="text-[13.5px] text-ink-2">{t("{n} selected", { n: selected.size })}</span>
            <button type="button" className="pill danger" onClick={() => remove([...selected])}>
              <Trash2Icon /> {t("Delete")}
            </button>
            <button type="button" className="pill" onClick={() => setSelected(new Set())}>
              {t("Cancel")}
            </button>
          </>
        ) : (
          <>
            <PillSelect id="table-type" label={t("Type")} value={typeFilter} onChange={setTypeFilter} options={[["", t("All")], ...options.types.map((t) => [t.name, t.name] as [string, string])]} />
            <PillSelect id="table-card" label={t("Card")} value={cardFilter} onChange={setCardFilter} options={[["", t("All")], ...options.cards.map((c) => [c.name, c.name] as [string, string])]} />
            <PillSelect
              id="table-order"
              label={t("Order")}
              value={order}
              onChange={(v) => setOrder(v as Order)}
              options={[
                ["date-asc", t("Date, oldest first")],
                ["date-desc", t("Date, newest first")],
                ["amount-desc", t("Amount, largest first")],
                ["amount-asc", t("Amount, smallest first")],
                ["name", t("Name, A to Z")],
              ]}
            />
            {(typeFilter || cardFilter || order !== "date-asc") && (
              <button type="button" className="h-8 rounded-full px-2.5 text-[13.5px] text-ink-2 hover:bg-wash hover:text-ink" onClick={() => { setTypeFilter(""); setCardFilter(""); setOrder("date-asc") }}>
                {t("Reset")}
              </button>
            )}
            <span className="flex-1" />
            {/* Search: an icon that opens into a field on hover (after a beat, so passing over it doesn't) or focus,
                and stays open while it holds text. The field eases open; its text fades in once there's room. */}
            <label
              title={t("Search")}
              data-open={query !== "" || undefined}
              className={cn(
                "group/search flex h-8 w-8 shrink-0 cursor-text items-center overflow-hidden rounded-full bg-sheet pl-2 text-ink-2",
                "shadow-[inset_0_0_0_1px_var(--rule-2)] transition-[width,background-color,box-shadow,color] duration-200 ease-[cubic-bezier(.32,.72,0,1)]",
                "hover:w-52 hover:bg-wash hover:text-ink hover:shadow-[inset_0_0_0_1px_transparent] hover:delay-100 hover:duration-300",
                "focus-within:w-52 focus-within:bg-sheet focus-within:text-ink focus-within:shadow-[inset_0_0_0_1.5px_var(--ink)] focus-within:delay-0 focus-within:duration-300",
                "data-open:w-52 data-open:bg-wash data-open:shadow-[inset_0_0_0_1px_transparent]",
                "motion-reduce:transition-none",
              )}
            >
              <SearchIcon className="size-4 shrink-0 transition-transform duration-300 ease-[cubic-bezier(.32,.72,0,1)] group-hover/search:scale-90 group-focus-within/search:scale-90" />
              <input
                id="table-search"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => e.key === "Escape" && (setQuery(""), (e.target as HTMLInputElement).blur())}
                placeholder={t("Search")}
                aria-label={t("Search")}
                className={cn(
                  "ml-2 h-full w-full min-w-0 bg-transparent pr-3 text-[14px] text-ink outline-none placeholder:text-ink-2",
                  "-translate-x-1.5 opacity-0 transition-[opacity,transform] duration-150 ease-out",
                  "group-hover/search:translate-x-0 group-hover/search:opacity-100 group-hover/search:delay-200 group-hover/search:duration-300",
                  "group-focus-within/search:translate-x-0 group-focus-within/search:opacity-100 group-focus-within/search:delay-100 group-focus-within/search:duration-300",
                  "group-data-open/search:translate-x-0 group-data-open/search:opacity-100",
                  "motion-reduce:transition-none",
                )}
              />
            </label>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button type="button" className="pill !w-8 justify-center !px-0" aria-label={t("More actions")}>
                  <EllipsisIcon />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-auto min-w-44 rounded-[12px] border-0 shadow-[0_16px_48px_rgb(0_0_0/.18),0_0_0_1px_var(--rule-2)] ring-0">
                <DropdownMenuItem asChild>
                  <a href={api.exportURL(month)} download>
                    <DownloadIcon /> {t("Export as CSV")}
                  </a>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <button type="button" className="pill shrink-0" onClick={refund} title={t("Refund (R)")}>
              <Undo2Icon /> {t("Refund")}
            </button>
            <button type="button" className="pill dark shrink-0" onClick={quickAdd} title={t("New entry (N)")}>
              <PlusIcon /> {t("New")}
            </button>
          </>
        )}
      </div>

      {/* The table, from tablets up */}
      <div className="card mt-3 hidden overflow-x-auto px-2 pt-1 pb-1 md:block">
        <table className="grid-table min-w-[760px]">
          <colgroup>
            <col style={{ width: 32 }} />
            <col />
            <col style={{ width: 170 }} />
            <col style={{ width: 140 }} />
            <col style={{ width: 140 }} />
            <col style={{ width: 170 }} />
          </colgroup>
          <thead>
            <tr>
              <th className="!px-0">
                <span className={cn("grid place-items-center", !selected.size && "opacity-0 hover:opacity-100 focus-within:opacity-100")}>
                  <Checkbox aria-label={t("Select all")} checked={allSelected} onCheckedChange={(c) => setSelected(c ? new Set(shown.map((e) => e.id)) : new Set())} />
                </span>
              </th>
              <th>{t("Name")}</th>
              <th>{t("Type")}</th>
              <th className="!text-right">
                {t("Amount")} <span className="text-muted-foreground">{currencySymbol(currency)}</span>
              </th>
              <th>{t("Date")}</th>
              <th>{t("Card")}</th>
            </tr>
          </thead>
          <tbody>
            {entries === null && <SkeletonRows />}
            {shown.map((e, i) => (
              <tr key={e.id} className="row" data-selected={selected.has(e.id)}>
                <td>
                  <span className="pick grid place-items-center">
                    <Checkbox
                      aria-label={t("Select {name}", { name: e.name })}
                      checked={selected.has(e.id)}
                      onCheckedChange={(c) =>
                        setSelected((s) => {
                          const n = new Set(s)
                          if (c) n.add(e.id)
                          else n.delete(e.id)
                          return n
                        })
                      }
                    />
                  </span>
                </td>
                <td>
                  <TextCell label={t("Name")} className="font-medium" value={e.name} onSave={(name) => update(e, { name })} />
                </td>
                <td>
                  <OptionSelect kind="type" label={t("Type")} value={e.type} options={options.types} onChange={(type) => void update(e, { type })} />
                </td>
                <td>
                  <AmountCell value={e.amount} onSave={(amount) => update(e, { amount })} />
                </td>
                <td>
                  <DateCell
                    value={e.date}
                    faded={order.startsWith("date") && i > 0 && shown[i - 1].date === e.date}
                    onChange={(d) => d !== e.date && void update(e, { date: d })}
                  />
                </td>
                <td>
                  <OptionSelect kind="card" label={t("Card")} value={e.card} options={options.cards} onChange={(card) => void update(e, { card })} />
                </td>
              </tr>
            ))}
            {drafting && entries !== null && (
              <Draft
                month={month}
                onDone={() => setDrafting(false)}
                onAdded={(e) => {
                  setEntries((l) => [...(l ?? []), e])
                  void reloadOptions()
                  loadStats()
                }}
              />
            )}
            {entries !== null && !drafting && (
              <tr>
                <td />
                <td colSpan={5}>
                  <button type="button" onClick={() => setDrafting(true)} className="flex h-[34px] w-full items-center gap-1.5 rounded-lg px-2 text-[14px] text-ink-2 hover:bg-wash hover:text-ink">
                    <PlusIcon className="size-4" /> {t("New entry")}
                  </button>
                </td>
              </tr>
            )}
          </tbody>
          {entries !== null && entries.length > 0 && (
            <tfoot>
              <tr className="border-t border-rule">
                <td />
                <td className="h-11 px-3 text-[13px] text-ink-2">{filtered ? tn(shown.length, "{n} entry shown", "{n} entries shown") : tn(shown.length, "{n} entry", "{n} entries")}</td>
                <td />
                <td className="px-3 text-right text-[14px] font-semibold tabular-nums">{money(total, currency)}</td>
                <td colSpan={2} />
              </tr>
            </tfoot>
          )}
        </table>
        {entries !== null && entries.length === 0 && !drafting && (
          <p className="px-3 pt-1 pb-3 text-[13.5px] text-ink-2">{t("Nothing for {month} yet.", { month: monthLabel(month) })}</p>
        )}
        {entries !== null && entries.length > 0 && shown.length === 0 && <p className="px-3 pt-1 pb-3 text-[13.5px] text-ink-2">{t("No entries match.")}</p>}
      </div>

      {/* Phones: a list; tap an entry to change it. */}
      <div className="card mt-3 px-4 pb-1 md:hidden">
        {entries === null && (
          <Loading className="grid gap-4 py-4">
            {[62, 45, 70, 52, 38].map((w, i) => (
              <div key={i} className="flex items-center gap-3">
                <span className="grid flex-1 gap-2">
                  <Skeleton style={{ width: `${w}%` }} />
                  <Skeleton className="h-3 w-24" />
                </span>
                <Skeleton className="w-16" />
              </div>
            ))}
          </Loading>
        )}
        {entries !== null && shown.length === 0 && (
          <p className="py-6 text-[13.5px] text-muted-foreground">{filtered ? t("No entries match.") : t("Nothing for {month} yet.", { month: monthLabel(month) })}</p>
        )}
        <div>
          {shown.map((e, i) => (
            <Fragment key={e.id}>
              {(i === 0 || shown[i - 1].date !== e.date) && <div className="pt-3 pb-1 text-[12px] text-ink-2">{dateLabel(e.date)}</div>}
              <button type="button" onClick={() => editEntry(e)} className="flex w-full items-center gap-3 border-b border-rule py-2.5 text-left active:bg-wash">
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[15px] font-medium">{e.name}</span>
                  <span className="mt-1 flex flex-wrap gap-1.5">
                    {e.type && <Tag name={e.type} color={optionColor(options, "type", e.type)} />}
                    {e.card && <Tag name={e.card} color={optionColor(options, "card", e.card)} />}
                  </span>
                </span>
                <span className={cn("text-[15px] tabular-nums", e.amount < 0 && "text-good")}>{money(e.amount, currency)}</span>
              </button>
            </Fragment>
          ))}
        </div>
        {shown.length > 0 && (
          <div className="flex items-baseline justify-between py-3 text-[13px] text-ink-2">
            <span>{tn(shown.length, "{n} entry", "{n} entries")}</span>
            <Money value={total} currency={currency} className="text-[17px] font-semibold text-ink" />
          </div>
        )}
      </div>
      {confirmEl}
    </div>
  )
}

/** Rows in the table's shape while the month loads: a name, a type, an amount, a date, a card. */
function SkeletonRows() {
  const names = [48, 30, 62, 36, 54, 28, 44, 34]
  return (
    <>
      {names.map((w, i) => (
        <tr key={i} className="loading-in" aria-hidden={i > 0 || undefined}>
          <td>{i === 0 && <span className="sr-only" role="status">{t("Loading")}</span>}</td>
          <td className="px-3">
            <Skeleton style={{ width: `${w}%`, animationDelay: `${i * 60}ms` }} />
          </td>
          <td className="px-3">
            <span className="flex items-center gap-2">
              <Skeleton className="size-2 rounded-full" style={{ animationDelay: `${i * 60}ms` }} />
              <Skeleton className="w-16" style={{ animationDelay: `${i * 60}ms` }} />
            </span>
          </td>
          <td className="px-3">
            <Skeleton className="ml-auto w-14" style={{ animationDelay: `${i * 60}ms` }} />
          </td>
          <td className="px-3">
            <Skeleton className="w-16" style={{ animationDelay: `${i * 60}ms` }} />
          </td>
          <td className="px-3">
            <span className="flex items-center gap-2">
              <Skeleton className="size-2 rounded-full" style={{ animationDelay: `${i * 60}ms` }} />
              <Skeleton className="w-20" style={{ animationDelay: `${i * 60}ms` }} />
            </span>
          </td>
        </tr>
      ))}
    </>
  )
}

/** The new row: fill it in and press Enter. It stays open for the next one; Escape closes it. */
function Draft({ month, onAdded, onDone }: { month: string; onAdded: (e: Entry) => void; onDone: () => void }) {
  const { options } = useApp()
  const [name, setName] = useState("")
  const [type, setType] = useState("")
  const [amount, setAmount] = useState("")
  const [date, setDate] = useState(today)
  const [card, setCard] = useState("")
  const [busy, setBusy] = useState(false)
  const nameRef = useRef<HTMLInputElement>(null)
  useEffect(() => nameRef.current?.focus(), [])

  const add = async () => {
    if (busy) return
    const n = parseAmount(amount)
    if (!name.trim()) {
      nameRef.current?.focus()
      return
    }
    if (!Number.isFinite(n)) {
      toast.error(t("Write the amount as a number, such as 12.50."))
      return
    }
    setBusy(true)
    try {
      const e = await api.createEntry({ name, type, amount: n, date, card })
      setName("")
      setAmount("")
      if (e.date.startsWith(month)) onAdded(e)
      else toast.success(t("Added {name} to {month}.", { name: e.name, month: monthLabel(e.date.slice(0, 7)) }))
      nameRef.current?.focus()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Enter") {
      e.preventDefault()
      void add()
    }
    if (e.key === "Escape") onDone()
  }

  return (
    <tr className="row">
      <td className="text-center">{busy && <Spinner className="mx-auto size-3.5" />}</td>
      <td>
        <input ref={nameRef} id="draft-name" className="cell" placeholder={t("Name, then Enter")} aria-label={t("New entry's name")} value={name} onChange={(e) => setName(e.target.value)} onKeyDown={onKey} maxLength={200} />
      </td>
      <td>
        <OptionSelect kind="type" label={t("New entry's type")} value={type} options={options.types} onChange={setType} />
      </td>
      <td>
        <input id="draft-amount" className="cell amount" placeholder="0.00" inputMode="decimal" aria-label={t("New entry's amount")} value={amount} onChange={(e) => setAmount(e.target.value)} onKeyDown={onKey} />
      </td>
      <td>
        <DateCell value={date} onChange={setDate} label={t("New entry's date")} />
      </td>
      <td>
        <OptionSelect kind="card" label={t("New entry's card")} value={card} options={options.cards} onChange={setCard} />
      </td>
    </tr>
  )
}
