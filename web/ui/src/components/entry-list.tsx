import { XIcon } from "lucide-react"

import { Tag } from "@/components/option-select"
import { Loading, Skeleton } from "@/components/ui/skeleton"
import { dateLabel, money, type Entry } from "@/lib/api"
import { t } from "@/lib/i18n"
import { optionColor, useApp } from "@/lib/state"
import { cn } from "@/lib/utils"

/**
 * The entries behind a part of a chart (a type, a card, a day), shown under
 * it: its name, then one row each; a row opens the entry to change it.
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
  const { user, options, editEntry } = useApp()
  const currency = user.prefs.currency
  return (
    <div className="mt-4 rounded-[14px] bg-side p-1.5 shadow-[inset_0_0_0_1px_var(--rule)]" role="region" aria-label={title}>
      <div className="flex items-center gap-2 px-2.5 pt-1.5 pb-2 text-[13px]">
        {color && <span className="dot" data-color={color} />}
        <span className="min-w-0 truncate font-semibold">{title}</span>
        <span className="flex-1" />
        <button type="button" aria-label={t("Close")} onClick={onClose} className="grid size-6 shrink-0 place-items-center rounded-md text-ink-2 hover:bg-wash hover:text-ink">
          <XIcon className="size-3.5" />
        </button>
      </div>
      {entries === null ? (
        <Loading className="grid gap-3 px-2.5 pt-1.5 pb-3">
          {[58, 40, 66].map((w, i) => (
            <span key={i} className="flex items-center gap-3">
              <Skeleton className="w-12" />
              <Skeleton style={{ width: `${w}%` }} />
              <Skeleton className="ml-auto w-16" />
            </span>
          ))}
        </Loading>
      ) : entries.length === 0 ? (
        <p className="px-2.5 pb-2 text-[13px] text-ink-2">{t("No entries.")}</p>
      ) : (
        <ul className="max-h-80 overflow-y-auto">
          {entries.map((e) => (
            <li key={e.id}>
              <button
                type="button"
                onClick={() => editEntry(e)}
                className="flex h-9 w-full items-center gap-3 rounded-[10px] px-2.5 text-left text-[13.5px] hover:bg-sheet hover:shadow-[0_0_0_1px_var(--rule)]"
              >
                {show.date && (
                  <span className="w-16 shrink-0 text-[12.5px] text-ink-2 tabular-nums">{dateLabel(e.date, { month: "short", day: "numeric" })}</span>
                )}
                <span className="min-w-0 flex-1 truncate">{e.name}</span>
                {show.type && e.type && <Tag name={e.type} color={optionColor(options, "type", e.type)} className="!shadow-none !bg-transparent !px-0 max-w-32 text-[12.5px] text-ink-2" />}
                {show.card && e.card && <Tag name={e.card} color={optionColor(options, "card", e.card)} className="!shadow-none !bg-transparent !px-0 max-w-32 text-[12.5px] text-ink-2 max-sm:hidden" />}
                <span className={cn("w-24 shrink-0 text-right tabular-nums", e.amount < 0 && "text-good")}>{money(e.amount, currency)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
