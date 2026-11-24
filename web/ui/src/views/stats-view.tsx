import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

import { CalendarHeat, ColumnChart, Donut, PaceChart } from "@/components/charts"
import { MonthSwitch, PageHeader } from "@/components/month-picker"
import { Spinner } from "@/components/ui/spinner"
import { api, compareLabel, dateLabel, money, monthLabel, monthName, shiftMonth, thisMonth, today, type Stats } from "@/lib/api"
import { Delta, Money } from "@/lib/money"
import { useApp } from "@/lib/state"
import { t, tn } from "@/lib/i18n"

export function StatsView() {
  const { user, tableVersion } = useApp()
  const currency = user.prefs.currency
  const [month, setMonth] = useState(thisMonth())
  const [stats, setStats] = useState<Stats | null>(null)
  const [prevStats, setPrevStats] = useState<Stats | null>(null)

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
  const name = monthName(month)
  const prevName = monthName(shiftMonth(month, -1))
  const isThisMonth = month === thisMonth()

  const pace = useMemo(() => {
    const cum = (s: Stats | null) => {
      let t = 0
      return (s?.days ?? []).map((d) => (t += d.total))
    }
    return { current: cum(stats), previous: cum(prevStats) }
  }, [stats, prevStats])

  const ready = stats && stats.month === month
  // This month: the 30 days up to today, reaching into the month before. A past month: all its days.
  const recent = useMemo(() => {
    if (!stats || !prevStats) return []
    if (month !== thisMonth()) return stats.days
    return [...prevStats.days, ...stats.days].filter((d) => d.date <= today()).slice(-30)
  }, [stats, prevStats, month])
  const shortDate = (d: string) => dateLabel(d, { month: "short", day: "numeric" })

  return (
    <div className="mx-auto w-full max-w-[1200px] px-4 pt-5 pb-16 sm:px-8">
      <PageHeader title={t("Statistics")}>
        <MonthSwitch month={month} onChange={setMonth} />
      </PageHeader>

      {!ready ? (
        <div className="grid place-items-center py-28 text-muted-foreground">
          <Spinner />
        </div>
      ) : (
        <div className="mt-5 grid grid-cols-1 gap-4">
          {/* The month at a glance */}
          <section className="card p-6 sm:p-7">
            <div className="grid gap-8 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,1fr)]">
            <div className="min-w-0">
              <div className="text-[14px] text-ink-2">{isThisMonth ? t("This month's spending") : t("Spending in {month}", { month: monthLabel(month) })}</div>
              <Money value={stats.total} currency={currency} className="mt-1 block text-[34px] leading-tight font-semibold tracking-[-0.01em]" />
              <div className="mt-1.5 flex min-h-[22px] items-center gap-2 text-[13.5px] text-ink-2">
                {stats.prev_to_date ? (
                  <>
                    <Delta now={stats.to_date} before={stats.prev_to_date} currency={currency} /> {t("vs. {when}", { when: compareLabel(stats) })}
                  </>
                ) : (
                  <>{t("Nothing recorded in {when}", { when: compareLabel(stats) })}</>
                )}
              </div>
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
                />
              </div>
              <div className="mt-6">
                <div className="mb-2 flex items-baseline justify-between gap-3">
                  <span className="label">{t("Daily spending")}</span>
                  <span className="text-[12px] text-muted-foreground">{isThisMonth ? t("Last 30 days") : monthLabel(month)}</span>
                </div>
                <ColumnChart
                  label={isThisMonth ? t("Spending on each of the last 30 days") : t("Spending on each day of {month}", { month: monthLabel(month) })}
                  height={160}
                  marks={recent.flatMap((d, i) => (i > 0 && d.date.endsWith("-01") ? [{ index: i, label: monthName(d.date.slice(0, 7)) }] : []))}
                  data={recent.map((d) => ({
                    key: d.date,
                    tick: String(Number(d.date.slice(8))),
                    label: dateLabel(d.date, { weekday: "short", month: "short", day: "numeric" }),
                    value: d.total,
                    detail: tn(d.count, "{n} entry", "{n} entries"),
                  }))}
                  format={fmt}
                  formatTick={fmtTick}
                />
              </div>
            </div>
            <div className="min-w-0 lg:border-l lg:border-rule lg:pl-8">
              <div className="mb-3 flex items-baseline justify-between">
                <h2 className="card-title">{t("Day by day")}</h2>
                <span className="text-[12px] text-ink-2">{t("The greener, the more spent")}</span>
              </div>
              <CalendarHeat days={stats.days} format={fmt} />
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
                  href={(type) => `#/ledger/${month}/${encodeURIComponent(type)}`}
                />
              </div>
              <div className="min-w-0 lg:border-l lg:border-rule lg:pl-10">
                <h2 className="card-title mb-4">{t("By card")}</h2>
                <Donut data={stats.by_card} format={fmt} label={t("Spending by card")} noun={(n) => tn(n, "{n} card", "{n} cards")} />
              </div>
            </div>
          </section>

          {/* Month by month */}
          <section className="card p-6 sm:p-7">
            <div className="flex flex-wrap items-baseline justify-between gap-3">
              <h2 className="card-title">{t("Monthly spending")}</h2>
              <span className="text-[12.5px] text-ink-2">{t("The last twelve months · click a month to open it")}</span>
            </div>
            <div className="mt-4">
              <ColumnChart
                label={t("Spending in each of the last twelve months")}
                height={220}
                data={stats.monthly.map((m) => ({
                  key: m.month,
                  tick: monthLabel(m.month, "short"),
                  label: monthLabel(m.month),
                  value: m.total,
                  detail: tn(m.count, "{n} entry", "{n} entries"),
                  soft: m.month !== month,
                }))}
                format={fmt}
                formatTick={fmtTick}
                onSelect={setMonth}
              />
            </div>
          </section>
        </div>
      )}
    </div>
  )
}
