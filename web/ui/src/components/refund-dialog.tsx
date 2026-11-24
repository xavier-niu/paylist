import { useEffect, useMemo, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { Undo2Icon } from "lucide-react"
import { toast } from "sonner"

import { parseAmount } from "@/components/entry-dialog"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Spinner } from "@/components/ui/spinner"
import { api, dateLabel, money, today, type Entry } from "@/lib/api"
import { t } from "@/lib/i18n"
import { useApp } from "@/lib/state"
import { cn } from "@/lib/utils"

/** How far back a refund looks for the purchase it undoes. */
const LOOKBACK_DAYS = 120
/** How many matches the dialog lists to pick from. */
const MAX_MATCHES = 5

/** "coffee 19.77" → { name: "coffee", amount: 19.77 }; either can be missing. */
export function parseRefund(text: string): { name: string; amount: number | null } {
  // The amount is a number standing on its own (¥19.77, 1,200, 35), the last one if there are several.
  const re = /(?:^|\s)([-−]?[¥$€£]?\d[\d,]*(?:\.\d+)?)(?=\s|$)/g
  let last: RegExpExecArray | null = null
  for (let m = re.exec(text); m; m = re.exec(text)) last = m
  if (!last) return { name: text.trim(), amount: null }
  const n = Math.abs(parseAmount(last[1]))
  const name = (text.slice(0, last.index) + " " + text.slice(last.index + last[0].length)).replace(/\s+/g, " ").trim()
  return { name, amount: Number.isFinite(n) && n > 0 ? n : null }
}

/** Entries that fit everything given (the name holds the text, the amount is the same to the cent), newest first. */
export function matchRefund(entries: Entry[], name: string, amount: number | null): Entry[] {
  if (!name && amount === null) return []
  const q = name.toLowerCase()
  return entries
    .filter((e) => e.amount > 0)
    .filter((e) => (!q || e.name.toLowerCase().includes(q)) && (amount === null || Math.round(e.amount * 100) === Math.round(amount * 100)))
    .sort((a, b) => b.date.localeCompare(a.date) || b.id - a.id)
}

/**
 * A refund (R): type what came back. If it matches a purchase, that entry is
 * deleted (one of them, the newest unless another is picked); if nothing
 * matches, the refund is added as a negative amount today.
 */
export function RefundDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { user, bumpTable, reloadOptions } = useApp()
  const currency = user.prefs.currency
  const [text, setText] = useState("")
  const [recent, setRecent] = useState<Entry[] | null>(null)
  const [active, setActive] = useState(0)
  const [busy, setBusy] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    setText("")
    setActive(0)
    setRecent(null)
    const from = new Date()
    from.setDate(from.getDate() - LOOKBACK_DAYS)
    api
      .entriesBetween(today(from), today())
      .then((r) => setRecent(r.entries))
      .catch((e) => {
        toast.error((e as Error).message)
        setRecent([])
      })
  }, [open])

  const { name, amount } = parseRefund(text)
  const matches = useMemo(() => (recent ? matchRefund(recent, name, amount).slice(0, MAX_MATCHES) : []), [recent, name, amount])
  const picked = matches[Math.min(active, matches.length - 1)]
  const canAdd = !picked && amount !== null && recent !== null

  const done = () => {
    bumpTable()
    void reloadOptions()
    onOpenChange(false)
  }

  const submit = async (e?: FormEvent) => {
    e?.preventDefault()
    if (busy || (!picked && !canAdd)) return
    setBusy(true)
    try {
      if (picked) {
        await api.deleteEntries([picked.id])
        const { name, type, amount, date, card } = picked
        toast.success(t("Refunded: deleted {name}.", { name }), {
          action: {
            label: t("Undo"),
            onClick: () => {
              api.createEntry({ name, type, amount, date, card }).then(bumpTable, (err) => toast.error((err as Error).message))
            },
          },
        })
      } else if (amount !== null) {
        const added = await api.createEntry({ name: name || t("Refund"), amount: -amount, date: today() })
        toast.success(t("Added a refund of {amount}.", { amount: money(Math.abs(added.amount), currency) }))
      }
      done()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const onKey = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown" && matches.length) {
      e.preventDefault()
      setActive((a) => Math.min(a + 1, matches.length - 1))
    } else if (e.key === "ArrowUp" && matches.length) {
      e.preventDefault()
      setActive((a) => Math.max(a - 1, 0))
    }
  }

  const empty = !name && amount === null
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        // Anchored near the top, like a command palette, so it doesn't jump as the matches change.
        className="top-[14vh] translate-y-0 gap-0 overflow-hidden rounded-[20px] border-0 bg-sheet p-0 shadow-[0_16px_48px_rgb(0_0_0/.3),0_0_0_1px_var(--rule-2)] ring-0 sm:max-w-[500px]"
        onOpenAutoFocus={(e) => {
          e.preventDefault()
          inputRef.current?.focus()
        }}
      >
        <form onSubmit={submit}>
          <div className="px-7 pt-6">
            <DialogTitle className="flex items-center gap-2 text-[13px] font-normal text-ink-2">
              <Undo2Icon className="size-4" /> {t("Refund")}
            </DialogTitle>
            <DialogDescription className="sr-only">{t("A matching purchase is deleted; otherwise the refund is added as a negative amount.")}</DialogDescription>
            <input
              ref={inputRef}
              id="refund-text"
              value={text}
              onChange={(e) => {
                setText(e.target.value)
                setActive(0)
              }}
              onKeyDown={onKey}
              placeholder={t("What came back? Coffee 19.77")}
              aria-label={t("What was refunded")}
              autoComplete="off"
              maxLength={200}
              className="mt-2 w-full bg-transparent text-[22px] leading-tight font-semibold outline-none placeholder:text-muted-foreground/50"
            />
          </div>

          <div className="min-h-[148px] px-5 pt-4 pb-4">
            {recent === null ? (
              <Spinner className="mx-auto mt-10 text-muted-foreground" />
            ) : empty ? (
              <p className="px-2 pt-1 text-[13px] text-ink-2">{t("Type a name, an amount, or both. A matching purchase from the last four months is deleted; with no match, the refund is added as a negative amount.")}</p>
            ) : picked ? (
              <>
                <div className="px-2 pb-1.5 text-[12px] text-ink-2">{matches.length === 1 ? t("Matches this purchase. It will be deleted.") : t("Matches these purchases. The one picked will be deleted.")}</div>
                <ul role="listbox" aria-label={t("Matching purchases")} className="grid gap-0.5">
                  {matches.map((e, i) => (
                    <li key={e.id}>
                      <button
                        type="button"
                        role="option"
                        aria-selected={e === picked}
                        onMouseEnter={() => setActive(i)}
                        onClick={() => {
                          setActive(i)
                          inputRef.current?.focus()
                        }}
                        className={cn("flex h-10 w-full items-center gap-3 rounded-[10px] px-2.5 text-left text-[14px]", e === picked ? "bg-red-wash" : "hover:bg-wash")}
                      >
                        <span className={cn("min-w-0 flex-1 truncate", e === picked && "font-medium text-red line-through decoration-1")}>{e.name}</span>
                        <span className="text-[12.5px] text-ink-2 tabular-nums">{dateLabel(e.date, { month: "short", day: "numeric" })}</span>
                        <span className={cn("w-24 text-right tabular-nums", e === picked && "text-red")}>{money(e.amount, currency)}</span>
                      </button>
                    </li>
                  ))}
                </ul>
              </>
            ) : canAdd ? (
              <>
                <div className="px-2 pb-1.5 text-[12px] text-ink-2">{t("No purchase matches. It will be added as a refund.")}</div>
                <div className="flex h-10 items-center gap-3 rounded-[10px] bg-good-wash px-2.5 text-[14px]">
                  <span className="min-w-0 flex-1 truncate font-medium">{name || t("Refund")}</span>
                  <span className="text-[12.5px] text-ink-2">{t("Today")}</span>
                  <span className="w-24 text-right font-medium text-good tabular-nums">{money(-amount!, currency)}</span>
                </div>
              </>
            ) : (
              <p className="px-2 pt-1 text-[13px] text-ink-2">{t("No purchase matches. Add the amount to record it as a refund.")}</p>
            )}
          </div>

          <div className="flex items-center gap-1.5 border-t border-rule px-5 py-3.5">
            <span className="flex-1 text-[12px] text-muted-foreground">{matches.length > 1 ? t("↑↓ to pick, Enter to confirm") : t("Enter to confirm")}</span>
            <button type="button" className="btn ghost" onClick={() => onOpenChange(false)}>
              {t("Cancel")}
            </button>
            <button type="submit" className={cn("btn", picked && "danger")} disabled={busy || (!picked && !canAdd)}>
              {busy && <Spinner className="size-3.5" />}
              {picked ? t("Delete purchase") : t("Add refund")}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
