import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

import { CalendarHeat, Donut, PaceChart, type DonutPick } from "@/components/charts"
import { EntryList } from "@/components/entry-list"
import { MonthSwitch, PageHeader } from "@/components/month-picker"
import { Loading, Skeleton } from "@/components/ui/skeleton"
import { api, compareLabel, dateLabel, money, monthLabel, monthName, shiftMonth, thisMonth, type Entry, type Stats } from "@/lib/api"
import { Delta, Money } from "@/lib/money"
import { useApp } from "@/lib/state"
import { t, tn } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { MonthlyCard, UsualCard, YearCard } from "@/views/overview-trends"

/** A part of a chart whose entries are listed: a day (key: its date), or a type or card (key: the donut's). */
type Pick = { kind: "day" | "type" | "card"; key: string; names: string[]; label: string; color?: string }

export function OverviewView() {
  const { tableVersion, ledger } = useApp()
  const currency = ledger.currency
  const [month, setMonth] = useState(thisMonth())
  const [stats, setStats] = useState<Stats | null>(null)
  const [prevStats, setPrevStats] = useState<Stats | null>(null)
  // What's picked in a chart, whose entries are listed under it.
  const [pick, setPick] = useState<Pick | null>(null)
  const [entries, setEntries] = useState<Entry[] | null>(null)
  useEffect(() => setPick(null), [month])
  const wantEntries = pick !== null
  useEffect(() => {
    if (!wantEntries) return
    let live = true
    setEntries(null)
    api
      .entries(month)
      .then((r) => live && setEntries(r.entries))
      .catch((e) => toast.error((e as Error).message))
    return () => {
      live = false
    }
  }, [wantEntries, month, tableVersion])

  useEffect(() => {
    let live = true
    Promise.all([api.stats(month, 12), api.stats(shiftMonth(month, -1), 1)])
      .then(([s, p]) => {
        if (!live) return
        setStats(s)
        setPrevStats(p)
      })
      .catch((e) => toast.error((e as Error).message))
    return () => {
      live = false
    }
  }, [month, tableVersion])

  const fmt = (v: number) => money(v, currency)
  const fmtTick = (v: number, compact: boolean) => (compact ? money(v, currency, true) : money(v, currency).replace(/[.,]00(?=\D*$)/, ""))
  // The month on screen: the one asked for once it has loaded; until then the one before, dimmed.
  const shown = stats?.month ?? month
  const stale = shown !== month
  const name = monthName(shown)
  const prevName = monthName(shiftMonth(shown, -1))
  const isThisMonth = shown === thisMonth()

  const pace = useMemo(() => {
    const cum = (s: Stats | null) => {
      let t = 0
      return (s?.days ?? []).map((d) => (t += d.total))
    }
    return { current: cum(stats), previous: cum(prevStats) }
  }, [stats, prevStats])

  const picked = (p: Pick) => (entries ?? []).filter((e) => (p.kind === "day" ? e.date === p.key : p.names.includes(p.kind === "type" ? e.type : e.card)))
  const toggleDay = (date: string) => setPick((p) => (p?.kind === "day" && p.key === date ? null : { kind: "day", key: date, names: [], label: dateLabel(date, { weekday: "short", month: "short", day: "numeric" }) }))
  const fromDonut = (kind: "type" | "card") => (d: DonutPick | null) => setPick(d && { kind, key: d.key, names: d.names, label: d.label, color: d.color })
  const list = (p: Pick) => {
    return (
      <EntryList
        title={p.label}
        color={p.color}
        entries={entries && picked(p)}
        show={{ date: p.kind !== "day", type: p.kind !== "type" || p.names.length > 1, card: p.kind !== "card" || p.names.length > 1 }}
        onClose={() => setPick(null)}
      />
    )
  }
  const shortDate = (d: string) => dateLabel(d, { month: "short", day: "numeric" })

  return (
    <div className="mx-auto w-full max-w-[1200px] px-4 pt-5 pb-16 sm:px-8">
      <PageHeader title={t("Overview")}>
        <MonthSwitch month={month} onChange={setMonth} />
      </PageHeader>

      {!stats ? (
        <OverviewSkeleton />
      ) : (
        <div className={cn("mt-5 grid grid-cols-1 gap-4", stale && "stale")} aria-busy={stale}>
          {/* The month at a glance */}
          <section className="card p-6 sm:p-7">
            <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_372px]">
            <div className="min-w-0">
              <div className="text-[14px] text-ink-2">{isThisMonth ? t("This month's spending") : t("Spending in {month}", { month: monthLabel(shown) })}</div>
              <Money value={stats.total} currency={currency} className="mt-1 block text-[34px] leading-tight font-semibold tracking-[-0.01em]" />
              <div className="mt-1.5 flex min-h-[22px] items-center gap-2 text-[13.5px] text-ink-2">
                {stats.prev_to_date ? (
                  <>
                    {t("Compared with last month")}
                    <Delta now={stats.to_date} before={stats.prev_to_date} currency={currency} title={t("vs. {when}", { when: compareLabel(stats) })} />
                  </>
                ) : (
                  <>{t("Nothing recorded in {when}", { when: compareLabel(stats) })}</>
                )}
              </div>
              {stats.avg_months > 0 && stats.avg_to_date > 0 && (
                <div className="mt-1 flex min-h-[22px] items-center gap-2 text-[13.5px] text-ink-2">
                  {t("Compared with usual")}
                  <Delta
                    now={stats.to_date}
                    before={stats.avg_to_date}
                    currency={currency}
                    title={stats.compare_day ? tn(stats.avg_months, "The month before, days 1–{d}", "The average of the {n} months before, days 1–{d} of each", { d: stats.compare_day }) : tn(stats.avg_months, "The month before", "The average of the {n} months before")}
                  />
                </div>
              )}
              <div className="mt-4">
                <PaceChart
                  current={pace.current}
                  previous={pace.previous}
                  daysInMonth={stats.days.length}
                  todayIndex={isThisMonth ? new Date().getDate() - 1 : stats.days.length - 1}
                  format={fmt}
                  formatTick={fmtTick}
                  labels={[name, prevName]}
                  compact={{ start: shortDate(stats.days[0].date), end: shortDate(stats.days[stats.days.length - 1].date) }}
                  height={230}
                />
              </div>
            </div>
            <div className="w-full max-w-[460px] min-w-0 lg:max-w-none lg:border-l lg:border-rule lg:pl-8">
              <h2 className="card-title mb-3">{t("Day by day")}</h2>
              <CalendarHeat days={stats.days} format={fmt} onPick={toggleDay} picked={pick?.kind === "day" ? pick.key : null} />
            </div>
            </div>

            {/* Where it went, and how it was paid */}
            <div className="mt-7 grid gap-x-10 gap-y-8 border-t border-rule pt-6 lg:grid-cols-2">
              <div className="min-w-0">
                <h2 className="card-title mb-4">{t("By type")}</h2>
                <Donut
                  data={stats.by_type}
                  format={fmt}
                  label={t("Spending by type")}
                  noun={(n) => tn(n, "{n} type", "{n} types")}
                  picked={pick?.kind === "type" ? pick.key : null}
                  onPick={fromDonut("type")}
                />
              </div>
              <div className="min-w-0 lg:border-l lg:border-rule lg:pl-10">
                <h2 className="card-title mb-4">{t("By card")}</h2>
                <Donut
                  data={stats.by_card}
                  format={fmt}
                  label={t("Spending by card")}
                  noun={(n) => tn(n, "{n} card", "{n} cards")}
                  picked={pick?.kind === "card" ? pick.key : null}
                  onPick={fromDonut("card")}
                />
              </div>
            </div>
          </section>

          {/* Each type against its usual month: where the spending grew */}
          <UsualCard stats={stats} currency={currency} onPickType={(name, color) => setPick({ kind: "type", key: name, names: [name], label: name || t("No type"), color: name ? color : "gray" })} />

          {/* Month by month */}
          <MonthlyCard stats={stats} month={month} currency={currency} onSelect={setMonth} />

          {/* The year against the one before */}
          <YearCard year={stats.year} currency={currency} onSelect={setMonth} />
        </div>
      )}
      {pick && list(pick)}
    </div>
  )
}

/** The overview's shape while it first loads: the figure, the chart, the calendar, two donuts, the months. */
function OverviewSkeleton() {
  return (
    <Loading className="mt-5 grid grid-cols-1 gap-4">
      <section className="card p-6 sm:p-7">
        <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_372px]">
          <div className="min-w-0">
            <Skeleton className="w-36" />
            <Skeleton className="mt-3 h-9 w-56" />
            <Skeleton className="mt-2.5 h-5 w-40" />
            <Skeleton className="mt-5 h-3 w-44" />
            <Skeleton className="mt-4 h-[230px] w-full rounded-[10px]" />
          </div>
          <div className="w-full max-w-[460px] min-w-0 lg:max-w-none lg:border-l lg:border-rule lg:pl-8">
            <Skeleton className="h-4 w-24" />
            <div className="mt-4 grid grid-cols-7 gap-[3px]">
              {Array.from({ length: 35 }, (_, i) => (
                <Skeleton key={i} className="aspect-square h-auto rounded-[5px]" style={{ animationDelay: `${(i % 7) * 40}ms` }} />
              ))}
            </div>
          </div>
        </div>
        <div className="mt-7 grid gap-x-10 gap-y-8 border-t border-rule pt-6 lg:grid-cols-2">
          {[0, 1].map((k) => (
            <div key={k} className={cn("min-w-0", k === 1 && "lg:border-l lg:border-rule lg:pl-10")}>
              <Skeleton className="h-4 w-20" />
              <div className="mt-4 grid items-center gap-x-7 gap-y-4 sm:grid-cols-[168px_minmax(0,1fr)]">
                <span className="relative mx-auto block size-[168px]">
                  <Skeleton className="absolute inset-0 h-auto rounded-full" />
                  <span className="absolute inset-[26px] rounded-full bg-sheet" />
                </span>
                <div className="grid gap-3.5">
                  {[70, 55, 62, 48, 58, 40].map((w, i) => (
                    <Skeleton key={i} className="h-3" style={{ width: `${w}%` }} />
                  ))}
                </div>
              </div>
            </div>
          ))}
        </div>
      </section>
      <section className="card p-6 sm:p-7">
        <Skeleton className="h-4 w-36" />
        <Skeleton className="mt-5 h-[200px] w-full rounded-[10px]" />
      </section>
    </Loading>
  )
}
