import { useEffect, useRef, useState, type FormEvent } from "react"
import { CalendarIcon, ChevronDownCircleIcon, HashIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { OptionSelect } from "@/components/option-select"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Spinner } from "@/components/ui/spinner"
import { api, currencySymbol, monthLabel, today, type Entry } from "@/lib/api"
import { useApp } from "@/lib/state"
import { cn } from "@/lib/utils"
import { t } from "@/lib/i18n"

/** "12.5", "¥1,200", "1 200,50", "(12.50)" → a number; NaN when it isn't one. */
export function parseAmount(s: string): number {
  let t = s.trim()
  const paren = /^\(.*\)$/.test(t)
  t = t.replace(/[^\d.,\-−]/g, "").replace("−", "-")
  if (/,\d{1,2}$/.test(t) && !t.includes(".")) t = t.replace(/\./g, "").replace(",", ".")
  t = t.replace(/,/g, "")
  if (t === "" || t === "-") return NaN
  const n = Number(t)
  return paren ? -Math.abs(n) : n
}

/**
 * The form for one entry: a new one (the New entry button, or N), or one from
 * the table to change or delete (tapping a line on a phone).
 */
export function EntryDialog({
  open,
  onOpenChange,
  entry,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  entry?: Entry | null
}) {
  const { options, user, reloadOptions, bumpTable } = useApp()
  const [name, setName] = useState("")
  const [amount, setAmount] = useState("")
  const [type, setType] = useState("")
  const [card, setCard] = useState("")
  const [date, setDate] = useState(today())
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const amountRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    setConfirmDelete(false)
    if (entry) {
      setName(entry.name)
      setAmount(entry.amount.toFixed(2))
      setType(entry.type)
      setCard(entry.card)
      setDate(entry.date)
    } else {
      setName("")
      setAmount("")
      setDate(today())
    }
  }, [open, entry])

  const n = parseAmount(amount)
  const ready = name.trim() !== "" && Number.isFinite(n)

  const save = async (e: FormEvent, again = false) => {
    e.preventDefault()
    if (!ready || busy) return
    setBusy(true)
    try {
      const fields = { name, amount: n, type, card, date }
      const saved = entry ? await api.updateEntry(entry.id, fields) : await api.createEntry(fields)
      toast.success(entry ? t("Saved {name}.", { name: saved.name }) : t("Added {name} to {month}.", { name: saved.name, month: monthLabel(saved.date.slice(0, 7)) }))
      bumpTable()
      void reloadOptions()
      if (again) {
        setName("")
        setAmount("")
        document.getElementById("entry-name")?.focus()
      } else onOpenChange(false)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!entry) return
    if (!confirmDelete) {
      setConfirmDelete(true)
      return
    }
    setBusy(true)
    try {
      await api.deleteEntries([entry.id])
      toast.success(t("Deleted {name}.", { name: entry.name }))
      bumpTable()
      void reloadOptions()
      onOpenChange(false)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const row = "grid grid-cols-[120px_minmax(0,1fr)] items-center gap-2 min-h-[34px]"
  const name_ = "flex items-center gap-1.5 text-[14px] text-muted-foreground [&_svg]:size-4"
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-0 overflow-hidden rounded-[20px] border-0 bg-sheet p-0 shadow-[0_16px_48px_rgb(0_0_0/.3),0_0_0_1px_var(--rule-2)] ring-0 sm:max-w-[500px]">
        <form onSubmit={save}>
          <div className="px-7 pt-7 pb-2">
            <DialogTitle className="sr-only">{entry ? t("Edit entry") : t("New entry")}</DialogTitle>
            <DialogDescription className="sr-only">{t("The entry's name, amount, type, card and date.")}</DialogDescription>
            <input
              id="entry-name"
              autoFocus={!entry}
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("Untitled")}
              maxLength={200}
              aria-label={t("Name")}
              className="w-full bg-transparent text-[24px] leading-tight font-semibold outline-none placeholder:text-muted-foreground/50"
            />
          </div>
          <div className="grid gap-0.5 px-7 pt-2 pb-6">
            <div className={row}>
              <span className={name_}><HashIcon /> {t("Amount")}</span>
              <span className="flex items-center gap-1.5 rounded-[8px] px-2 hover:bg-wash focus-within:bg-wash">
                <span className="text-muted-foreground">{currencySymbol(user.prefs.currency)}</span>
                <input
                  ref={amountRef}
                  id="entry-amount"
                  inputMode="decimal"
                  value={amount}
                  onChange={(e) => setAmount(e.target.value)}
                  placeholder="0.00"
                  aria-label={t("Amount")}
                  className={cn("h-[34px] w-full min-w-0 bg-transparent text-[14px] tabular-nums outline-none placeholder:text-muted-foreground", n < 0 && "text-red")}
                />
              </span>
            </div>
            <div className={row}>
              <span className={name_}><ChevronDownCircleIcon /> {t("Type")}</span>
              <OptionSelect kind="type" label={t("Type")} placeholder={t("Empty")} value={type} options={options.types} onChange={setType} className="rounded-md hover:bg-wash" />
            </div>
            <div className={row}>
              <span className={name_}><CalendarIcon /> {t("Date")}</span>
              <span className="flex items-center gap-2">
                <input id="entry-date" type="date" className="h-[34px] rounded-md bg-transparent px-2 text-[14px] tabular-nums outline-none hover:bg-wash focus:bg-wash" value={date} onChange={(e) => e.target.value && setDate(e.target.value)} aria-label={t("Date")} />
                {date !== today() && (
                  <button type="button" className="pill soft !h-7 !px-3 text-[13px]" onClick={() => setDate(today())}>
                    {t("Today")}
                  </button>
                )}
              </span>
            </div>
            <div className={row}>
              <span className={name_}><ChevronDownCircleIcon /> {t("Card")}</span>
              <OptionSelect kind="card" label={t("Card")} placeholder={t("Empty")} value={card} options={options.cards} onChange={setCard} className="rounded-md hover:bg-wash" />
            </div>
            <p className="mt-2 text-[12.5px] text-muted-foreground">{t("A refund is a negative amount, such as −20.")}</p>
          </div>
          <div className="flex flex-wrap items-center gap-1.5 border-t border-rule px-5 py-3.5">
            {entry ? (
              <button type="button" className="btn ghost !text-red" onClick={remove} disabled={busy}>
                <Trash2Icon /> {confirmDelete ? t("Click again to delete") : t("Delete")}
              </button>
            ) : (
              <button type="button" className="btn ghost" disabled={!ready || busy} onClick={(e) => void save(e, true)}>
                {t("Save and add another")}
              </button>
            )}
            <span className="flex-1" />
            <button type="button" className="btn ghost" onClick={() => onOpenChange(false)}>
              {t("Cancel")}
            </button>
            <button type="submit" className="btn" disabled={!ready || busy}>
              {busy && <Spinner className="size-3.5" />}
              {entry ? t("Save") : t("Add")}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
