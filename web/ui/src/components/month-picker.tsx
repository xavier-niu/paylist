import { useState, type ReactNode } from "react"
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { monthLabel, shiftMonth, thisMonth } from "@/lib/api"
import { cn } from "@/lib/utils"
import { t } from "@/lib/i18n"

/** A page's top line: its title and a few facts on the left, its controls on the right. */
export function PageHeader({ title, meta, children }: { title: ReactNode; meta?: ReactNode; children?: ReactNode }) {
  return (
    <div className="flex min-h-9 flex-wrap items-center justify-between gap-x-4 gap-y-3">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
        <h1 className="text-[17px] font-semibold">{title}</h1>
        {meta && <div className="text-[13px] text-ink-2 tabular-nums">{meta}</div>}
      </div>
      {children && <div className="flex flex-wrap items-center gap-2">{children}</div>}
    </div>
  )
}

/** ‹ November 2026 › in a pill; the name opens a year of months to jump to. */
export function MonthSwitch({ month, onChange }: { month: string; onChange: (m: string) => void }) {
  const [open, setOpen] = useState(false)
  const [year, setYear] = useState(Number(month.slice(0, 4)))
  const now = thisMonth()
  const arrow = "grid size-7 place-items-center rounded-full text-ink-2 hover:bg-wash hover:text-ink disabled:pointer-events-none disabled:opacity-30"
  return (
    <div className="flex h-8 items-center rounded-full bg-sheet px-0.5 shadow-[inset_0_0_0_1px_var(--rule-2)]">
      <button type="button" aria-label={t("Previous month")} onClick={() => onChange(shiftMonth(month, -1))} className={arrow}>
        <ChevronLeftIcon className="size-4" />
      </button>
      <Popover
        open={open}
        onOpenChange={(o) => {
          setOpen(o)
          if (o) setYear(Number(month.slice(0, 4)))
        }}
      >
        <PopoverTrigger asChild>
          <button type="button" className="h-7 min-w-[118px] rounded-full px-2 text-[14px] hover:bg-wash" aria-label={t("{month}. Choose another month", { month: monthLabel(month) })}>
            {monthLabel(month)}
          </button>
        </PopoverTrigger>
        <PopoverContent align="center" sideOffset={8} className="w-64 gap-2 rounded-[14px] border border-rule-2 p-2 shadow-[0_16px_48px_rgb(0_0_0/.18)] ring-0">
          <div className="flex items-center justify-between">
            <button type="button" aria-label={t("Previous year")} onClick={() => setYear((y) => y - 1)} className={arrow}>
              <ChevronLeftIcon className="size-4" />
            </button>
            <span className="font-semibold tabular-nums">{year}</span>
            <button type="button" aria-label={t("Next year")} disabled={year >= Number(now.slice(0, 4))} onClick={() => setYear((y) => y + 1)} className={arrow}>
              <ChevronRightIcon className="size-4" />
            </button>
          </div>
          <div className="grid grid-cols-3 gap-1">
            {Array.from({ length: 12 }, (_, i) => {
              const m = `${year}-${String(i + 1).padStart(2, "0")}`
              return (
                <button
                  key={m}
                  type="button"
                  disabled={m > now}
                  onClick={() => {
                    onChange(m)
                    setOpen(false)
                  }}
                  className={cn("h-8 rounded-full text-[13.5px] hover:bg-wash disabled:pointer-events-none disabled:text-muted-foreground/50", m === month && "bg-ink text-paper hover:bg-ink", m === now && m !== month && "font-semibold")}
                >
                  {monthLabel(m, "short")}
                </button>
              )
            })}
          </div>
          {month !== now && (
            <button type="button" className="h-8 rounded-full text-[13.5px] hover:bg-wash" onClick={() => { onChange(now); setOpen(false) }}>
              {t("This month")}
            </button>
          )}
        </PopoverContent>
      </Popover>
      <button type="button" aria-label={t("Next month")} disabled={month >= now} onClick={() => onChange(shiftMonth(month, 1))} className={arrow}>
        <ChevronRightIcon className="size-4" />
      </button>
    </div>
  )
}

/** The month as a page's big title: click it to pick another month. */
export function MonthHeading({ month, onChange }: { month: string; onChange: (m: string) => void }) {
  const [open, setOpen] = useState(false)
  const [year, setYear] = useState(Number(month.slice(0, 4)))
  const now = thisMonth()
  const arrow = "grid size-8 shrink-0 place-items-center rounded-full text-ink-2 hover:bg-wash hover:text-ink disabled:pointer-events-none disabled:opacity-30"
  return (
    <div className="flex min-w-0 items-center gap-0.5 sm:gap-1">
      <Popover
        open={open}
        onOpenChange={(o) => {
          setOpen(o)
          if (o) setYear(Number(month.slice(0, 4)))
        }}
      >
        <PopoverTrigger asChild>
          <button type="button" className="-ml-2 truncate rounded-[10px] px-2 text-[24px] leading-tight font-semibold tracking-[-0.015em] hover:bg-wash sm:text-[32px]" aria-label={t("{month}. Choose another month", { month: monthLabel(month) })}>
            {monthLabel(month)}
          </button>
        </PopoverTrigger>
        <PopoverContent align="start" sideOffset={8} className="w-64 gap-2 rounded-[14px] border border-rule-2 p-2 shadow-[0_16px_48px_rgb(0_0_0/.18)] ring-0">
          <div className="flex items-center justify-between">
            <button type="button" aria-label={t("Previous year")} onClick={() => setYear((y) => y - 1)} className={arrow}>
              <ChevronLeftIcon className="size-4" />
            </button>
            <span className="font-semibold tabular-nums">{year}</span>
            <button type="button" aria-label={t("Next year")} disabled={year >= Number(now.slice(0, 4))} onClick={() => setYear((y) => y + 1)} className={arrow}>
              <ChevronRightIcon className="size-4" />
            </button>
          </div>
          <div className="grid grid-cols-3 gap-1">
            {Array.from({ length: 12 }, (_, i) => {
              const m = `${year}-${String(i + 1).padStart(2, "0")}`
              return (
                <button
                  key={m}
                  type="button"
                  disabled={m > now}
                  onClick={() => {
                    onChange(m)
                    setOpen(false)
                  }}
                  className={cn("h-8 rounded-full text-[13.5px] hover:bg-wash disabled:pointer-events-none disabled:text-muted-foreground/50", m === month && "bg-ink text-paper hover:bg-ink", m === now && m !== month && "font-semibold")}
                >
                  {monthLabel(m, "short")}
                </button>
              )
            })}
          </div>
        </PopoverContent>
      </Popover>
      {month !== now && (
        <button type="button" className="pill soft !ml-2 !h-7 shrink-0 !px-3 text-[13px] max-sm:hidden" onClick={() => onChange(now)}>
          {t("This month")}
        </button>
      )}
    </div>
  )
}
