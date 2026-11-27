// The ledger's filters and sort, the way Notion does them: a Filter and a Sort
// button, each opening a line under the toolbar. The filter line holds a chip
// per filter (Name contains…, Type is…, Amount between…, Date between…, Card
// is…) and "+ Filter"; the sort line holds the sort and "Set to default".
import { useState, type ReactNode } from "react"
import { ArrowUpDownIcon, CalendarIcon, CheckIcon, ChevronDownIcon, CreditCardIcon, HashIcon, PlusIcon, TagIcon, Trash2Icon, TypeIcon, type LucideIcon } from "lucide-react"

import { DatePicker } from "@/components/date-picker"
import { parseAmount } from "@/components/entry-dialog"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { dateLabel, type Entry, type LedgerOrder, type Option } from "@/lib/api"
import { t } from "@/lib/i18n"
import { matches } from "@/lib/search"
import { isInactive } from "@/lib/options"
import { cn } from "@/lib/utils"

export type FilterField = "name" | "type" | "amount" | "date" | "card"

export interface Filter {
  id: string
  field: FilterField
  text?: string // name: contains
  values?: string[] // type, card: is any of ("" for none)
  min?: string // amount
  max?: string
  from?: string // date, YYYY-MM-DD
  to?: string
}

const FIELDS: { field: FilterField; label: () => string; icon: LucideIcon }[] = [
  { field: "name", label: () => t("Name"), icon: TypeIcon },
  { field: "type", label: () => t("Type"), icon: TagIcon },
  { field: "amount", label: () => t("Amount"), icon: HashIcon },
  { field: "date", label: () => t("Date"), icon: CalendarIcon },
  { field: "card", label: () => t("Card"), icon: CreditCardIcon },
]
const fieldOf = (f: FilterField) => FIELDS.find((x) => x.field === f)!

export const ORDERS: { order: LedgerOrder; label: () => string }[] = [
  { order: "date-asc", label: () => t("Date, oldest first") },
  { order: "date-desc", label: () => t("Date, newest first") },
  { order: "amount-desc", label: () => t("Amount, largest first") },
  { order: "amount-asc", label: () => t("Amount, smallest first") },
  { order: "name", label: () => t("Name, A to Z") },
]

let nextID = 0
export function newFilter(field: FilterField, init: Partial<Filter> = {}): Filter {
  return { id: `f${++nextID}`, field, ...init }
}

const num = (s?: string) => (s && s.trim() ? parseAmount(s) : NaN)

/** Whether a filter says anything yet (an empty one passes everything). */
export function isActive(f: Filter): boolean {
  switch (f.field) {
    case "name":
      return !!f.text?.trim()
    case "type":
    case "card":
      return !!f.values?.length
    case "amount":
      return Number.isFinite(num(f.min)) || Number.isFinite(num(f.max))
    case "date":
      return !!f.from || !!f.to
  }
}

/** Whether an entry passes every filter. */
export function passes(e: Entry, filters: Filter[]): boolean {
  return filters.every((f) => {
    if (!isActive(f)) return true
    switch (f.field) {
      case "name":
        return matches(e.name, f.text!)
      case "type":
        return f.values!.includes(e.type)
      case "card":
        return f.values!.includes(e.card)
      case "amount": {
        const lo = num(f.min), hi = num(f.max)
        return (!Number.isFinite(lo) || e.amount >= lo) && (!Number.isFinite(hi) || e.amount <= hi)
      }
      case "date":
        return (!f.from || e.date >= f.from) && (!f.to || e.date <= f.to)
    }
  })
}

/** What a chip says after its field: "coffee", "Food, Shopping", "10–50", "Nov 3–10". */
function summary(f: Filter): string {
  if (!isActive(f)) return ""
  const short = (d: string) => dateLabel(d, { month: "short", day: "numeric" })
  switch (f.field) {
    case "name":
      return `“${f.text!.trim()}”`
    case "type":
    case "card":
      return f.values!.map((v) => v || t("None")).join(", ")
    case "amount": {
      const lo = num(f.min), hi = num(f.max)
      if (Number.isFinite(lo) && Number.isFinite(hi)) return `${lo}–${hi}`
      return Number.isFinite(lo) ? `≥ ${lo}` : `≤ ${hi}`
    }
    case "date":
      if (f.from && f.to) return `${short(f.from)} – ${short(f.to)}`
      return f.from ? `≥ ${short(f.from)}` : `≤ ${short(f.to!)}`
  }
}

const popover = "w-64 gap-0 overflow-hidden rounded-[14px] border border-rule-2 p-0 shadow-[0_16px_48px_rgb(0_0_0/.18)] ring-0"
const chip = "chip-in flex h-7 max-w-72 items-center gap-1.5 rounded-full px-3 text-[13px] transition-colors"

/**
 * The toolbar's Filter or Sort button: an icon, its name sliding open on hover
 * (or keyboard focus); highlighted while what it opens is shown or doing
 * something, with a count of active filters on its corner.
 */
export function ToolbarToggle({ icon: Icon, label, count, on, active, onClick }: { icon: LucideIcon; label: string; count?: number; on: boolean; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      aria-expanded={on}
      aria-label={count ? `${label} (${count})` : label}
      onClick={onClick}
      className={cn("pill group/tt relative !gap-0 !px-2", (on || active) && "!bg-wash !shadow-none", active && "text-blue")}
    >
      <Icon />
      <span className="max-w-0 overflow-hidden whitespace-nowrap opacity-0 transition-[max-width,opacity,margin] duration-200 ease-[cubic-bezier(.32,.72,0,1)] group-hover/tt:ml-1.5 group-hover/tt:max-w-24 group-hover/tt:opacity-100 group-hover/tt:delay-75 group-focus-visible/tt:ml-1.5 group-focus-visible/tt:max-w-24 group-focus-visible/tt:opacity-100 motion-reduce:transition-none">
        {label}
      </span>
      {!!count && (
        // Keyed by the count, so it pops each time the number changes.
        <span key={count} className="badge-pop absolute -top-1 -right-1 grid h-4 min-w-4 place-items-center rounded-full bg-blue px-1 text-[10.5px] font-semibold text-paper shadow-[0_0_0_2px_var(--paper)]">
          {count}
        </span>
      )}
    </button>
  )
}

/** The filter line: a chip per filter, then "+ Filter". */
export function FilterLine({
  filters,
  setFilters,
  types,
  cards,
  month,
  openID,
  setOpenID,
  adding,
  setAdding,
}: {
  filters: Filter[]
  setFilters: (fn: (l: Filter[]) => Filter[]) => void
  types: Option[]
  cards: Option[]
  month: string
  openID: string | null
  setOpenID: (id: string | null) => void
  /** The "+ Filter" menu, open. */
  adding: boolean
  setAdding: (o: boolean) => void
}) {
  const change = (id: string, patch: Partial<Filter>) => setFilters((l) => l.map((f) => (f.id === id ? { ...f, ...patch } : f)))
  const remove = (id: string) => setFilters((l) => l.filter((f) => f.id !== id))
  return (
    <div role="group" aria-label={t("Filters")} className="flex flex-wrap items-center gap-1.5">
      {filters.map((f) => {
        const { label, icon: Icon } = fieldOf(f.field)
        const said = summary(f)
        return (
          <Popover
            key={f.id}
            open={openID === f.id}
            onOpenChange={(o) => {
              setOpenID(o ? f.id : null)
              // A filter closed without saying anything goes away.
              if (!o && !isActive(f)) remove(f.id)
            }}
          >
            <PopoverTrigger asChild>
              <button type="button" className={cn(chip, said ? "bg-blue-wash text-blue" : "shadow-[inset_0_0_0_1px_var(--rule-2)] text-ink-2 hover:bg-wash")}>
                <Icon className="size-3.5 shrink-0" />
                <span className="shrink-0">
                  {label()}
                  {said && ":"}
                </span>
                {said && <span className="-ml-0.5 min-w-0 truncate font-medium">{said}</span>}
                <ChevronDownIcon className="size-3.5 shrink-0 opacity-60" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="start" sideOffset={6} className={popover}>
              <FilterEditor filter={f} types={types} cards={cards} month={month} onChange={(p) => change(f.id, p)} />
              <button
                type="button"
                onClick={() => {
                  remove(f.id)
                  setOpenID(null)
                }}
                className="flex h-9 w-full items-center gap-2 border-t border-rule px-3 text-[13px] text-ink-2 hover:bg-wash hover:text-red"
              >
                <Trash2Icon className="size-3.5" /> {t("Remove filter")}
              </button>
            </PopoverContent>
          </Popover>
        )
      })}
      <Popover open={adding} onOpenChange={setAdding}>
        <PopoverTrigger asChild>
          <button type="button" className={cn(chip, "text-ink-2 hover:bg-wash hover:text-ink")}>
            <PlusIcon className="size-3.5" /> {t("Filter")}
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" sideOffset={6} className={cn(popover, "w-48 p-1")}>
          {FIELDS.map(({ field, label, icon: Icon }) => (
            <button
              key={field}
              type="button"
              onClick={() => {
                const f = newFilter(field)
                setFilters((l) => [...l, f])
                setAdding(false)
                // Open the new filter to fill it in, once the menu has closed.
                setTimeout(() => setOpenID(f.id), 0)
              }}
              className="flex h-8 w-full items-center gap-2.5 rounded-[8px] px-2.5 text-left text-[13.5px] hover:bg-wash"
            >
              <Icon className="size-4 text-ink-2" /> {label()}
            </button>
          ))}
        </PopoverContent>
      </Popover>
    </div>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="grid gap-1">
      <span className="text-[12px] text-ink-2">{label}</span>
      {children}
    </label>
  )
}

const input = "h-8 w-full min-w-0 rounded-[8px] bg-wash px-2.5 text-[13.5px] outline-none focus:bg-sheet focus:shadow-[inset_0_0_0_1.5px_var(--ink)]"

/** What a filter's chip opens: a text, a list to tick, or two bounds. */
function FilterEditor({ filter: f, types, cards, month, onChange }: { filter: Filter; types: Option[]; cards: Option[]; month: string; onChange: (p: Partial<Filter>) => void }) {
  const [query, setQuery] = useState("")
  switch (f.field) {
    case "name":
      return (
        <div className="p-3">
          <Field label={t("Name contains")}>
            <input autoFocus className={input} value={f.text ?? ""} onChange={(e) => onChange({ text: e.target.value })} />
          </Field>
        </div>
      )
    case "amount":
      return (
        <div className="grid grid-cols-2 gap-2 p-3">
          <Field label={t("At least")}>
            <input autoFocus inputMode="decimal" className={cn(input, "tabular-nums")} placeholder="0.00" value={f.min ?? ""} onChange={(e) => onChange({ min: e.target.value })} />
          </Field>
          <Field label={t("At most")}>
            <input inputMode="decimal" className={cn(input, "tabular-nums")} placeholder="0.00" value={f.max ?? ""} onChange={(e) => onChange({ max: e.target.value })} />
          </Field>
        </div>
      )
    case "date": {
      // Within the month on screen.
      const first = `${month}-01`
      const [y, m] = month.split("-").map(Number)
      const last = `${month}-${String(new Date(y, m, 0).getDate()).padStart(2, "0")}`
      return (
        <div className="grid gap-2 p-3">
          <Field label={t("From")}>
            <DatePicker autoFocus clearable placeholder={t("Any day")} min={first} max={last} label={t("From")} className={input} value={f.from ?? ""} onChange={(d) => onChange({ from: d })} />
          </Field>
          <Field label={t("To")}>
            <DatePicker clearable placeholder={t("Any day")} min={first} max={last} label={t("To")} className={input} value={f.to ?? ""} onChange={(d) => onChange({ to: d })} />
          </Field>
        </div>
      )
    }
    case "type":
    case "card": {
      const values = f.values ?? []
      // Inactive ones only when chosen already, or searched for.
      const list = [...(f.field === "type" ? types : cards).filter((o) => !isInactive(o) || values.includes(o.name) || query.trim()).map((o) => ({ name: o.name, color: o.color })), { name: "", color: "gray" }]
      const shown = list.filter((o) => !query.trim() || matches(o.name || t("None"), query))
      const toggle = (v: string) => onChange({ values: values.includes(v) ? values.filter((x) => x !== v) : [...values, v] })
      return (
        <>
          <input autoFocus value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("Search")} className="h-10 w-full border-b border-rule bg-transparent px-3.5 text-[14px] outline-none placeholder:text-muted-foreground" />
          <div className="max-h-64 overflow-y-auto p-1" role="listbox" aria-multiselectable="true" aria-label={fieldOf(f.field).label()}>
            {shown.map((o) => (
              <button key={o.name || "(none)"} type="button" role="option" aria-selected={values.includes(o.name)} onClick={() => toggle(o.name)} className="flex h-8 w-full items-center gap-2 rounded-[8px] px-2.5 text-left text-[13.5px] hover:bg-wash" data-color={o.color}>
                <span className="dot" />
                <span className={cn("min-w-0 flex-1 truncate", !o.name && "text-ink-2")}>{o.name || t("None")}</span>
                {values.includes(o.name) && <CheckIcon className="size-4" />}
              </button>
            ))}
          </div>
        </>
      )
    }
  }
}

/** The sort line: the sort, and "Set to default" while it differs from the saved one. */
export function SortLine({ order, setOrder, savedOrder, onSetDefault, open, setOpen }: { order: LedgerOrder; setOrder: (o: LedgerOrder) => void; savedOrder: LedgerOrder; onSetDefault: () => void; open: boolean; setOpen: (o: boolean) => void }) {
  const label = ORDERS.find((o) => o.order === order)?.label() ?? ""
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button type="button" className={cn(chip, order !== savedOrder ? "bg-blue-wash text-blue" : "shadow-[inset_0_0_0_1px_var(--rule-2)] text-ink-2 hover:bg-wash")}>
            <ArrowUpDownIcon className="size-3.5" />
            <span className="font-medium">{label}</span>
            <ChevronDownIcon className="size-3.5 opacity-60" />
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" sideOffset={6} className={cn(popover, "w-56 p-1")}>
          <div role="listbox" aria-label={t("Sort")}>
            {ORDERS.map((o) => (
              <button
                key={o.order}
                type="button"
                role="option"
                aria-selected={o.order === order}
                onClick={() => {
                  setOrder(o.order)
                  setOpen(false)
                }}
                className="flex h-8 w-full items-center gap-2 rounded-[8px] px-2.5 text-left text-[13.5px] hover:bg-wash"
              >
                <span className="flex-1">{o.label()}</span>
                {o.order === savedOrder && <span className="text-[11.5px] text-muted-foreground">{t("Default")}</span>}
                {o.order === order && <CheckIcon className="size-4" />}
              </button>
            ))}
          </div>
        </PopoverContent>
      </Popover>
      {order !== savedOrder && (
        <button type="button" onClick={onSetDefault} className={cn(chip, "font-medium text-ink hover:bg-wash")}>
          {t("Set to default")}
        </button>
      )}
    </div>
  )
}
