import { useId, useLayoutEffect, useRef, useState, type ReactNode } from "react"

import { cn } from "@/lib/utils"
import { dateLabel } from "@/lib/api"
import { lang, locale, t, tn } from "@/lib/i18n"

/** The width of an element, kept up to date. The element should be [contain:inline-size], so what it draws at that width never holds it open when its space shrinks. */
export function useWidth<T extends HTMLElement>(): [React.RefObject<T | null>, number] {
  const ref = useRef<T>(null)
  const [w, setW] = useState(0)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const ro = new ResizeObserver(([e]) => setW(Math.floor(e.contentRect.width)))
    ro.observe(el)
    setW(Math.floor(el.getBoundingClientRect().width))
    return () => ro.disconnect()
  }, [])
  return [ref, w]
}

let measurer: CanvasRenderingContext2D | null = null
/** How wide axis text renders (.viz text: 11.5px in the page's font), for the room a y-axis needs. */
function axisTextWidth(labels: string[]): number {
  measurer ??= document.createElement("canvas").getContext("2d")
  if (!measurer) return Math.max(0, ...labels.map((l) => l.length * 7.5))
  measurer.font = `11.5px ${getComputedStyle(document.body).fontFamily}`
  return Math.ceil(Math.max(0, ...labels.map((l) => measurer!.measureText(l).width)))
}

/** Round tick values covering [min, max], about `count` of them. */
function niceTicks(min: number, max: number, count = 4): number[] {
  if (max <= min) max = min + 1
  const raw = (max - min) / count
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= raw) ?? 10 * mag
  const lo = Math.floor(min / step) * step
  const out: number[] = []
  for (let v = lo; v <= max + step * 0.999; v += step) out.push(Math.round(v * 1e6) / 1e6)
  return out
}

/** A column: rounded 4px at the data end, square at the baseline. */
function columnPath(x: number, w: number, y0: number, y1: number): string {
  const h = Math.abs(y1 - y0)
  if (h < 0.5) return ""
  const r = Math.min(4, w / 2, h)
  if (y1 < y0) return `M${x},${y0}V${y1 + r}Q${x},${y1} ${x + r},${y1}H${x + w - r}Q${x + w},${y1} ${x + w},${y1 + r}V${y0}Z`
  return `M${x},${y0}V${y1 - r}Q${x},${y1} ${x + r},${y1}H${x + w - r}Q${x + w},${y1} ${x + w},${y1 - r}V${y0}Z`
}

function Tooltip({ x, y, width, children }: { x: number; y: number; width: number; children: ReactNode }) {
  return (
    <div
      className="pointer-events-none absolute z-10 rounded-md border border-rule bg-sheet px-2.5 py-1.5 text-xs whitespace-nowrap shadow-[0_3px_6px_rgb(15_15_15/.1),0_9px_24px_rgb(15_15_15/.15)]"
      style={{ left: Math.min(Math.max(x, 80), width - 80), top: Math.max(0, y - 10), transform: "translate(-50%, -100%)" }}
    >
      {children}
    </div>
  )
}

export interface Column {
  key: string
  tick: string // under the axis; "" leaves it unlabelled
  label: string // in the tooltip
  value: number
  detail?: string
  soft?: boolean // drawn in the de-emphasised step, so the current one stands out
}

/** A single-series column chart with a hover tooltip (no legend: the card's title names it). */
export function ColumnChart({
  data,
  height = 220,
  format,
  formatTick,
  onSelect,
  label,
}: {
  data: Column[]
  height?: number
  format: (v: number) => string
  formatTick: (v: number, compact: boolean) => string
  onSelect?: (key: string) => void
  label: string
}) {
  const [ref, width] = useWidth<HTMLDivElement>()
  const [hover, setHover] = useState<number | null>(null)
  const values = data.map((d) => d.value)
  const ticks = niceTicks(Math.min(0, ...values), Math.max(0, ...values))
  const lo = ticks[0]
  const hi = ticks[ticks.length - 1]
  const compact = Math.max(...ticks.map(Math.abs)) >= 10000
  const tickLabels = ticks.map((t) => formatTick(t, compact))
  const pad = { top: 10, right: 4, bottom: 24, left: axisTextWidth(tickLabels) + 12 }
  const plotW = Math.max(0, width - pad.left - pad.right)
  const plotH = height - pad.top - pad.bottom
  const band = data.length ? plotW / data.length : 0
  const barW = Math.max(2, Math.min(24, band - 2))
  // Leave labels out where they'd collide, always keeping the last (current) one.
  const longest = Math.max(1, ...data.map((d) => d.tick.length))
  const tickEvery = Math.max(1, Math.ceil((longest * 6.5 + 8) / (band || 1)))
  const y = (v: number) => pad.top + plotH - ((v - lo) / (hi - lo || 1)) * plotH
  const y0 = y(0)
  const h = hover === null ? null : data[hover]

  return (
    <div ref={ref} className="relative select-none [contain:inline-size]" onMouseLeave={() => setHover(null)}>
      {width > 0 && (
        <svg className="viz block" width={width} height={height} role="img" aria-label={label}>
          {ticks.map((t, i) => (
            <g key={t}>
              <line className={t === 0 ? "baseline" : "grid"} x1={pad.left} x2={width - pad.right} y1={Math.round(y(t)) + 0.5} y2={Math.round(y(t)) + 0.5} />
              <text x={pad.left - 8} y={y(t)} dy="0.32em" textAnchor="end">{tickLabels[i]}</text>
            </g>
          ))}
          {hover !== null && <rect className="hover-band" x={pad.left + band * hover} y={pad.top} width={band} height={plotH} />}
          {data.map((d, i) => (
            <path key={d.key} className={cn("bar", d.soft && hover !== i && "soft")} d={columnPath(pad.left + band * i + (band - barW) / 2, barW, y0, y(d.value))} />
          ))}
          {data.map((d, i) =>
            d.tick && (data.length - 1 - i) % tickEvery === 0 ? (
              <text key={d.key} x={pad.left + band * i + band / 2} y={height - 6} textAnchor="middle">{d.tick}</text>
            ) : null,
          )}
          {data.map((d, i) => (
            <rect key={d.key} className="hit" x={pad.left + band * i} y={pad.top} width={band} height={plotH}
              onMouseEnter={() => setHover(i)} onClick={() => onSelect?.(d.key)} style={{ cursor: onSelect ? "pointer" : undefined }} />
          ))}
        </svg>
      )}
      {h && (
        <Tooltip x={pad.left + band * hover! + band / 2} y={y(Math.max(h.value, 0))} width={width}>
          <div className="figure text-[13px] font-semibold text-foreground">{format(h.value)}</div>
          <div className="text-muted-foreground">{h.label}{h.detail ? ` · ${h.detail}` : ""}</div>
        </Tooltip>
      )}
    </div>
  )
}

/**
 * Cumulative spending through the month: this month (the series) against the
 * month before (context gray), with a crosshair that reads both.
 */
export function PaceChart({
  current,
  previous,
  daysInMonth,
  todayIndex,
  format,
  formatTick,
  labels,
  compact,
  height: fixedHeight,
}: {
  current: number[] // cumulative totals for each day of the month
  previous: number[] // cumulative totals for every day of the month before
  daysInMonth: number
  todayIndex: number // last day of `current` to draw (0-based)
  format: (v: number) => string
  formatTick: (v: number, compact: boolean) => string
  labels: [string, string] // series names
  /** No axes, just the first and last day: for a figure's own card. */
  compact?: { start: string; end: string }
  /** The chart's height; by default 150 compact, 250 otherwise. */
  height?: number
}) {
  const [ref, width] = useWidth<HTMLDivElement>()
  const [hover, setHover] = useState<number | null>(null)
  const id = useId()
  const height = fixedHeight ?? (compact ? 150 : 250)
  const days = Math.max(daysInMonth, previous.length)
  const cur = current.slice(0, todayIndex + 1)
  const ticks = niceTicks(0, Math.max(1, ...cur, ...previous))
  const hi = ticks[ticks.length - 1]
  const compactTicks = hi >= 10000
  const tickLabels = ticks.map((t) => formatTick(t, compactTicks))
  const pad = compact ? { top: 22, right: 6, bottom: 22, left: 2 } : { top: 22, right: 14, bottom: 24, left: axisTextWidth(tickLabels) + 12 }
  const plotW = Math.max(0, width - pad.left - pad.right)
  const plotH = height - pad.top - pad.bottom
  const x = (i: number) => pad.left + (days > 1 ? (i / (days - 1)) * plotW : 0)
  const y = (v: number) => pad.top + plotH - (v / (hi || 1)) * plotH
  const path = (vs: number[]) => vs.map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join("")
  const area = cur.length > 1 ? `${path(cur)}L${x(cur.length - 1)},${y(0)}L${x(0)},${y(0)}Z` : ""
  const dayTicks = [0, 6, 13, 20, 27].filter((d) => d < days)
  const endX = cur.length ? x(cur.length - 1) : 0

  const onMove = (e: React.MouseEvent<SVGRectElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const i = Math.round(((e.clientX - rect.left) / rect.width) * (days - 1))
    setHover(Math.max(0, Math.min(days - 1, i)))
  }

  return (
    <div>
      <div className="mb-2 flex flex-wrap items-center gap-4 text-[12px] text-ink-2">
        <span className="flex items-center gap-1.5"><span className="h-0.5 w-4 rounded bg-[var(--viz-series)]" /> {labels[0]}</span>
        <span className="flex items-center gap-1.5"><span className={compact ? "w-4 border-t-[1.5px] border-dashed border-[var(--viz-context)]" : "h-0.5 w-4 rounded bg-[var(--viz-context)]"} /> {labels[1]}</span>
      </div>
      <div ref={ref} className="relative select-none [contain:inline-size]" onMouseLeave={() => setHover(null)}>
        {width > 0 && (
          <svg className="viz block" width={width} height={height} role="img" aria-label={t("Cumulative spending: {a} against {b}", { a: labels[0], b: labels[1] })}>
            <defs>
              <linearGradient id={id} x1="0" x2="0" y1="0" y2="1">
                <stop offset="0" stopColor="var(--viz-series)" stopOpacity=".14" />
                <stop offset="1" stopColor="var(--viz-series)" stopOpacity="0" />
              </linearGradient>
            </defs>
            {!compact &&
              ticks.map((t, i) => (
                <g key={t}>
                  <line className={t === 0 ? "baseline" : "grid"} x1={pad.left} x2={width - pad.right} y1={Math.round(y(t)) + 0.5} y2={Math.round(y(t)) + 0.5} />
                  <text x={pad.left - 8} y={y(t)} dy="0.32em" textAnchor="end">{tickLabels[i]}</text>
                </g>
              ))}
            {compact ? (
              <>
                <line className="baseline" x1={pad.left} x2={width - pad.right} y1={Math.round(y(0)) + 0.5} y2={Math.round(y(0)) + 0.5} />
                <text x={pad.left} y={height - 4} textAnchor="start">{compact.start}</text>
                <text x={width - pad.right} y={height - 4} textAnchor="end">{compact.end}</text>
              </>
            ) : (
              dayTicks.map((d) => <text key={d} x={x(d)} y={height - 6} textAnchor="middle">{d + 1}</text>)
            )}
            {previous.length > 1 && <path className="line" d={path(previous)} stroke="var(--viz-context)" strokeDasharray={compact ? "3 4" : undefined} strokeWidth={compact ? 1.5 : undefined} />}
            {area && <path d={area} fill={`url(#${id})`} />}
            {cur.length > 1 && <path className="line" d={path(cur)} stroke="var(--viz-series)" />}
            {cur.length > 0 && (
              <>
                <circle cx={endX} cy={y(cur[cur.length - 1])} r={4.5} fill="var(--viz-series)" stroke="var(--sheet)" strokeWidth={2} />
                <text x={endX > width - 110 ? endX - 8 : endX + 8} y={y(cur[cur.length - 1]) - 10} textAnchor={endX > width - 110 ? "end" : "start"} style={{ fill: "var(--ink)", fontWeight: 600, fontSize: 11.5 }}>
                  {format(cur[cur.length - 1])}
                </text>
              </>
            )}
            {hover !== null && (
              <>
                <line className="crosshair" x1={x(hover)} x2={x(hover)} y1={pad.top} y2={pad.top + plotH} />
                {hover < cur.length && <circle cx={x(hover)} cy={y(cur[hover])} r={4} fill="var(--viz-series)" stroke="var(--sheet)" strokeWidth={2} />}
                {hover < previous.length && <circle cx={x(hover)} cy={y(previous[hover])} r={4} fill="var(--viz-context)" stroke="var(--sheet)" strokeWidth={2} />}
              </>
            )}
            <rect className="hit" x={pad.left} y={pad.top} width={plotW} height={plotH} onMouseMove={onMove} />
          </svg>
        )}
        {hover !== null && (
          <Tooltip x={x(hover)} y={pad.top + 4} width={width}>
            <div className="mb-1 text-muted-foreground">{t("Day {n}", { n: hover + 1 })}</div>
            {hover < cur.length && (
              <div className="flex items-center gap-2"><span className="h-0.5 w-3 rounded bg-[var(--viz-series)]" /><span className="figure font-semibold text-foreground">{format(cur[hover])}</span><span className="text-muted-foreground">{labels[0]}</span></div>
            )}
            {hover < previous.length && (
              <div className="flex items-center gap-2"><span className="h-0.5 w-3 rounded bg-[var(--viz-context)]" /><span className="figure font-semibold text-foreground">{format(previous[hover])}</span><span className="text-muted-foreground">{labels[1]}</span></div>
            )}
          </Tooltip>
        )}
      </div>
    </div>
  )
}

/** A day's amount in a calendar cell: 36, 208, 1.2K (1200 in Chinese, where 1.2千 reads oddly). */
function compactAmount(n: number) {
  if (Math.abs(n) < 1000 || (lang() === "zh" && Math.abs(n) < 10000)) return String(Math.round(n))
  return new Intl.NumberFormat(locale(), { notation: "compact", maximumFractionDigits: 1 }).format(n)
}

/** Monday to Sunday, short: Mo Tu … or 一 二 … */
function weekdays(): string[] {
  // 2024-01-01 was a Monday.
  return Array.from({ length: 7 }, (_, i) => {
    const d = new Date(2024, 0, 1 + i)
    return lang() === "zh" ? d.toLocaleDateString("zh-CN", { weekday: "narrow" }) : d.toLocaleDateString("en-US", { weekday: "short" }).slice(0, 2)
  })
}

/** Daily spending as a month calendar: one hue, darker is more, each day's amount under its date. */
export function CalendarHeat({
  days,
  format,
  onPick,
  picked = null,
}: {
  days: { date: string; total: number; count: number }[]
  format: (v: number) => string
  onPick?: (date: string) => void
  /** The selected day, outlined. */
  picked?: string | null
}) {
  const [hover, setHover] = useState<string | null>(null)
  if (days.length === 0) return null
  const first = new Date(days[0].date + "T00:00")
  const lead = (first.getDay() + 6) % 7 // Monday first
  const positive = days.map((d) => d.total).filter((v) => v > 0).sort((a, b) => a - b)
  // Steps by quantile of the days with spending, so one big day (rent) doesn't wash out the rest.
  const cuts = [0.2, 0.4, 0.6, 0.8].map((q) => positive[Math.floor(q * (positive.length - 1))] ?? 0)
  const step = (v: number) => (v <= 0 ? 0 : 1 + cuts.filter((c) => v > c).length)
  const todayStr = new Date().toDateString()
  const h = days.find((d) => d.date === hover)
  return (
    <div>
      <div className="grid grid-cols-7 gap-[3px] text-center text-[11px] text-muted-foreground">
        {weekdays().map((d) => <div key={d} className="label pb-1 !text-[11px] !font-normal">{d}</div>)}
        {Array.from({ length: lead }, (_, i) => <div key={"l" + i} />)}
        {days.map((d) => {
          const s = step(d.total)
          const isToday = new Date(d.date + "T00:00").toDateString() === todayStr
          return (
            <button
              key={d.date}
              type="button"
              onMouseEnter={() => setHover(d.date)}
              onMouseLeave={() => setHover(null)}
              onFocus={() => setHover(d.date)}
              onBlur={() => setHover(null)}
              onClick={() => onPick?.(d.date)}
              aria-label={`${dateLabel(d.date)}: ${format(d.total)}, ${tn(d.count, "{n} entry", "{n} entries")}`}
              className={cn("figure flex aspect-square flex-col items-start justify-between rounded-[5px] p-1 text-left outline-offset-1 hover:outline-2 hover:outline-ink sm:px-1.5", isToday && "outline-2 outline-blue", d.date === picked && "outline-2 outline-ink", onPick && "cursor-pointer")}
              aria-pressed={onPick ? d.date === picked : undefined}
              style={{ background: `var(--seq-${s})`, color: s >= 4 ? "var(--seq-text)" : s > 0 ? "var(--ink)" : "var(--muted-foreground)" }}
            >
              <span className="text-[10.5px] leading-none opacity-70">{Number(d.date.slice(8))}</span>
              {d.total !== 0 && <span className="w-full truncate text-right text-[11px] leading-none font-semibold sm:text-[12px]">{compactAmount(d.total)}</span>}
            </button>
          )
        })}
      </div>
      <div className="mt-3 flex items-center justify-between gap-2 text-[12px] text-muted-foreground">
        <span className="min-h-[18px] truncate">
          {h ? (
            <>
              <span className="figure font-semibold text-foreground">{format(h.total)}</span> · {dateLabel(h.date, { weekday: "short", month: "short", day: "numeric" })} · {tn(h.count, "{n} entry", "{n} entries")}
            </>
          ) : (
            t("Hover a day for its total")
          )}
        </span>
        <span className="flex shrink-0 items-center gap-1">
          {t("Less")}
          {[0, 1, 2, 3, 4, 5].map((s) => <span key={s} className="size-2.5 rounded-[2px]" style={{ background: `var(--seq-${s})` }} />)}
          {t("More")}
        </span>
      </div>
    </div>
  )
}

type Part = { name: string; color: string; total: number; count: number; other?: boolean; members?: string[] }

/** A group picked in a donut: its own name, or for "Other" the names folded into it. */
export interface DonutPick {
  key: string
  label: string
  names: string[]
  color: string
  total: number
  count: number
}

const OTHER_KEY = "\u0000other"

/** Slices past the sixth fold into "Other", so each one stays big enough to read. */
function foldParts(data: Part[], max = 7): Part[] {
  const pos = data.filter((d) => d.total > 0)
  if (pos.length <= max) return pos
  const rest = pos.slice(max - 1)
  return [
    ...pos.slice(0, max - 1),
    {
      name: t("Other"),
      color: "gray",
      total: rest.reduce((s, d) => s + d.total, 0),
      count: rest.reduce((s, d) => s + d.count, 0),
      other: true,
      members: rest.map((d) => d.name),
    },
  ]
}

/** One donut slice from angle a0 to a1 (radians, clockwise from 12 o'clock). */
function arcPath(cx: number, cy: number, r0: number, r1: number, a0: number, a1: number): string {
  if (a1 - a0 >= Math.PI * 2 - 1e-6) {
    // A whole ring: two half circles, outer then inner.
    return `M${cx},${cy - r1}A${r1},${r1} 0 1 1 ${cx},${cy + r1}A${r1},${r1} 0 1 1 ${cx},${cy - r1}Z` +
      `M${cx},${cy - r0}A${r0},${r0} 0 1 0 ${cx},${cy + r0}A${r0},${r0} 0 1 0 ${cx},${cy - r0}Z`
  }
  const p = (r: number, a: number) => `${(cx + r * Math.sin(a)).toFixed(2)},${(cy - r * Math.cos(a)).toFixed(2)}`
  const large = a1 - a0 > Math.PI ? 1 : 0
  return `M${p(r1, a0)}A${r1},${r1} 0 ${large} 1 ${p(r1, a1)}L${p(r0, a1)}A${r0},${r0} 0 ${large} 0 ${p(r0, a0)}Z`
}

/**
 * Part of a whole as a donut with its legend: each slice in its group's own
 * color, a gap between slices, the total in the middle. Hovering a slice or a
 * legend row picks that group out and shows it in the middle; clicking one
 * (with onPick) selects it, and clicking it again lets it go.
 */
export function Donut({
  data,
  format,
  label,
  noun,
  picked = null,
  onPick,
}: {
  data: Part[]
  format: (v: number) => string
  /** What the donut shows, for screen readers: "Spending by type". */
  label: string
  /** The count under the total: (n) => "9 types". */
  noun: (n: number) => string
  /** The selected group's key (its name, "" for none), or null. */
  picked?: string | null
  onPick?: (p: DonutPick | null) => void
}) {
  const [hover, setHover] = useState<number | null>(null)
  const parts = foldParts(data)
  const keyOf = (d: Part) => (d.other ? OTHER_KEY : d.name)
  const pickedIndex = picked === null ? -1 : parts.findIndex((d) => keyOf(d) === picked)
  const focus = hover ?? (pickedIndex >= 0 ? pickedIndex : null)
  const toggle = (d: Part) => {
    if (!onPick) return
    if (keyOf(d) === picked) return onPick(null)
    onPick({ key: keyOf(d), label: d.name || t("None"), names: d.members ?? [d.name], color: d.name && !d.other ? d.color : "gray", total: d.total, count: d.count })
  }
  const sum = parts.reduce((s, d) => s + d.total, 0)
  if (parts.length === 0) return <p className="py-6 text-[13.5px] text-ink-2">{t("Nothing this month.")}</p>
  const size = 168, c = size / 2, r1 = size / 2 - 2, r0 = r1 - 26
  let a = 0
  const slices = parts.map((d) => {
    const a0 = a
    a += (d.total / sum) * Math.PI * 2
    return { d, a0, a1: a }
  })
  const h = focus === null ? null : parts[focus]
  const name = (d: Part) => d.name || t("None")
  const share = (v: number) => {
    const pct = (v / sum) * 100
    return `${pct.toFixed(pct < 10 ? 1 : 0)}%`
  }

  return (
    <div className="grid items-center gap-x-7 gap-y-4 sm:grid-cols-[168px_minmax(0,1fr)]">
      <div className="relative mx-auto size-[168px]" onMouseLeave={() => setHover(null)}>
        <svg viewBox={`0 0 ${size} ${size}`} className="block size-full" role="img" aria-label={label}>
          {slices.map(({ d, a0, a1 }, i) => (
            <path
              key={name(d)}
              d={arcPath(c, c, r0, r1, a0, a1)}
              data-color={d.name && !d.other ? d.color : "gray"}
              fill="var(--sw)"
              stroke="var(--sheet)"
              strokeWidth={slices.length > 1 ? 2 : 0}
              strokeLinejoin="round"
              opacity={focus === null || focus === i ? 1 : 0.35}
              onMouseEnter={() => setHover(i)}
              onClick={() => toggle(d)}
              className={cn("transition-opacity", onPick && "cursor-pointer")}
            />
          ))}
        </svg>
        <div className="pointer-events-none absolute inset-0 grid place-content-center px-9 text-center">
          {h ? (
            <>
              <span className="truncate text-[12px] text-ink-2">{name(h)}</span>
              <span className="text-[15px] font-semibold tabular-nums">{format(h.total)}</span>
              <span className="text-[12px] text-ink-2 tabular-nums">{share(h.total)}</span>
            </>
          ) : (
            <>
              <span className="text-[15px] font-semibold tabular-nums">{format(sum)}</span>
              <span className="text-[12px] text-ink-2">{noun(data.filter((d) => d.total > 0).length)}</span>
            </>
          )}
        </div>
      </div>
      <ul className="grid min-w-0 content-center" onMouseLeave={() => setHover(null)}>
        {parts.map((d, i) => {
          const body = (
            <>
              <span className="dot" />
              <span className={cn("min-w-0 flex-1 truncate", (!d.name || d.other) && "text-ink-2")}>{name(d)}</span>
              <span className="font-medium tabular-nums">{format(d.total)}</span>
              <span className="w-11 text-right text-[12.5px] text-ink-2 tabular-nums">{share(d.total)}</span>
            </>
          )
          const row = cn(
            "flex h-8 w-full items-center gap-2.5 rounded-[8px] px-2 text-left text-[13.5px]",
            i === pickedIndex ? "bg-fill" : hover === i && "bg-wash",
          )
          const title = `${name(d)}: ${format(d.total)}, ${tn(d.count, "{n} entry", "{n} entries")}`
          return (
            <li key={name(d)} data-color={d.name && !d.other ? d.color : "gray"} onMouseEnter={() => setHover(i)}>
              {onPick ? (
                <button type="button" title={title} className={row} aria-pressed={i === pickedIndex} onClick={() => toggle(d)}>
                  {body}
                </button>
              ) : (
                <div title={title} className={row}>{body}</div>
              )}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
