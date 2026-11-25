import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react"
import { LogOutIcon, Trash2Icon, XIcon } from "lucide-react"
import { toast } from "sonner"

import { useConfirm } from "@/components/confirm"
import { LedgerOptions } from "@/components/ledger-options"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { api, type Ledger, type LedgerMember } from "@/lib/api"
import { CURRENCIES } from "@/lib/currencies"
import { t } from "@/lib/i18n"
import { useApp } from "@/lib/state"

const roleNames = () => ({ owner: t("Owner"), editor: t("Editor"), viewer: t("Viewer") })

/**
 * Deleting a ledger (its owner) or leaving it (anyone else), once asked;
 * returns the ask and the confirmation to render.
 */
export function useRemoveLedger(): [(l: Ledger) => void, ReactNode] {
  const { user, reloadLedgers } = useApp()
  const [ask, confirmEl] = useConfirm()
  const remove = (l: Ledger) =>
    l.role === "owner"
      ? ask({
          title: t("Delete the ledger “{name}”?", { name: l.name }),
          body: t("Its entries, types and cards are deleted for good, for everyone it's shared with."),
          action: t("Delete ledger"),
          run: async () => {
            try {
              await api.deleteLedger(l.id)
              await reloadLedgers()
            } catch (e) {
              toast.error((e as Error).message)
            }
          },
        })
      : ask({
          title: t("Leave “{name}”?", { name: l.name }),
          body: t("You'll no longer see it, until its owner shares it with you again."),
          action: t("Leave"),
          run: async () => {
            try {
              await api.removeLedgerMember(l.id, user.id)
              await reloadLedgers()
            } catch (e) {
              toast.error((e as Error).message)
            }
          },
        })
  return [remove, confirmEl]
}

function Field({ label, htmlFor, children }: { label: string; htmlFor?: string; children: ReactNode }) {
  return (
    <div className="grid content-start gap-1">
      <label htmlFor={htmlFor} className="label">{label}</label>
      {children}
    </div>
  )
}

/** A ledger's settings, in a window: its name and currency, and the people who have it. */
export function LedgerSettingsDialog({ id, onClose }: { id: number | null; onClose: () => void }) {
  const { ledgers } = useApp()
  const ledger = ledgers.find((l) => l.id === id)
  // Gone (deleted, or left) while open.
  useEffect(() => {
    if (id !== null && !ledger) onClose()
  }, [id, ledger, onClose])
  return (
    <Dialog open={!!ledger} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 overflow-hidden rounded-[20px] border border-rule-2 bg-sheet p-0 shadow-[0_16px_48px_rgb(0_0_0/.3)] ring-0 sm:max-w-[680px]">
        {ledger && <LedgerSettings key={ledger.id} ledger={ledger} />}
      </DialogContent>
    </Dialog>
  )
}

function LedgerSettings({ ledger }: { ledger: Ledger }) {
  const { user, reloadLedgers } = useApp()
  const owner = ledger.role === "owner"
  const [name, setName] = useState(ledger.name)
  useEffect(() => setName(ledger.name), [ledger.name])
  const [members, setMembers] = useState<LedgerMember[] | null>(null)
  const [email, setEmail] = useState("")
  const [role, setRole] = useState<"editor" | "viewer">("editor")
  const [ask, confirmEl] = useConfirm()
  const [remove, removeEl] = useRemoveLedger()
  const loadMembers = useCallback(() => {
    api.ledgerMembers(ledger.id).then(setMembers).catch((e) => toast.error((e as Error).message))
  }, [ledger.id])
  useEffect(loadMembers, [loadMembers])

  const change = async (c: { name?: string; currency?: string }) => {
    try {
      await api.updateLedger(ledger.id, c)
      await reloadLedgers()
    } catch (e) {
      toast.error((e as Error).message)
      setName(ledger.name)
    }
  }
  const invite = async (e: FormEvent) => {
    e.preventDefault()
    try {
      const m = await api.addLedgerMember(ledger.id, email.trim(), role)
      setEmail("")
      toast.success(t("Shared with {name}.", { name: m.name || m.email }))
      loadMembers()
      void reloadLedgers()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }
  const currencies = CURRENCIES.includes(ledger.currency) ? CURRENCIES : [ledger.currency, ...CURRENCIES]
  const roles = roleNames()
  return (
    <>
      <div className="shrink-0 border-b border-rule px-6 pt-5 pb-4 pr-14">
        <DialogTitle className="truncate text-[17px] font-semibold">{t("Ledger settings")}</DialogTitle>
        <DialogDescription className="sr-only">{t("A ledger's name, currency, types and cards, and the people who have it.")}</DialogDescription>
      </div>
      <div className="grid min-h-0 flex-1 gap-6 overflow-y-auto overscroll-contain px-6 pt-5 pb-6">
        <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_150px]">
          <Field label={t("Name")} htmlFor="ledger-name-edit">
            <input
              id="ledger-name-edit"
              className="field"
              value={name}
              disabled={!owner}
              maxLength={60}
              onChange={(e) => setName(e.target.value)}
              onBlur={() => name.trim() && name !== ledger.name && void change({ name })}
              onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
            />
          </Field>
          <Field label={t("Currency")} htmlFor="ledger-currency-edit">
            <select id="ledger-currency-edit" className="field" value={ledger.currency} disabled={!owner} onChange={(e) => void change({ currency: e.target.value })}>
              {currencies.map((c) => (
                <option key={c} value={c}>{c}</option>
              ))}
            </select>
          </Field>
        </div>

        <div className="grid gap-1">
          <span className="label">{t("People")}</span>
          <ul className="grid">
            {(members ?? []).map((m) => (
              <li key={m.user_id} className="flex min-h-11 items-center gap-3 border-b border-rule py-1.5 last:border-b-0">
                <span className="grid min-w-0 flex-1">
                  <span className="truncate text-[14px] font-medium">
                    {m.name || m.email}
                    {m.user_id === user.id && <span className="ml-1.5 text-[12px] font-normal text-muted-foreground">{t("(you)")}</span>}
                  </span>
                  {m.name && <span className="truncate text-[12.5px] text-muted-foreground">{m.email}</span>}
                </span>
                {owner && m.role !== "owner" ? (
                  <select
                    className="field !h-8 !w-28"
                    value={m.role}
                    aria-label={t("Role of {name}", { name: m.name || m.email })}
                    onChange={async (e) => {
                      try {
                        await api.setLedgerMemberRole(ledger.id, m.user_id, e.target.value as "editor" | "viewer")
                        loadMembers()
                      } catch (err) {
                        toast.error((err as Error).message)
                      }
                    }}
                  >
                    <option value="editor">{roles.editor}</option>
                    <option value="viewer">{roles.viewer}</option>
                  </select>
                ) : (
                  <span className="w-28 text-right text-[13px] text-ink-2">{roles[m.role]}</span>
                )}
                {owner && m.role !== "owner" ? (
                  <button
                    type="button"
                    className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-wash hover:text-red"
                    aria-label={t("Remove {name}", { name: m.name || m.email })}
                    title={t("Remove {name}", { name: m.name || m.email })}
                    onClick={() =>
                      ask({
                        title: t("Stop sharing with {name}?", { name: m.name || m.email }),
                        body: t("They lose access to this ledger at once."),
                        action: t("Remove"),
                        run: async () => {
                          await api.removeLedgerMember(ledger.id, m.user_id).catch((e) => toast.error((e as Error).message))
                          loadMembers()
                          void reloadLedgers()
                        },
                      })
                    }
                  >
                    <XIcon className="size-3.5" />
                  </button>
                ) : (
                  <span className="w-7" />
                )}
              </li>
            ))}
          </ul>
        </div>

        {owner && (
          <form onSubmit={invite} className="grid gap-1">
            <label htmlFor="invite-email" className="label">{t("Invite someone")}</label>
            <div className="flex flex-wrap gap-2">
              <input
                id="invite-email"
                className="field min-w-48 flex-1"
                type="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder={t("Their Paylist account's email")}
              />
              <select className="field !w-28" value={role} onChange={(e) => setRole(e.target.value as "editor" | "viewer")} aria-label={t("Role")}>
                <option value="editor">{roles.editor}</option>
                <option value="viewer">{roles.viewer}</option>
              </select>
              <button type="submit" className="btn !h-[34px]" disabled={!email.trim()}>
                {t("Invite")}
              </button>
            </div>
          </form>
        )}

        <div className="grid gap-1">
          <span className="label">{t("Types and cards")}</span>
          <LedgerOptions ledger={ledger} />
        </div>

        <div className="border-t border-rule pt-5">
          <button type="button" className={owner ? "btn danger" : "btn quiet"} onClick={() => remove(ledger)}>
            {owner ? <Trash2Icon /> : <LogOutIcon />} {owner ? t("Delete ledger") : t("Leave ledger")}
          </button>
        </div>
      </div>
      {confirmEl}
      {removeEl}
    </>
  )
}

