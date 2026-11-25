import { useEffect, useState, type FormEvent } from "react"
import { toast } from "sonner"

import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Spinner } from "@/components/ui/spinner"
import { api, type Ledger } from "@/lib/api"
import { CURRENCIES } from "@/lib/currencies"
import { t } from "@/lib/i18n"

/** Making a new ledger: its name and currency. */
export function NewLedgerDialog({ open, onOpenChange, currency, onCreated }: { open: boolean; onOpenChange: (o: boolean) => void; currency: string; onCreated: (l: Ledger) => void }) {
  const [name, setName] = useState("")
  const [cur, setCur] = useState(currency)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    if (!open) return
    setName("")
    setCur(currency)
  }, [open, currency])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim() || busy) return
    setBusy(true)
    try {
      const l = await api.createLedger(name.trim(), cur)
      onOpenChange(false)
      onCreated(l)
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const currencies = CURRENCIES.includes(currency) ? CURRENCIES : [currency, ...CURRENCIES]
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="gap-0 overflow-hidden rounded-[20px] border border-rule-2 bg-sheet p-0 shadow-[0_16px_48px_rgb(0_0_0/.3)] ring-0 sm:max-w-[420px]">
        <form onSubmit={submit}>
          <div className="grid gap-4 px-7 pt-6 pb-6">
            <DialogTitle className="text-[13px] font-normal text-ink-2">{t("New ledger")}</DialogTitle>
            <DialogDescription className="sr-only">{t("A ledger's name and currency.")}</DialogDescription>
            <input
              id="ledger-name"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("Name it, such as Home or Japan trip")}
              maxLength={60}
              aria-label={t("Name")}
              className="w-full bg-transparent text-[22px] leading-tight font-semibold outline-none placeholder:text-muted-foreground/50"
            />
            <label className="grid grid-cols-[120px_minmax(0,1fr)] items-center gap-2 text-[14px]">
              <span className="text-muted-foreground">{t("Currency")}</span>
              <select id="ledger-currency" className="field !h-8" value={cur} onChange={(e) => setCur(e.target.value)}>
                {currencies.map((c) => (
                  <option key={c} value={c}>{c}</option>
                ))}
              </select>
            </label>
          </div>
          <div className="flex items-center justify-end gap-1.5 border-t border-rule px-5 py-3.5">
            <button type="button" className="btn ghost" onClick={() => onOpenChange(false)}>
              {t("Cancel")}
            </button>
            <button type="submit" className="btn" disabled={!name.trim() || busy}>
              {busy && <Spinner className="size-3.5" />}
              {t("Create")}
            </button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
