import { Tag } from "@/components/option-select"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Loading, Skeleton } from "@/components/ui/skeleton"
import { dateLabel, money, type Entry } from "@/lib/api"
import { t, tn } from "@/lib/i18n"
import { optionColor, useApp } from "@/lib/state"
import { cn } from "@/lib/utils"

/**
 * The entries behind a part of a chart (a type, a card, a day), in a window
 * with room for them: its name, how many and their total, then one row each,
 * with every column that differs; a row opens the entry to change it (unless
 * the ledger is only viewed).
 */
export function EntryList({
  title,
  color,
  entries,
  show,
  onClose,
}: {
  title: string
  /** The group's color, for its dot; none for a day. */
  color?: string
  /** null while loading. */
  entries: Entry[] | null
  /** Which of date, type and card each row shows (the picked one is left out, being the same for all). */
  show: { date?: boolean; type?: boolean; card?: boolean }
  onClose: () => void
}) {
  const { options, editEntry, ledger, canWrite } = useApp()
  const currency = ledger.currency
  const total = (entries ?? []).reduce((s, e) => s + Math.round(e.amount * 100), 0) / 100
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex max-h-[80vh] flex-col gap-0 overflow-hidden rounded-[20px] border-0 bg-sheet p-0 shadow-[0_16px_48px_rgb(0_0_0/.3),0_0_0_1px_var(--rule-2)] ring-0 sm:max-w-[620px]">
        <div className="flex shrink-0 items-baseline gap-3 border-b border-rule px-6 pt-5 pb-4 pr-14">
          <DialogTitle className="flex min-w-0 items-center gap-2 text-[17px] font-semibold">
            {color && <span className="dot" data-color={color} />}
            <span className="truncate">{title}</span>
          </DialogTitle>
          {entries && (
            <DialogDescription className="shrink-0 text-[13px] text-ink-2 tabular-nums">
              {tn(entries.length, "{n} entry", "{n} entries")} · <span className="font-medium text-ink">{money(total, currency)}</span>
            </DialogDescription>
          )}
        </div>
        {entries === null ? (
          <Loading className="grid gap-3.5 px-6 py-5">
            {[58, 40, 66, 48].map((w, i) => (
              <span key={i} className="flex items-center gap-3">
                <Skeleton className="w-14" />
                <Skeleton style={{ width: `${w}%` }} />
                <Skeleton className="ml-auto w-16" />
              </span>
            ))}
          </Loading>
        ) : entries.length === 0 ? (
          <p className="px-6 py-6 text-[13.5px] text-ink-2">{t("No entries.")}</p>
        ) : (
          <ul className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-2">
            {entries.map((e) => (
              <li key={e.id}>
                <button
                  type="button"
                  disabled={!canWrite}
                  onClick={() => editEntry(e)}
                  className={cn("flex h-10 w-full items-center gap-3 rounded-[10px] px-4 text-left text-[14px]", canWrite && "hover:bg-wash")}
                >
                  {show.date && <span className="w-24 shrink-0 text-[13px] text-ink-2 tabular-nums">{dateLabel(e.date, { month: "short", day: "numeric", weekday: "short" })}</span>}
                  <span className="min-w-0 flex-1 truncate">{e.name}</span>
                  {show.type && <span className="w-32 shrink-0">{e.type && <Tag name={e.type} color={optionColor(options, "type", e.type)} className="!shadow-none !bg-transparent !px-0 text-[13px] text-ink-2" />}</span>}
                  {show.card && <span className="w-32 shrink-0 max-sm:hidden">{e.card && <Tag name={e.card} color={optionColor(options, "card", e.card)} className="!shadow-none !bg-transparent !px-0 text-[13px] text-ink-2" />}</span>}
                  <span className={cn("w-28 shrink-0 text-right tabular-nums", e.amount < 0 && "text-good")}>{money(e.amount, currency)}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  )
}
