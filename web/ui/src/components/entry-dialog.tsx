import { useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { CalendarIcon, CheckIcon, ChevronDownCircleIcon, CoinsIcon, HashIcon, Trash2Icon, Undo2Icon } from "lucide-react"
import { toast } from "sonner"

import { OptionSelect, Tag } from "@/components/option-select"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Spinner } from "@/components/ui/spinner"
import { api, currencySymbol, dateLabel, money, monthLabel, today, type Entry, type FxRate, type Suggestion } from "@/lib/api"
import { CURRENCIES } from "@/lib/currencies"
import { optionColor, useApp } from "@/lib/state"
import { matchAt } from "@/lib/search"
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
 * another with names used before suggested as they're typed; one from the
 * table to change or delete (tapping a line on a phone); or a refund (R),
 * filled in the same way, which deletes the purchase with the same name and
 * amount, or with none, is added as a negative amount.
 */
export function EntryDialog({
  open,
  onOpenChange,
  entry,
  refund = false,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  entry?: Entry | null
  refund?: boolean
}) {
  const { options, reloadOptions, bumpTable, ledger } = useApp()
  const [name, setName] = useState("")
  const [amount, setAmount] = useState("")
  const [type, setType] = useState("")
  const [card, setCard] = useState("")
  const [date, setDate] = useState(today())
  // The currency the amount is typed in: the ledger's, or another, converted at the date's rate when saved (only the ledger's amount is kept).
  const [cur, setCur] = useState(ledger.currency)
  const [rate, setRate] = useState<{ key: string; rate?: FxRate; error?: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const amountRef = useRef<HTMLInputElement>(null)
  const nameRef = useRef<HTMLInputElement>(null)
  // What was just added, while the form stays open for the next one.
  const [added, setAdded] = useState<{ name: string; month: string; n: number } | null>(null)
  // Names used before, loaded when the form opens; those matching what's typed (by pinyin too) are offered, and picking one fills in the rest.
  const [known, setKnown] = useState<Suggestion[]>([])
  const [suggesting, setSuggesting] = useState(false) // the list is wanted: typing in the name
  const [active, setActive] = useState(-1)
  // A refund: the purchases with this name and amount, for the text they're for; the one picked is deleted.
  const [found, setFound] = useState<{ key: string; matches: Entry[] } | null>(null)
  const [pickedMatch, setPickedMatch] = useState(0)

  useEffect(() => {
    if (!open) return
    setConfirmDelete(false)
    setAdded(null)
    setSuggesting(false)
    setFound(null)
    setCur(ledger.currency)
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, entry])

  useEffect(() => {
    if (!open || entry) return
    let live = true
    api
      .suggest("", 1000)
      .then((list) => live && setKnown(list))
      .catch(() => {})
    return () => {
      live = false
    }
  }, [open, entry])
  // Names that start with what's typed first, then the most used, then the latest.
  const suggestions = useMemo(() => {
    const q = name.trim()
    if (!suggesting || !q) return []
    return known
      .map((sg) => ({ sg, at: matchAt(sg.name, q) }))
      .filter((x) => x.at >= 0)
      .sort((a, b) => Number(a.at !== 0) - Number(b.at !== 0) || b.sg.uses - a.sg.uses || b.sg.date.localeCompare(a.sg.date))
      .slice(0, 6)
      .map((x) => x.sg)
  }, [known, name, suggesting])
  const showSuggestions = suggesting && suggestions.length > 0

  const pick = (sg: Suggestion) => {
    setName(sg.name)
    setType(sg.type)
    if (sg.card) setCard(sg.card)
    setAmount(sg.amount.toFixed(2))
    // That amount is in the ledger's currency.
    setCur(ledger.currency)
    setSuggesting(false)
    requestAnimationFrame(() => {
      amountRef.current?.focus()
      amountRef.current?.select()
    })
  }

  const onNameKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing) return
    // The arrows go round the suggestions, as in the type and card lists.
    if (showSuggestions && e.key === "ArrowDown") {
      e.preventDefault()
      setActive((a) => (a + 1) % suggestions.length)
    } else if (showSuggestions && e.key === "ArrowUp") {
      e.preventDefault()
      setActive((a) => (a <= 0 ? suggestions.length - 1 : a - 1))
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

  const typed = parseAmount(amount)
  // Another currency: its rate for the date, and the amount in the ledger's currency, to the cent.
  const foreign = !entry && cur !== ledger.currency
  const rateKey = `${cur}|${ledger.currency}|${date}`
  useEffect(() => {
    if (!open || !foreign || !date) return
    let live = true
    api
      .fx(cur, ledger.currency, date)
      .then((r) => live && setRate({ key: rateKey, rate: r }))
      .catch((e) => live && setRate({ key: rateKey, error: (e as Error).message }))
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, foreign, rateKey])
  const fx = foreign && rate?.key === rateKey ? rate : null
  const n = !foreign ? typed : fx?.rate && Number.isFinite(typed) ? Math.round(typed * fx.rate.rate * 100) / 100 : NaN
  // A refund needs everything: the name and amount to match, the rest to record it if nothing does.
  const ready = refund
    ? name.trim() !== "" && Number.isFinite(n) && n !== 0 && type !== "" && card !== "" && date !== ""
    : name.trim() !== "" && Number.isFinite(n)

  const changed = !!entry && (name !== entry.name || n !== entry.amount || type !== entry.type || card !== entry.card || date !== entry.date)

  const matchKey = `${name.trim().toLowerCase()}|${Number.isFinite(n) ? Math.abs(n) : ""}`
  useEffect(() => {
    if (!open || !refund || !name.trim() || !Number.isFinite(n) || n === 0) return
    let live = true
    const timer = setTimeout(() => {
      api
        .refundMatches(name.trim(), Math.abs(n))
        .then((matches) => {
          if (!live) return
          setFound({ key: matchKey, matches })
          setPickedMatch(0)
        })
        .catch((e) => toast.error((e as Error).message))
    }, 150)
    return () => {
      live = false
      clearTimeout(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, refund, matchKey])
  const matchesReady = found?.key === matchKey
  const matches = matchesReady ? found.matches : []
  const picked = matches[Math.min(pickedMatch, matches.length - 1)]

  const applyRefund = async () => {
    const res = await api.refund(picked ? { id: picked.id } : { name: name.trim(), amount: Math.abs(n), type, card, date })
    if (res.matches) {
      // Something changed in the meantime: show what matches now.
      setFound({ key: matchKey, matches: res.matches })
      return
    }
    if (res.refunded) {
      const { name, type, amount, date, card } = res.refunded
      toast.success(t("Refunded: deleted {name}.", { name }), {
        action: {
          label: t("Undo"),
          onClick: () => {
            api.createEntry({ name, type, amount, date, card }).then(bumpTable, (err) => toast.error((err as Error).message))
          },
        },
      })
    } else if (res.added) {
      toast.success(t("Added a refund of {amount}.", { amount: money(Math.abs(res.added.amount), ledger.currency) }))
    }
    bumpTable()
    void reloadOptions()
    onOpenChange(false)
  }

  // Adding keeps the form open for the next entry (same date and card); saving a change closes it.
  const save = async (e?: FormEvent) => {
    e?.preventDefault()
    if (!ready || busy || (refund && !matchesReady)) return
    setBusy(true)
    try {
      if (refund) {
        await applyRefund()
        return
      }
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
        className="gap-0 overflow-hidden rounded-[20px] border border-rule-2 bg-sheet p-0 shadow-[0_16px_48px_rgb(0_0_0/.3)] ring-0 sm:max-w-[500px]"
        onEscapeKeyDown={(e) => {
          // Escape closes the suggestions first, then the form; changes to an entry are saved on the way out.
          if (showSuggestions) {
            e.preventDefault()
            setSuggesting(false)
          } else if (entry && changed && ready) {
            e.preventDefault()
            void save()
          }
        }}
      >
        <form onSubmit={save}>
          <div className="relative px-7 pt-7 pb-2">
            {refund ? (
              <DialogTitle className="mb-2 flex items-center gap-2 text-[13px] font-normal text-ink-2">
                <Undo2Icon className="size-4" /> {t("Refund")}
              </DialogTitle>
            ) : (
              <DialogTitle className="sr-only">{entry ? t("Edit entry") : t("New entry")}</DialogTitle>
            )}
            <DialogDescription className="sr-only">{t("The entry's name, amount, type, card and date.")}</DialogDescription>
            <input
              ref={nameRef}
              id="entry-name"
              autoFocus={!entry}
              value={name}
              onChange={(e) => {
                setName(e.target.value)
                setSuggesting(true)
                setActive(-1)
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
                className="absolute inset-x-5 top-full z-10 mt-1 grid gap-0.5 rounded-[14px] bg-sheet p-1 shadow-[0_16px_48px_rgb(0_0_0/.18)]"
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
                      <span className="w-24 text-right tabular-nums">{money(sg.amount, ledger.currency)}</span>
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
                <span className="text-muted-foreground">{currencySymbol(foreign ? cur : ledger.currency)}</span>
                <input
                  ref={amountRef}
                  id="entry-amount"
                  inputMode="decimal"
                  value={amount}
                  onChange={(e) => setAmount(e.target.value)}
                  placeholder="0.00"
                  aria-label={t("Amount")}
                  className={cn("h-[34px] w-full min-w-0 bg-transparent text-[14px] tabular-nums outline-none placeholder:text-muted-foreground", typed < 0 && !refund && "text-red")}
                />
              </span>
            </div>
            {foreign && (
              // What it comes to in the ledger's currency, which is what's saved.
              <div className={row}>
                <span />
                <span className="-mt-1 px-2 text-[12.5px] text-ink-2 tabular-nums" role="status" id="entry-converted">
                  {!fx ? (
                    t("Getting the rate…")
                  ) : fx.error ? (
                    <span className="text-red">{fx.error}</span>
                  ) : (
                    <>
                      {Number.isFinite(n) && amount.trim() !== "" && <span className="font-medium text-ink">≈ {money(n, ledger.currency)} · </span>}
                      {t("1 {from} = {rate} {to}", { from: cur, rate: String(+fx.rate!.rate.toPrecision(6)), to: ledger.currency })}
                      {fx.rate!.date !== date && <> · {dateLabel(fx.rate!.date, { month: "short", day: "numeric" })}</>}
                    </>
                  )}
                </span>
              </div>
            )}
            <div className={row}>
              <span className={name_}><ChevronDownCircleIcon /> {t("Type")}</span>
              <OptionSelect kind="type" label={t("Type")} placeholder={t("Empty")} value={type} options={options.types} onChange={setType} className="rounded-md hover:bg-wash" />
            </div>
            <div className={row}>
              <span className={name_}><ChevronDownCircleIcon /> {t("Card")}</span>
              <OptionSelect kind="card" label={t("Card")} placeholder={t("Empty")} value={card} options={options.cards} onChange={setCard} className="rounded-md hover:bg-wash" />
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
            {!entry && (
              // Paid in another currency: typed in it, saved in the ledger's.
              <div className={row}>
                <span className={name_}><CoinsIcon /> {t("Currency")}</span>
                <select
                  id="entry-currency"
                  value={cur}
                  onChange={(e) => setCur(e.target.value)}
                  aria-label={t("Currency")}
                  className="h-[34px] w-fit cursor-pointer appearance-none rounded-md bg-transparent px-2 text-[14px] outline-none hover:bg-wash focus:bg-wash"
                >
                  <option value={ledger.currency}>{ledger.currency}</option>
                  {CURRENCIES.filter((c) => c !== ledger.currency).map((c) => (
                    <option key={c} value={c}>{c}</option>
                  ))}
                </select>
              </div>
            )}
            {refund && matches.length > 0 && (
              <ul role="listbox" aria-label={t("Matching purchases")} className="mt-3 grid gap-0.5">
                {matches.map((m, i) => (
                  <li key={m.id}>
                    <button
                      type="button"
                      role="option"
                      aria-selected={m === picked}
                      onClick={() => setPickedMatch(i)}
                      className={cn("flex h-9 w-full items-center gap-3 rounded-[10px] px-2.5 text-left text-[13.5px]", m === picked ? "bg-red-wash" : "hover:bg-wash")}
                    >
                      <span className={cn("min-w-0 flex-1 truncate", m === picked && "font-medium text-red line-through decoration-1")}>{m.name}</span>
                      <span className="text-[12.5px] text-ink-2 tabular-nums">{dateLabel(m.date, { month: "short", day: "numeric" })}</span>
                      {m.card && <span className="max-w-28 truncate text-[12.5px] text-ink-2">{m.card}</span>}
                      <span className={cn("w-24 text-right tabular-nums", m === picked && "text-red")}>{money(m.amount, ledger.currency)}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
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
            <button type="submit" className={cn("btn", refund && picked && "danger")} disabled={!ready || busy || (refund && !matchesReady)}>
              {busy && <Spinner className="size-3.5" />}
              {refund ? (picked ? t("Delete purchase") : t("Add refund")) : entry ? t("Save") : t("Add")}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
