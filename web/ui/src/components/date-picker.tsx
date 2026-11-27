import { useEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

import { focusNext } from "@/components/option-select"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { dateLabel, monthLabel, shiftMonth, today } from "@/lib/api"
import { cn } from "@/lib/utils"
import { lang, locale, t } from "@/lib/i18n"

/** The date by days later (or, negative, earlier), YYYY-MM-DD. */
function shiftDay(date: string, by: number): string {
  const d = new Date(date + "T00:00")
  d.setDate(d.getDate() + by)
  return today(d)
}

/** The day of the week, 0 for the first in the calendar's row: Monday in Chinese, Sunday in English. */
const weekStart = () => (lang() === "zh" ? 1 : 0)
const column = (date: string) => (new Date(date + "T00:00").getDay() - weekStart() + 7) % 7

/**
 * A date as one field: a button showing it, which opens a month's calendar to
 * pick from. Reached with Tab it's one stop, not three (day, month, year), and
 * opens the calendar (or, with openOnFocus off, Enter, Space or ↓ does), where
 * the arrows move by a day or a week, Page Up and Page Down by a month, Enter
 * picks, and Tab moves on.
 */
export function DatePicker({
  value,
  onChange,
  label,
  min,
  max,
  clearable = false,
  placeholder = "",
  className,
  children,
  autoFocus,
  openOnFocus = true,
}: {
  /** YYYY-MM-DD, or "" when none is set (with clearable). */
  value: string
  onChange: (date: string) => void
  label: string
  min?: string
  max?: string
  /** Offer a Clear button, which sets "". */
  clearable?: boolean
  placeholder?: string
  className?: string
  /** What the button shows instead of the date. */
  children?: ReactNode
  autoFocus?: boolean
  /** Open the calendar when the field is reached with Tab. */
  openOnFocus?: boolean
}) {
  const [open, setOpen] = useState(false)
  // The day the keyboard is on, and so the month shown.
  const [active, setActive] = useState(value || today())
  const [month, setMonth] = useState(active.slice(0, 7))
  const gridRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  // Tab out of the calendar moves the focus on itself, rather than back to the button.
  const tabbing = useRef(false)
  // Closing hands the focus back to the button: it mustn't open the calendar again then.
  const justClosed = useRef(false)
  const now = today()
  const outside = (d: string) => (!!min && d < min) || (!!max && d > max)

  const show = (o: boolean) => {
    if (o) {
      let start = value || now
      if (min && start < min) start = min
      if (max && start > max) start = max
      setActive(start)
      setMonth(start.slice(0, 7))
      tabbing.current = false
    } else justClosed.current = true
    setOpen(o)
  }
  const pick = (d: string) => {
    if (outside(d)) return
    onChange(d)
    show(false)
  }
  const move = (d: string) => {
    if (outside(d)) return
    setActive(d)
    setMonth(d.slice(0, 7))
  }
  // The keyboard's day keeps the focus as it moves.
  useEffect(() => {
    if (open) gridRef.current?.querySelector<HTMLElement>(`[data-day="${active}"]`)?.focus()
  }, [open, active])

  const onKey = (e: KeyboardEvent) => {
    const by: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }
    if (e.key in by) {
      e.preventDefault()
      move(shiftDay(active, by[e.key]))
    } else if (e.key === "PageUp" || e.key === "PageDown") {
      e.preventDefault()
      move(clampTo(shiftMonth(month, e.key === "PageUp" ? -1 : 1), active, min, max))
    } else if (e.key === "Home" || e.key === "End") {
      e.preventDefault()
      move(shiftDay(active, e.key === "Home" ? -column(active) : 6 - column(active)))
    } else if (e.key === "Tab") {
      e.preventDefault()
      tabbing.current = true
      show(false)
      const from = triggerRef.current
      requestAnimationFrame(() => {
        // The focus went on, not back to the button: coming back to it later opens the calendar again.
        justClosed.current = false
        if (from) focusNext(from, e.shiftKey ? -1 : 1)
      })
    }
  }

  // The month's days, from the start of its first week to the end of its last.
  const first = `${month}-01`
  const lead = column(first)
  const days = new Date(Number(month.slice(0, 4)), Number(month.slice(5, 7)), 0).getDate()
  const cells = Array.from({ length: Math.ceil((lead + days) / 7) * 7 }, (_, i) => shiftDay(first, i - lead))
  const weekdays = Array.from({ length: 7 }, (_, i) => cells[i])
  const arrow = "grid size-7 place-items-center rounded-full text-ink-2 hover:bg-wash hover:text-ink disabled:pointer-events-none disabled:opacity-30"

  return (
    <Popover open={open} onOpenChange={show}>
      <PopoverTrigger asChild>
        <button
          ref={triggerRef}
          type="button"
          autoFocus={autoFocus}
          aria-label={value ? `${label}: ${dateLabel(value, { weekday: "long", month: "long", day: "numeric", year: "numeric" })}` : label}
          className={cn("text-left tabular-nums", className)}
          onFocus={(e) => {
            // Reached by keyboard (not a click, not the focus coming back from the calendar): open.
            if (openOnFocus && !open && !justClosed.current && e.currentTarget.matches(":focus-visible")) show(true)
          }}
          onBlur={() => (justClosed.current = false)}
          onKeyDown={(e) => {
            if (e.key === "ArrowDown" && !open) {
              e.preventDefault()
              show(true)
            }
          }}
        >
          {children ?? (value ? dateLabel(value) : <span className="text-muted-foreground/70">{placeholder}</span>)}
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        sideOffset={4}
        className="w-[17rem] gap-1.5 rounded-[14px] border border-rule-2 p-2 shadow-[0_16px_48px_rgb(0_0_0/.18)] ring-0"
        onOpenAutoFocus={(e) => e.preventDefault()}
        onCloseAutoFocus={(e) => tabbing.current && e.preventDefault()}
        onKeyDown={onKey}
      >
        <div className="flex items-center justify-between">
          <button type="button" tabIndex={-1} aria-label={t("Previous month")} disabled={!!min && first <= min} onClick={() => move(clampTo(shiftMonth(month, -1), active, min, max))} className={arrow}>
            <ChevronLeftIcon className="size-4" />
          </button>
          <span className="text-[14px] font-semibold" aria-live="polite">{monthLabel(month)}</span>
          <button type="button" tabIndex={-1} aria-label={t("Next month")} disabled={!!max && `${month}-${String(days).padStart(2, "0")}` >= max} onClick={() => move(clampTo(shiftMonth(month, 1), active, min, max))} className={arrow}>
            <ChevronRightIcon className="size-4" />
          </button>
        </div>
        <div ref={gridRef} role="grid" aria-label={monthLabel(month)} className="grid grid-cols-7 gap-y-0.5">
          {weekdays.map((d) => (
            <span key={d} role="columnheader" className="grid h-7 place-items-center text-[11.5px] text-muted-foreground">
              {new Date(d + "T00:00").toLocaleDateString(locale(), { weekday: "narrow" })}
            </span>
          ))}
          {cells.map((d) => {
            const inMonth = d.slice(0, 7) === month
            return (
              <button
                key={d}
                type="button"
                role="gridcell"
                data-day={d}
                tabIndex={d === active ? 0 : -1}
                disabled={outside(d)}
                aria-selected={d === value}
                aria-label={dateLabel(d, { weekday: "long", month: "long", day: "numeric", year: "numeric" })}
                onClick={() => pick(d)}
                className={cn(
                  "mx-auto grid size-8 place-items-center rounded-full text-[13.5px] tabular-nums outline-none hover:bg-wash focus-visible:shadow-[inset_0_0_0_1.5px_color-mix(in_srgb,var(--blue)_55%,transparent)] disabled:pointer-events-none disabled:opacity-30",
                  !inMonth && "text-muted-foreground/60",
                  d === now && d !== value && "font-semibold text-blue",
                  d === value && "bg-ink text-paper hover:bg-ink",
                )}
              >
                {Number(d.slice(8))}
              </button>
            )
          })}
        </div>
        <div className="flex gap-1">
          {!outside(now) && (
            <button type="button" tabIndex={-1} className="h-8 flex-1 rounded-full text-[13.5px] hover:bg-wash" onClick={() => pick(now)}>
              {t("Today")}
            </button>
          )}
          {clearable && value && (
            <button
              type="button"
              tabIndex={-1}
              className="h-8 flex-1 rounded-full text-[13.5px] text-ink-2 hover:bg-wash"
              onClick={() => {
                onChange("")
                show(false)
              }}
            >
              {t("Clear")}
            </button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  )
}

/** The same day of the month in another month (or its last day), kept within min and max. */
function clampTo(month: string, day: string, min?: string, max?: string): string {
  const days = new Date(Number(month.slice(0, 4)), Number(month.slice(5, 7)), 0).getDate()
  let d = `${month}-${String(Math.min(Number(day.slice(8)), days)).padStart(2, "0")}`
  if (min && d < min) d = min
  if (max && d > max) d = max
  return d
}
