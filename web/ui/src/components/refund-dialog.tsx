import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent } from "react"
import { Undo2Icon } from "lucide-react"
import { toast } from "sonner"

import { parseAmount } from "@/components/entry-dialog"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Loading, Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { api, dateLabel, money, type Entry } from "@/lib/api"
import { t } from "@/lib/i18n"
import { useApp } from "@/lib/state"
import { cn } from "@/lib/utils"

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

/**
 * A refund (R): type what came back. The server decides, by the same rule as
 * the MCP refund tool: a matching purchase from the last four months is
 * deleted (the newest unless another is picked); with no match, the refund is
 * added as a negative amount today.
 */
export function RefundDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { user, bumpTable, reloadOptions } = useApp()
  const currency = user.prefs.currency
  const [text, setText] = useState("")
  // The purchases matching what's typed, and the text they're for (null until the first answer).
  const [found, setFound] = useState<{ key: string; matches: Entry[] } | null>(null)
  const [active, setActive] = useState(0)
  const [busy, setBusy] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!open) return
    setText("")
    setActive(0)
    setFound(null)
  }, [open])

  const { name, amount } = parseRefund(text)
  const empty = !name && amount === null
  const key = `${name.toLowerCase()}|${amount ?? ""}`
  useEffect(() => {
    if (!open || empty) return
    let live = true
    const timer = setTimeout(() => {
      api
        .refundMatches(name, amount)
        .then((matches) => {
          if (!live) return
          setFound({ key, matches })
          setActive(0)
        })
        .catch((e) => toast.error((e as Error).message))
    }, 120)
    return () => {
      live = false
      clearTimeout(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, key])

  // While the answer for this text is on its way, the last one stays on screen, but nothing can be done with it.
  const ready = found?.key === key
  const matches = found?.matches ?? []
  const picked = matches[Math.min(active, matches.length - 1)]
  const canAdd = ready && !picked && amount !== null

  const done = () => {
    bumpTable()
    void reloadOptions()
    onOpenChange(false)
  }

  const submit = async (e?: FormEvent) => {
    e?.preventDefault()
    if (busy || !ready || (!picked && !canAdd)) return
    setBusy(true)
    try {
      const res = await api.refund(picked ? { id: picked.id } : { name, amount: amount ?? undefined })
      if (res.matches) {
        // Something changed in the meantime: show what matches now.
        setFound({ key, matches: res.matches })
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
        toast.success(t("Added a refund of {amount}.", { amount: money(Math.abs(res.added.amount), currency) }))
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
              placeholder={t("What came back?")}
              aria-label={t("What was refunded")}
              autoComplete="off"
              maxLength={200}
              className="mt-2 w-full bg-transparent text-[22px] leading-tight font-semibold outline-none placeholder:text-muted-foreground/50"
            />
          </div>

          <div className="min-h-[148px] px-5 pt-4 pb-4">
            {empty ? null : found === null ? (
              <Loading className="grid gap-3.5 px-2.5 pt-3">
                {[52, 36, 60].map((w, i) => (
                  <span key={i} className="flex items-center gap-3">
                    <Skeleton style={{ width: `${w}%` }} />
                    <Skeleton className="ml-auto w-12" />
                    <Skeleton className="w-16" />
                  </span>
                ))}
              </Loading>
            ) : picked ? (
              <>
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
                <div className="flex h-10 items-center gap-3 rounded-[10px] bg-good-wash px-2.5 text-[14px]">
                  <span className="min-w-0 flex-1 truncate font-medium">{name || t("Refund")}</span>
                  <span className="text-[12.5px] text-ink-2">{t("Today")}</span>
                  <span className="w-24 text-right font-medium text-good tabular-nums">{money(-amount!, currency)}</span>
                </div>
              </>
            ) : (
              <p className="px-2 pt-1 text-[13px] text-ink-2">{t("No purchase matches.")}</p>
            )}
          </div>

          <div className="flex items-center gap-1.5 border-t border-rule px-5 py-3.5">
            <span className="flex-1" />
            <button type="button" className="btn ghost" onClick={() => onOpenChange(false)}>
              {t("Cancel")}
            </button>
            <button type="submit" className={cn("btn", picked && "danger")} disabled={busy || !ready || (!picked && !canAdd)}>
              {busy && <Spinner className="size-3.5" />}
              {picked ? t("Delete purchase") : t("Add refund")}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
