import { useMemo, type ReactNode } from "react"

import { ColumnChart, Donut, Legend, Sparkline } from "@/components/charts"
import { money, monthLabel, thisMonth, type Stats, type YearStats } from "@/lib/api"
import { lang, t, tn } from "@/lib/i18n"
import { Delta, Money } from "@/lib/money"
import { cn } from "@/lib/utils"

// The overview's trends: each type against its usual month, the months one
// after another, and the year against the one before.

/** A figure under its name, in a row of them. */
function Figure({ label, children, title }: { label: string; children: ReactNode; title?: string }) {
  return (
    <div className="min-w-0" title={title}>
      <div className="text-[12.5px] text-ink-2">{label}</div>
      <div className="mt-0.5 truncate text-[17px] font-semibold tabular-nums">{children}</div>
    </div>
  )
}

/** The change against a figure, as a share: ↑ 45% (more is red), or "New" with nothing before. */
function Change({ now, before }: { now: number; before: number }) {
  if (!before) return now > 0 ? <span className="delta up">{t("New")}</span> : <span className="delta bg-wash text-ink-2">±0</span>
  const diff = now - before
  if (Math.abs(diff) < 0.005) return <span className="delta bg-wash text-ink-2">±0</span>
  const pct = Math.abs(diff / before) * 100
  return (
    <span className={cn("delta", diff > 0 ? "up" : "down")}>
      {diff > 0 ? "↑" : "↓"} {pct >= 100 ? pct.toFixed(0) : pct.toFixed(1)}%
    </span>
  )
}

type TypeRow = { name: string; color: string; now: number; usual: number; diff: number; spark: number[] }

/**
 * Each type this month against its usual month (the average of the months
 * before, counted the same way), the furthest above it first: where the
 * spending grew, so where to look to spend less next month.
 */
export function UsualCard({ stats, currency, onPickType }: { stats: Stats; currency: string; onPickType: (name: string, color: string) => void }) {
  const rows = useMemo<TypeRow[]>(() => {
    const out = new Map<string, TypeRow>()
    const spark = new Map(stats.type_monthly.map((s) => [s.name, s.totals]))
    for (const g of stats.by_type_to_date) out.set(g.name, { name: g.name, color: g.color, now: g.total, usual: 0, diff: 0, spark: [] })
    for (const g of stats.type_avg) {
      const r = out.get(g.name) ?? { name: g.name, color: g.color, now: 0, usual: 0, diff: 0, spark: [] }
      r.usual = g.total
      out.set(g.name, r)
    }
    return [...out.values()]
      .map((r) => ({ ...r, diff: Math.round((r.now - r.usual) * 100) / 100, spark: spark.get(r.name) ?? [] }))
      .filter((r) => r.now !== 0 || r.usual !== 0)
      .sort((a, b) => b.diff - a.diff || b.now - a.now)
  }, [stats])
  if (stats.avg_months === 0 || rows.length === 0) return null
  // Worth a look: well above the usual, by share and by amount.
  const floor = Math.max(1, stats.avg_to_date * 0.01)
  const above = rows.filter((r) => r.diff >= floor && (r.usual === 0 || r.diff / r.usual >= 0.1)).slice(0, 3)
  const usualTitle = stats.compare_day
    ? tn(stats.avg_months, "The month before, days 1–{d}", "The average of the {n} months before, days 1–{d} of each", { d: stats.compare_day })
    : tn(stats.avg_months, "The month before", "The average of the {n} months before")
  const name = (r: TypeRow) => r.name || t("No type")

  return (
    <section className="card p-6 sm:p-7">
      <h2 className="card-title">{t("Against the usual")}</h2>
      {above.length > 0 && (
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <span className="mr-1 text-[13px] text-ink-2">{t("Above usual")}</span>
          {above.map((r) => (
            <button key={r.name} type="button" onClick={() => onPickType(r.name, r.color)} className="flex h-8 items-center gap-2 rounded-full bg-red-wash px-3 text-[13px] hover:brightness-95" data-color={r.name ? r.color : "gray"}>
              <span className="dot" />
              <span className="font-medium">{name(r)}</span>
              <span className="text-red tabular-nums">
                +{money(r.diff, currency)}
                {r.usual > 0 && ` (+${((r.diff / r.usual) * 100).toFixed(0)}%)`}
              </span>
            </button>
          ))}
        </div>
      )}
      <div className="mt-4 overflow-x-auto">
        <table className="w-full min-w-0 text-[14px]">
          <thead>
            <tr className="border-b border-rule text-left text-[12.5px] text-ink-2">
              <th className="py-2 pr-3 font-normal">{t("Type")}</th>
              <th className="py-2 pr-3 text-right font-normal">{stats.compare_day ? t("This month so far") : t("This month")}</th>
              <th className="py-2 pr-3 text-right font-normal max-sm:hidden" title={usualTitle}>
                <span className="cursor-help underline decoration-dotted underline-offset-2">{t("Usual")}</span>
              </th>
              <th className="py-2 pr-3 text-right font-normal">{t("Change")}</th>
              <th className="py-2 font-normal max-md:hidden">{t("12 months")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.name} className="group cursor-pointer border-b border-rule last:border-b-0 hover:bg-wash" onClick={() => onPickType(r.name, r.color)} data-color={r.name ? r.color : "gray"}>
                <td className="py-2 pr-3">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="dot" />
                    <span className={cn("truncate", !r.name && "text-ink-2")}>{name(r)}</span>
                  </span>
                </td>
                <td className="py-2 pr-3 text-right tabular-nums">{money(r.now, currency)}</td>
                <td className="py-2 pr-3 text-right text-ink-2 tabular-nums max-sm:hidden">{money(r.usual, currency)}</td>
                <td className="py-2 pr-3 text-right">
                  <span className="inline-flex items-center gap-2">
                    <span className={cn("text-[12.5px] tabular-nums max-sm:hidden", r.diff > 0 ? "text-red" : r.diff < 0 ? "text-good" : "text-ink-2")}>
                      {r.diff > 0 ? "+" : r.diff < 0 ? "−" : ""}
                      {money(Math.abs(r.diff), currency)}
                    </span>
                    <Change now={r.now} before={r.usual} />
                  </span>
                </td>
                <td className="py-2 max-md:hidden">
                  <Sparkline values={r.spark} label={t("{name} over the last twelve months", { name: name(r) })} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

/** The months one after another: the average, highest and lowest, and each month's column against the average. */
export function MonthlyCard({ stats, month, currency, onSelect }: { stats: Stats; month: string; currency: string; onSelect: (month: string) => void }) {
  const fmt = (v: number) => money(v, currency)
  const fmtTick = (v: number, compact: boolean) => (compact ? money(v, currency, true) : money(v, currency).replace(/[.,]00(?=\D*$)/, ""))
  // Whole months only (the current one is still going), from the first with spending.
  const done = useMemo(() => {
    const ms = stats.monthly.filter((m) => m.month < thisMonth())
    const first = ms.findIndex((m) => m.total !== 0)
    return first < 0 ? [] : ms.slice(first)
  }, [stats])
  const avg = done.length ? done.reduce((s, m) => s + m.total, 0) / done.length : 0
  const high = done.reduce<(typeof done)[number] | null>((a, m) => (!a || m.total > a.total ? m : a), null)
  const low = done.reduce<(typeof done)[number] | null>((a, m) => (!a || m.total < a.total ? m : a), null)
  const change = (i: number) => {
    const before = stats.monthly[i - 1]?.total
    const now = stats.monthly[i].total
    if (!before) return ""
    const pct = ((now - before) / Math.abs(before)) * 100
    return t("{pct} on {month}", { pct: `${pct >= 0 ? "+" : "−"}${Math.abs(pct).toFixed(1)}%`, month: monthLabel(stats.monthly[i - 1].month, "short") })
  }

  return (
    <section className="card p-6 sm:p-7">
      <h2 className="card-title">{t("Monthly spending")}</h2>
      {done.length > 1 && (
        <div className="mt-4 grid grid-cols-3 gap-4 sm:max-w-[560px]">
          <Figure label={t("Average a month")} title={tn(done.length, "Over {n} whole month", "Over {n} whole months")}>{fmt(avg)}</Figure>
          {high && <Figure label={t("Highest · {month}", { month: monthLabel(high.month, "short") })}>{fmt(high.total)}</Figure>}
          {low && <Figure label={t("Lowest · {month}", { month: monthLabel(low.month, "short") })}>{fmt(low.total)}</Figure>}
        </div>
      )}
      <div className="mt-5">
        {done.length > 1 && <Legend items={[{ label: t("Average"), swatch: "average" }]} />}
        <ColumnChart
          label={t("Spending in each of the last twelve months")}
          height={220}
          data={stats.monthly.map((m, i) => ({
            key: m.month,
            tick: monthLabel(m.month, "short"),
            label: monthLabel(m.month),
            value: m.total,
            detail: [tn(m.count, "{n} entry", "{n} entries"), change(i)].filter(Boolean).join(" · "),
            soft: m.month !== month,
          }))}
          format={fmt}
          formatTick={fmtTick}
          onSelect={onSelect}
          average={done.length > 1 ? { value: avg, label: t("Average") } : undefined}
        />
      </div>
    </section>
  )
}

/** "Jan 1–Nov 25, 2025": the part of last year a year so far is compared with. */
function yearCompareLabel(y: YearStats): string {
  const prev = y.year - 1
  if (!y.compare_date) return String(prev)
  const d = new Date(y.compare_date + "T00:00")
  if (lang() === "zh") return `${prev}年1月1日–${d.getMonth() + 1}月${d.getDate()}日`
  return `Jan 1–${d.toLocaleDateString("en-US", { month: "short", day: "numeric" })}, ${prev}`
}

/** The year: its spending against last year's, its months beside last year's, and where it went. */
export function YearCard({ year: y, currency, onSelect }: { year: YearStats; currency: string; onSelect: (month: string) => void }) {
  const fmt = (v: number) => money(v, currency)
  const fmtTick = (v: number, compact: boolean) => (compact ? money(v, currency, true) : money(v, currency).replace(/[.,]00(?=\D*$)/, ""))
  const current = y.year === new Date().getFullYear()
  // Months so far this year (all twelve for a past one), from the first with spending.
  const elapsed = useMemo(() => {
    const upTo = current ? new Date().getMonth() + 1 : 12
    const first = y.months.findIndex((m) => m.total !== 0)
    return first < 0 ? 0 : Math.max(1, upTo - first)
  }, [y, current])
  const biggest = y.months.reduce<(typeof y.months)[number] | null>((a, m) => (m.total > 0 && (!a || m.total > a.total) ? m : a), null)
  const now = thisMonth()

  return (
    <section className="card p-6 sm:p-7">
      <div className="grid gap-8 xl:grid-cols-2">
        <div className="min-w-0">
          <div className="text-[14px] text-ink-2">{current ? t("Spending this year") : t("Spending in {year}", { year: String(y.year) })}</div>
          <Money value={y.total} currency={currency} className="mt-1 block text-[30px] leading-tight font-semibold tracking-[-0.01em]" />
          <div className="mt-1.5 flex min-h-[22px] items-center gap-2 text-[13.5px] text-ink-2">
            {y.prev_to_date ? (
              <>
                {t("Compared with last year")}
                <Delta now={y.to_date} before={y.prev_to_date} currency={currency} title={t("vs. {when}", { when: yearCompareLabel(y) })} />
              </>
            ) : (
              <>{t("Nothing recorded in {when}", { when: yearCompareLabel(y) })}</>
            )}
          </div>
          <div className="mt-5 grid grid-cols-2 gap-4 sm:max-w-[380px]">
            <Figure label={t("Average a month")}>{elapsed ? fmt(y.total / elapsed) : "—"}</Figure>
            <Figure label={biggest ? t("Biggest · {month}", { month: monthLabel(biggest.month, "short") }) : t("Biggest month")}>{biggest ? fmt(biggest.total) : "—"}</Figure>
          </div>
          <div className="mt-6">
            <Legend items={[{ label: String(y.year), swatch: "series" }, { label: String(y.year - 1), swatch: "context" }]} />
            <ColumnChart
              label={t("{year} month by month, against {prev}", { year: String(y.year), prev: String(y.year - 1) })}
              height={200}
              series={[String(y.year), String(y.year - 1)]}
              data={y.months.map((m, i) => ({
                key: m.month,
                tick: monthLabel(m.month, "short"),
                label: lang() === "zh" ? monthLabel(m.month, "short") : new Date(m.month + "-01T00:00").toLocaleDateString("en-US", { month: "long" }),
                value: m.total,
                prev: y.prev_months[i]?.total ?? 0,
              }))}
              format={fmt}
              formatTick={fmtTick}
              onSelect={(m) => m <= now && onSelect(m)}
            />
          </div>
        </div>
        <div className="min-w-0 max-xl:border-t max-xl:border-rule max-xl:pt-6 xl:border-l xl:border-rule xl:pl-8">
          <h2 className="card-title mb-4">{t("By type")}</h2>
          <Donut data={y.by_type} format={fmt} label={t("Spending by type in {year}", { year: String(y.year) })} noun={(n) => tn(n, "{n} type", "{n} types")} />
        </div>
      </div>
    </section>
  )
}
