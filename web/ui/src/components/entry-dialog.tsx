import { useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { CalendarIcon, CheckIcon, ChevronDownCircleIcon, CoinsIcon, HashIcon, PencilIcon, PlusIcon, Trash2Icon, TypeIcon, Undo2Icon } from "lucide-react"
import { toast } from "sonner"

import { DatePicker } from "@/components/date-picker"
import { EntryHistory } from "@/components/entry-history"
import { ChoiceSelect, OptionSelect, Tag } from "@/components/option-select"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Spinner } from "@/components/ui/spinner"
import { api, currencySymbol, dateLabel, money, today, type Entry, type FxRate, type Suggestion } from "@/lib/api"
import { CURRENCIES } from "@/lib/currencies"
import { lastCard, rememberCard } from "@/lib/options"
import { Original } from "@/lib/money"
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
  of,
  onSaved,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  entry?: Entry | null
  /** Changing an entry: called with it as saved, or null when it was deleted. */
  onSaved?: (e: Entry | null) => void
  refund?: boolean
  /** A refund of this purchase: the form starts from it, with it picked. */
  of?: Entry | null
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
  const formRef = useRef<HTMLFormElement>(null)
  // Shift+Enter adds (or saves, or refunds); a plain Enter is for picking in
  // the lists, and never does, unless it's on the button itself.
  const plainEnter = useRef(false)
  useEffect(() => {
    if (!open) return
    const down = (e: globalThis.KeyboardEvent) => {
      if (e.key !== "Enter" || e.isComposing || editingRef.current) return
      if (e.shiftKey) {
        e.preventDefault()
        e.stopPropagation()
        plainEnter.current = false
        formRef.current?.requestSubmit()
        return
      }
      const on = document.activeElement
      plainEnter.current = !(on instanceof HTMLButtonElement && on.type === "submit")
    }
    const up = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Enter") plainEnter.current = false
    }
    window.addEventListener("keydown", down, true)
    window.addEventListener("keyup", up, true)
    return () => {
      window.removeEventListener("keydown", down, true)
      window.removeEventListener("keyup", up, true)
    }
  }, [open])
  const nameRef = useRef<HTMLInputElement>(null)
  // What was added since the form opened, newest first, while it stays open
  // for the next one; and the one of them being changed, in a form of its own.
  // A deleted one stays, struck through, to be brought back. Each keeps the
  // key it came with (brought back, it's a new entry), so its row stays put;
  // one deleted in its own form closes up (leaving) and goes.
  const [added, setAdded] = useState<(Entry & { key: number; deleted?: boolean; leaving?: boolean })[]>([])
  const [editing, setEditing] = useState<Entry | null>(null)
  const editingRef = useRef(false)
  editingRef.current = !!editing
  // Names used before, loaded when the form opens; those matching what's typed (by pinyin too) are offered, and picking one fills in the rest.
  const [known, setKnown] = useState<Suggestion[]>([])
  const [suggesting, setSuggesting] = useState(false) // the list is wanted: typing in the name
  const [active, setActive] = useState(-1)
  // A refund: the purchases with this name costing at least this much, for the text they're for. The one
  // picked is deleted (the same amount) or goes down by the refund (more); -1 adds the refund as new.
  const [found, setFound] = useState<{ key: string; matches: Entry[] } | null>(null)
  const [pickedMatch, setPickedMatch] = useState(-1)
  // The purchase last picked from the list, kept picked as the list comes back for its amount.
  const keepRef = useRef<number | null>(null)

  useEffect(() => {
    if (!open) return
    setConfirmDelete(false)
    setAdded([])
    setEditing(null)
    setSuggesting(false)
    setFound(null)
    setCur(ledger.currency)
    if (refund && of) {
      setName(of.name)
      setAmount(of.amount.toFixed(2))
      setType(of.type)
      setCard(of.card)
      setDate(today())
    } else if (entry) {
      setName(entry.name)
      setAmount(entry.amount.toFixed(2))
      setType(entry.type)
      setCard(entry.card)
      setDate(entry.date)
    } else {
      setName("")
      setAmount("")
      setType("")
      // The card last added with: entries tend to come in a card's batches.
      setCard(lastCard(ledger.id, options.cards))
      setDate(today())
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, entry, of])

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
    // Paid as it was last time: the same card, and in the same currency.
    if (sg.card) setCard(sg.card)
    const elsewhere = sg.orig_currency && sg.orig_amount != null
    setAmount((elsewhere ? sg.orig_amount! : sg.amount).toFixed(2))
    setCur(elsewhere ? sg.orig_currency! : ledger.currency)
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
    } else if (e.key === "Tab" && !e.shiftKey && showSuggestions) {
      // On to the amount, taking the suggestion arrowed to, as Enter does.
      // Moved here rather than left to the browser, which loses its way when
      // the list closes as the name loses focus.
      e.preventDefault()
      if (active >= 0) {
        pick(suggestions[active])
      } else {
        setSuggesting(false)
        amountRef.current?.focus()
      }
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
  // A refund of one purchase picked in the table: no search, no other choices.
  const locked = refund && !!of
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
  // A new refund needs everything, to record it; one taken off a picked purchase only its amount (see ready).
  const readyFull = refund
    ? name.trim() !== "" && Number.isFinite(n) && n !== 0 && type !== "" && card !== "" && date !== ""
    : name.trim() !== "" && Number.isFinite(n)

  const changed = !!entry && (name !== entry.name || n !== entry.amount || type !== entry.type || card !== entry.card || date !== entry.date)

  const matchKey = `${name.trim().toLowerCase()}|${Number.isFinite(n) ? Math.abs(n) : ""}`
  useEffect(() => {
    // Purchases to take it off, by the amount alone until a name narrows them; none to look for when it's for one picked in the table.
    if (!open || !refund || locked || !Number.isFinite(n) || n === 0) return
    let live = true
    const timer = setTimeout(() => {
      api
        .refundMatches(name.trim(), Math.abs(n))
        .then((matches) => {
          if (!live) return
          // The purchase the form was opened for is among them, however long ago, while it costs at least the refund.
          const mine = of && name.trim().toLowerCase() === of.name.toLowerCase() && Math.round(of.amount * 100) >= Math.round(Math.abs(n) * 100)
          if (mine && !matches.some((m) => m.id === of.id)) matches = [of, ...matches]
          setFound({ key: matchKey, matches })
          // That purchase is picked, or the one just chosen; otherwise one of the same amount; one costing more only if chosen.
          const kept = keepRef.current !== null ? matches.findIndex((m) => m.id === keepRef.current) : -1
          keepRef.current = null
          setPickedMatch(mine ? matches.findIndex((m) => m.id === of.id) : kept >= 0 ? kept : matches.findIndex((m) => Math.round(m.amount * 100) === Math.round(Math.abs(n) * 100)))
        })
        .catch((e) => toast.error((e as Error).message))
    }, 150)
    return () => {
      live = false
      clearTimeout(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, refund, matchKey])
  const matchesReady = locked || found?.key === matchKey
  const matches = locked || !matchesReady ? [] : found!.matches
  // Opened for a purchase picked in the table, it's that one throughout, whatever the amount.
  const picked = locked ? of! : pickedMatch >= 0 ? matches[pickedMatch] : undefined
  // A refund bigger than what it's taken off.
  const tooMuch = !!picked && Number.isFinite(n) && Math.round(Math.abs(n) * 100) > Math.round(picked.amount * 100)
  // Picked, but costing more than the refund: it goes down by it.
  const partial = !!picked && Number.isFinite(n) && n !== 0 && Math.round(picked.amount * 100) !== Math.round(Math.abs(n) * 100)
  const left = picked ? Math.round((picked.amount - Math.abs(n)) * 100) / 100 : 0
  const ready = refund && picked ? Number.isFinite(n) && n !== 0 && !tooMuch : readyFull

  const applyRefund = async () => {
    const paidElsewhere = foreign ? { orig_amount: Math.abs(typed), orig_currency: cur } : {}
    const res = await api.refund(
      picked
        ? partial
          ? { id: picked.id, amount: Math.abs(n), ...paidElsewhere }
          : { id: picked.id }
        : // Added as a refund of its own, whatever matches.
          { name: name.trim(), amount: Math.abs(n), type, card, date, ...paidElsewhere, new: true },
    )
    if (res.matches) {
      // Something changed in the meantime: show what matches now.
      setFound({ key: matchKey, matches: res.matches })
      return
    }
    if (res.refunded) {
      const { name, type, amount, date, card, orig_amount, orig_currency } = res.refunded
      toast.success(t("Refunded: deleted {name}.", { name }), {
        action: {
          label: t("Undo"),
          onClick: () => {
            api.createEntry({ name, type, amount, date, card, orig_amount, orig_currency }).then(bumpTable, (err) => toast.error((err as Error).message))
          },
        },
      })
    } else if (res.reduced && picked) {
      const was = picked
      toast.success(t("Refunded {amount} of {name}: it's {left} now.", { amount: money(Math.abs(n), ledger.currency), name: was.name, left: money(res.reduced.amount, ledger.currency) }), {
        action: {
          label: t("Undo"),
          onClick: () => {
            api.updateEntry(was.id, { amount: was.amount, orig_amount: was.orig_amount, orig_currency: was.orig_currency ?? "" }).then(bumpTable, (err) => toast.error((err as Error).message))
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
    // A plain Enter in a field (or a list it opened) doesn't add: Shift+Enter does.
    if (plainEnter.current) return
    if (!ready || busy || (refund && !matchesReady)) return
    setBusy(true)
    try {
      if (refund) {
        await applyRefund()
        return
      }
      // Paid in another currency: what it was there is kept beside the ledger's amount.
      const fields = { name, amount: n, type, card, date, ...(foreign ? { orig_amount: typed, orig_currency: cur } : {}) }
      const saved = entry ? await api.updateEntry(entry.id, fields) : await api.createEntry(fields)
      if (!entry) rememberCard(ledger.id, saved.card)
      bumpTable()
      void reloadOptions()
      if (entry) {
        toast.success(t("Saved {name}.", { name: saved.name }))
        onSaved?.(saved)
        onOpenChange(false)
      } else {
        setAdded((a) => [{ ...saved, key: saved.id }, ...a])
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
      onSaved?.(null)
      onOpenChange(false)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  // Deletes one of the entries just added, at once; it stays in the list,
  // struck through, with a way back (here rather than in a toast, which is
  // outside the form: clicking it would close the form).
  const removeAdded = async (e: Entry) => {
    try {
      await api.deleteEntries([e.id])
      setAdded((a) => a.map((x) => (x.id === e.id ? { ...x, deleted: true } : x)))
      bumpTable()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }
  const restoreAdded = async (e: Entry) => {
    try {
      const back = await api.createEntry({ name: e.name, amount: e.amount, type: e.type, card: e.card, date: e.date, ...(e.orig_currency ? { orig_amount: e.orig_amount, orig_currency: e.orig_currency } : {}) })
      setAdded((a) => a.map((x) => (x.id === e.id ? { ...back, key: x.key } : x)))
      bumpTable()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }

  // The key a button answers to, beside its name; not on a touch screen.
  const keyHint = "ml-0.5 tracking-[0.08em] [@media(hover:none)]:hidden"

  // Labels on one line.
  const row = "grid grid-cols-[120px_minmax(0,1fr)] items-center gap-2 min-h-[34px]"
  const name_ = "flex items-center gap-1.5 whitespace-nowrap text-[14px] text-muted-foreground [&_svg]:size-4 [&_svg]:shrink-0"
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
        <form ref={formRef} onSubmit={save}>
          <div className="px-7 pt-6 pb-1">
            {/* Which form this is; the name is a field like the others, below. */}
            <DialogTitle className="flex items-center gap-2 text-[13px] font-normal text-ink-2">
              {refund ? <Undo2Icon className="size-4" /> : entry ? <PencilIcon className="size-4" /> : <PlusIcon className="size-4" />}
              {refund ? t("Refund") : entry ? t("Edit entry") : t("New entry")}
            </DialogTitle>
            <DialogDescription className="sr-only">{t("The entry's name, amount, type, card and date.")}</DialogDescription>
          </div>
          <div className="grid gap-0.5 px-7 pt-2 pb-6">
            <div className={cn(row, "relative")}>
              <span className={name_}><TypeIcon /> {t("Name")}</span>
              <span className="flex items-center rounded-[8px] px-2 hover:bg-wash focus-within:bg-wash">
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
                  placeholder={refund && picked ? picked.name : t("Untitled")}
                  readOnly={locked}
                  maxLength={200}
                  aria-label={t("Name")}
                  role="combobox"
                  aria-expanded={showSuggestions}
                  aria-controls="entry-suggestions"
                  aria-autocomplete="list"
                  autoComplete="off"
                  // Taking a refund off a purchase, its name stands in the field until another is typed.
                  className={cn("h-[34px] w-full min-w-0 bg-transparent text-[14px] font-medium outline-none placeholder:font-normal placeholder:text-muted-foreground", refund && picked && "placeholder:font-medium placeholder:text-ink")}
                />
              </span>
              {showSuggestions && (
                <ul
                  id="entry-suggestions"
                  role="listbox"
                  aria-label={t("Used before")}
                  className="absolute inset-x-0 top-full z-10 mt-1 grid gap-0.5 rounded-[14px] border border-rule-2 bg-sheet p-1 shadow-[0_16px_48px_rgb(0_0_0/.18)]"
                >
                  <li className="px-2.5 pt-1 pb-0.5 text-[11.5px] text-muted-foreground">{t("Used before")}</li>
                  {suggestions.map((sg, i) => (
                    <li key={sg.name}>
                      <button
                        type="button"
                        role="option"
                        aria-selected={i === active}
                        // Out of the Tab order: the arrows go round them, and Tab goes on to the amount.
                        tabIndex={-1}
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
            <div className={row}>
              <span className={name_}>{refund ? <Undo2Icon /> : <HashIcon />} {refund ? t("Refund") : t("Amount")}</span>
              <span className="flex items-center gap-1.5 rounded-[8px] px-2 hover:bg-wash focus-within:bg-wash">
                <span className="text-muted-foreground">{currencySymbol(foreign ? cur : ledger.currency)}</span>
                <input
                  ref={amountRef}
                  id="entry-amount"
                  inputMode="decimal"
                  value={amount}
                  onChange={(e) => setAmount(e.target.value)}
                  placeholder="0.00"
                  aria-label={refund ? t("Refund") : t("Amount")}
                  className={cn("h-[34px] w-full min-w-0 bg-transparent text-[14px] tabular-nums outline-none placeholder:text-muted-foreground", typed < 0 && !refund && "text-red")}
                />
              </span>
            </div>
            {refund && picked && (
              // The picked purchase's amount, as it is, under the refund that comes off it.
              <div className={row}>
                <span className={name_}><HashIcon /> {t("Amount")}</span>
                <span className="flex items-baseline gap-2 px-2 text-[14px] tabular-nums" id="refund-purchase-amount">
                  {money(picked.amount, ledger.currency)}
                  <Original entry={picked} />
                  {tooMuch && <span className="text-[12.5px] text-red">{t("The refund is more than it cost.")}</span>}
                </span>
              </div>
            )}
            {entry?.orig_currency && entry.orig_amount != null && (
              // Paid in another currency; changing the amount here leaves that behind.
              <div className={row}>
                <span />
                <span className="-mt-1 px-2 text-[12.5px] text-ink-2 tabular-nums" id="entry-original">
                  {n === entry.amount ? t("Paid {amount}", { amount: money(entry.orig_amount, entry.orig_currency) }) : t("The {amount} paid will be dropped", { amount: money(entry.orig_amount, entry.orig_currency) })}
                </span>
              </div>
            )}
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
            {!(refund && picked) && (
              // A new entry's (or new refund's) type, card and date; taking a refund off a purchase needs none.
              <>
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
                <DatePicker value={date} onChange={setDate} label={t("Date")} className="h-[34px] rounded-md bg-transparent px-2 text-[14px] outline-none hover:bg-wash focus:bg-wash" />
                {date !== today() && (
                  <button type="button" className="pill soft !h-7 !px-3 text-[13px]" onClick={() => setDate(today())}>
                    {t("Today")}
                  </button>
                )}
              </span>
            </div>
              </>
            )}
            {!entry && (
              // Paid in another currency: typed in it, saved in the ledger's.
              <div className={row}>
                <span className={name_}><CoinsIcon /> {t("Currency")}</span>
                <ChoiceSelect
                  id="entry-currency"
                  value={cur}
                  choices={[ledger.currency, ...CURRENCIES.filter((c) => c !== ledger.currency)]}
                  onChange={setCur}
                  label={t("Currency")}
                  className="h-[34px] w-fit cursor-pointer rounded-md bg-transparent px-2 text-left text-[14px] outline-none hover:bg-wash focus:bg-wash"
                />
              </div>
            )}
            {entry && <EntryHistory entry={entry} currency={ledger.currency} />}
            {refund && matches.length > 0 && (
              <ul role="listbox" aria-label={t("Matching purchases")} className="mt-3 grid gap-0.5">
                {matches.map((m, i) => {
                  const on = m === picked
                  const part = on && partial
                  return (
                    <li key={m.id}>
                      <button
                        type="button"
                        role="option"
                        aria-selected={on}
                        onClick={() => {
                          setPickedMatch(i)
                          // Found by how its amount starts (238 for 238.40): that was a search, so the refund is all of it.
                          const typedText = amount.trim()
                          if (!foreign && typedText && Math.round(m.amount * 100) !== Math.round(Math.abs(n) * 100) && m.amount.toFixed(2).startsWith(typedText)) {
                            keepRef.current = m.id
                            setAmount(m.amount.toFixed(2))
                          }
                        }}
                        className={cn("flex h-9 w-full items-center gap-3 rounded-[10px] px-2.5 text-left text-[13.5px]", on ? (part ? "bg-wash" : "bg-red-wash") : "hover:bg-wash")}
                      >
                        <span className={cn("min-w-0 flex-1 truncate", on && !part && "font-medium text-red line-through decoration-1", part && "font-medium")}>{m.name}</span>
                        <span className="text-[12.5px] text-ink-2 tabular-nums">{dateLabel(m.date, { month: "short", day: "numeric" })}</span>
                        {m.card && <span className="max-w-28 truncate text-[12.5px] text-ink-2">{m.card}</span>}
                        <span className={cn("text-right tabular-nums", on && !part && "text-red")}>
                          {part ? (
                            <>
                              <span className="text-ink-2 line-through decoration-1">{money(m.amount, ledger.currency)}</span> → <span className="font-medium">{money(left, ledger.currency)}</span>
                            </>
                          ) : (
                            money(m.amount, ledger.currency)
                          )}
                        </span>
                      </button>
                    </li>
                  )
                })}
                <li>
                  <button
                    type="button"
                    role="option"
                    aria-selected={!picked}
                    onClick={() => setPickedMatch(-1)}
                    className={cn("flex h-9 w-full items-center gap-2 rounded-[10px] px-2.5 text-left text-[13.5px]", !picked ? "bg-wash font-medium" : "text-ink-2 hover:bg-wash")}
                  >
                    <PlusIcon className="size-3.5" /> {t("Add as a new refund")}
                  </button>
                </li>
              </ul>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-1.5 border-t border-rule px-5 py-3.5">
            {entry ? (
              <button type="button" className="btn ghost !text-red" onClick={remove} disabled={busy}>
                <Trash2Icon /> {confirmDelete ? t("Click again to delete") : t("Delete")}
              </button>
            ) : null}
            <span className="flex-1" />
            <button type="button" className="btn ghost" onClick={() => onOpenChange(false)}>
              {entry || !added.some((e) => !e.deleted) ? t("Cancel") : t("Done")}
              {/* Changing an entry, Esc saves it; elsewhere it closes the form. */}
              {!entry && <kbd className={keyHint}>Esc</kbd>}
            </button>
            <button type="submit" title="Shift+Enter" className={cn("btn", refund && picked && !partial && !locked && "danger")} disabled={!ready || busy || (refund && !matchesReady)}>
              {busy && <Spinner className="size-3.5" />}
              {refund ? (picked ? (partial ? t("Refund part") : locked ? t("Refund") : t("Delete purchase")) : t("Add refund")) : entry ? t("Save") : t("Add")}
              <kbd className={cn(keyHint, refund && picked && !partial && !locked ? "" : "!bg-paper/15 !text-paper/75")} aria-label="Shift+Enter">⇧↵</kbd>
            </button>
          </div>
          {!entry && added.length > 0 && (
            // What was added since the form opened, to change or take back at once.
            <div className="msg-in border-t border-rule px-5 pt-2.5 pb-3">
              <div className="flex items-center gap-1.5 px-2 pb-1 text-[12px] text-ink-2" role="status">
                <CheckIcon className="size-3.5 text-good" /> {t("Added")} · {added.filter((e) => !e.deleted && !e.leaving).length}
              </div>
              <ul className="max-h-[188px] overflow-y-auto">
                {added.map((e) => (
                  <li key={e.key} className={cn("row-in flex h-9 items-center gap-2.5 overflow-hidden rounded-[8px] px-2 text-[13.5px] transition-colors duration-300 hover:bg-wash", e.deleted && "struck text-muted-foreground", e.leaving && "row-out")}>
                    <span className="min-w-0 flex-1 truncate font-medium">
                      <span className="strike">{e.name}</span>
                    </span>
                    {e.type && <Tag name={e.type} color={optionColor(options, "type", e.type)} className="strike !shadow-none !bg-transparent !px-0 text-[12.5px] text-ink-2 max-sm:hidden" />}
                    <span className="strike text-[12.5px] text-ink-2 tabular-nums">{dateLabel(e.date, { month: "short", day: "numeric" })}</span>
                    <span className="w-24 text-right">
                      <span className={cn("strike tabular-nums", e.amount < 0 && !e.deleted && "text-red")}>{money(e.amount, ledger.currency)}</span>
                    </span>
                    {e.deleted ? (
                      <button type="button" className="flex h-7 w-[60px] shrink-0 items-center justify-center gap-1 rounded-md text-[12.5px] text-ink no-underline hover:bg-sheet" style={{ textDecoration: "none" }} aria-label={t("Undo deleting {name}", { name: e.name })} onClick={() => void restoreAdded(e)}>
                        <Undo2Icon className="size-3.5" /> {t("Undo")}
                      </button>
                    ) : (
                      <>
                        <button type="button" className="grid size-7 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-sheet hover:text-ink" aria-label={t("Edit {name}", { name: e.name })} title={t("Edit")} onClick={() => setEditing(e)}>
                          <PencilIcon className="size-3.5" />
                        </button>
                        <button type="button" className="grid size-7 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-sheet hover:text-red" aria-label={t("Delete {name}", { name: e.name })} title={t("Delete")} onClick={() => void removeAdded(e)}>
                          <Trash2Icon className="size-3.5" />
                        </button>
                      </>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          )}
        </form>
        {!entry && !refund && (
          <EntryDialog
            open={!!editing}
            entry={editing}
            onOpenChange={(o) => !o && setEditing(null)}
            onSaved={(saved) => {
              const was = editing
              if (saved) setAdded((a) => a.map((x) => (x.id === saved.id ? { ...saved, key: x.key } : x)))
              else if (was) {
                setAdded((a) => a.map((x) => (x.id === was.id ? { ...x, leaving: true } : x)))
                setTimeout(() => setAdded((a) => a.filter((x) => x.id !== was.id)), 200)
              }
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}
