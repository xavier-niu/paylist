import { money, type Entry } from "@/lib/api"
import { t } from "@/lib/i18n"
import { cn } from "@/lib/utils"

/** An amount with its decimals set small, the way a big figure reads best: ¥8,070.30 */
export function Money({ value, currency, className }: { value: number; currency: string; className?: string }) {
  const text = money(value, currency)
  const m = text.match(/^(.*?)([.,]\d{2})(\D*)$/)
  if (!m) return <span className={cn("tabular-nums", className)}>{text}</span>
  return (
    <span className={cn("tabular-nums", className)}>
      {m[1]}
      <span className="text-[0.5em] font-medium">{m[2]}</span>
      {m[3]}
    </span>
  )
}

/** The change against another figure, as a tinted pill: ↑ ¥10.98 (12.5%) (more spending is red). */
export function Delta({ now, before, currency, title }: { now: number; before: number; currency: string; title?: string }) {
  if (!before) return null
  const diff = now - before
  if (Math.abs(diff) < 0.005) return <span className="delta bg-wash text-ink-2" title={title}>±0</span>
  const up = diff > 0
  const pct = Math.abs(diff / before) * 100
  return (
    <span className={cn("delta", up ? "up" : "down")} title={title}>
      {up ? "↑" : "↓"} {money(Math.abs(diff), currency)} ({pct.toFixed(1)}%)
    </span>
  )
}

/** What an entry paid in another currency was, small and muted beside its amount: US$12.50. */
export function Original({ entry, className }: { entry: Pick<Entry, "orig_amount" | "orig_currency">; className?: string }) {
  if (!entry.orig_currency || entry.orig_amount == null) return null
  return (
    <span className={cn("shrink-0 text-[12px] font-normal text-muted-foreground tabular-nums", className)} title={t("Paid in {currency}", { currency: entry.orig_currency })}>
      {money(entry.orig_amount, entry.orig_currency)}
    </span>
  )
}
