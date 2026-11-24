import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { CalendarIcon, CheckIcon, ChevronDownCircleIcon, HashIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { OptionSelect, Tag } from "@/components/option-select"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Spinner } from "@/components/ui/spinner"
import { api, currencySymbol, money, monthLabel, today, type Entry, type Suggestion } from "@/lib/api"
import { optionColor, useApp } from "@/lib/state"
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
 * The form for one entry: new ones (the New button, or N), added one after
 * another with names used before suggested as they're typed; or one from the
 * table to change or delete (tapping a line on a phone).
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
  const nameRef = useRef<HTMLInputElement>(null)
  // What was just added, while the form stays open for the next one.
  const [added, setAdded] = useState<{ name: string; month: string; n: number } | null>(null)
  // Names used before that hold what's being typed; picking one fills in the rest.
  const [suggestions, setSuggestions] = useState<Suggestion[]>([])
  const [suggesting, setSuggesting] = useState(false) // the list is wanted: typing in the name
  const [active, setActive] = useState(-1)

  useEffect(() => {
    if (!open) return
    setConfirmDelete(false)
    setAdded(null)
    setSuggesting(false)
    if (entry) {
      setName(entry.name)
      setAmount(entry.amount.toFixed(2))
      setType(entry.type)
      setCard(entry.card)
      setDate(entry.date)
    } else {
      setName("")
      setAmount("")
      setType("")
      setCard("")
      setDate(today())
    }
  }, [open, entry])

  useEffect(() => {
    const q = name.trim()
    if (entry || !suggesting || !q) {
      setSuggestions([])
      return
    }
    let live = true
    const timer = setTimeout(() => {
      api
        .suggest(q)
        .then((list) => {
          if (!live) return
          setSuggestions(list)
          setActive(-1)
        })
        .catch(() => {})
    }, 120)
    return () => {
      live = false
      clearTimeout(timer)
    }
  }, [name, suggesting, entry])
  const showSuggestions = suggesting && suggestions.length > 0

  const pick = (sg: Suggestion) => {
    setName(sg.name)
    setType(sg.type)
    if (sg.card) setCard(sg.card)
    setAmount(sg.amount.toFixed(2))
    setSuggesting(false)
    requestAnimationFrame(() => {
      amountRef.current?.focus()
      amountRef.current?.select()
    })
  }

  const onNameKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing) return
    if (showSuggestions && e.key === "ArrowDown") {
      e.preventDefault()
      setActive((a) => Math.min(a + 1, suggestions.length - 1))
    } else if (showSuggestions && e.key === "ArrowUp") {
      e.preventDefault()
      setActive((a) => Math.max(a - 1, -1))
    } else if (e.key === "Enter") {
      if (showSuggestions && active >= 0) {
        e.preventDefault()
        pick(suggestions[active])
      } else if (!ready && name.trim()) {
        // Nothing to add yet: on to the amount.
        e.preventDefault()
        setSuggesting(false)
        amountRef.current?.focus()
      }
    }
  }

  const n = parseAmount(amount)
  const ready = name.trim() !== "" && Number.isFinite(n)

  // Adding keeps the form open for the next entry (same date and card); saving a change closes it.
  const save = async (e: FormEvent) => {
    e.preventDefault()
    if (!ready || busy) return
    setBusy(true)
    try {
      const fields = { name, amount: n, type, card, date }
      const saved = entry ? await api.updateEntry(entry.id, fields) : await api.createEntry(fields)
      bumpTable()
      void reloadOptions()
      if (entry) {
        toast.success(t("Saved {name}.", { name: saved.name }))
        onOpenChange(false)
      } else {
        setAdded((a) => ({ name: saved.name, month: monthLabel(saved.date.slice(0, 7)), n: (a?.n ?? 0) + 1 }))
        setName("")
        setAmount("")
        setType("")
        setSuggesting(false)
        nameRef.current?.focus()
      }
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
      <DialogContent
        className="gap-0 overflow-hidden rounded-[20px] border-0 bg-sheet p-0 shadow-[0_16px_48px_rgb(0_0_0/.3),0_0_0_1px_var(--rule-2)] ring-0 sm:max-w-[500px]"
        onEscapeKeyDown={(e) => {
          // Escape closes the suggestions first, then the form.
          if (showSuggestions) {
            e.preventDefault()
            setSuggesting(false)
          }
        }}
      >
        <form onSubmit={save}>
          <div className="relative px-7 pt-7 pb-2">
            <DialogTitle className="sr-only">{entry ? t("Edit entry") : t("New entry")}</DialogTitle>
            <DialogDescription className="sr-only">{t("The entry's name, amount, type, card and date.")}</DialogDescription>
            <input
              ref={nameRef}
              id="entry-name"
              autoFocus={!entry}
              value={name}
              onChange={(e) => {
                setName(e.target.value)
                setSuggesting(true)
              }}
              onKeyDown={onNameKey}
              onBlur={() => setSuggesting(false)}
              placeholder={t("Untitled")}
              maxLength={200}
              aria-label={t("Name")}
              role="combobox"
              aria-expanded={showSuggestions}
              aria-controls="entry-suggestions"
              aria-autocomplete="list"
              autoComplete="off"
              className="w-full bg-transparent text-[24px] leading-tight font-semibold outline-none placeholder:text-muted-foreground/50"
            />
            {showSuggestions && (
              <ul
                id="entry-suggestions"
                role="listbox"
                aria-label={t("Used before")}
                className="absolute inset-x-5 top-full z-10 mt-1 grid gap-0.5 rounded-[14px] bg-sheet p-1 shadow-[0_16px_48px_rgb(0_0_0/.18),0_0_0_1px_var(--rule-2)]"
              >
                <li className="px-2.5 pt-1 pb-0.5 text-[11.5px] text-muted-foreground">{t("Used before")}</li>
                {suggestions.map((sg, i) => (
                  <li key={sg.name}>
                    <button
                      type="button"
                      role="option"
                      aria-selected={i === active}
                      // Keep the focus in the name, so picking doesn't close the list first.
                      onMouseDown={(e) => e.preventDefault()}
                      onMouseEnter={() => setActive(i)}
                      onClick={() => pick(sg)}
                      className={cn("flex h-9 w-full items-center gap-2.5 rounded-[10px] px-2.5 text-left text-[14px]", i === active && "bg-wash")}
                    >
                      <span className="min-w-0 flex-1 truncate font-medium">{sg.name}</span>
                      {sg.type && <Tag name={sg.type} color={optionColor(options, "type", sg.type)} className="!shadow-none !bg-transparent !px-0 text-[12.5px] text-ink-2" />}
                      {sg.uses > 1 && <span className="text-[12px] text-muted-foreground tabular-nums">×{sg.uses}</span>}
                      <span className="w-24 text-right tabular-nums">{money(sg.amount, user.prefs.currency)}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
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
          </div>
          <div className="flex flex-wrap items-center gap-1.5 border-t border-rule px-5 py-3.5">
            {entry ? (
              <button type="button" className="btn ghost !text-red" onClick={remove} disabled={busy}>
                <Trash2Icon /> {confirmDelete ? t("Click again to delete") : t("Delete")}
              </button>
            ) : (
              added && (
                <span className="flex min-w-0 items-center gap-1.5 px-2 text-[12.5px] text-ink-2" role="status">
                  <CheckIcon className="size-3.5 shrink-0 text-good" />
                  <span className="truncate">
                    {added.n > 1 ? t("Added {name} · {n} so far", { name: added.name, n: added.n }) : t("Added {name} to {month}.", { name: added.name, month: added.month })}
                  </span>
                </span>
              )
            )}
            <span className="flex-1" />
            <button type="button" className="btn ghost" onClick={() => onOpenChange(false)}>
              {entry || !added ? t("Cancel") : t("Done")}
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
