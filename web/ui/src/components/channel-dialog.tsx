import { useEffect, useState, type FormEvent } from "react"
import { PlusIcon, XIcon } from "lucide-react"
import { toast } from "sonner"

import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { api, type Channel } from "@/lib/api"
import { t } from "@/lib/i18n"

export const CHANNEL_KINDS = ["webhook", "slack", "discord", "feishu", "dingtalk", "wecom", "bark", "ntfy"]

export const kindName = (k: string): string =>
  ({
    webhook: "Webhook",
    slack: "Slack",
    discord: "Discord",
    feishu: t("Feishu"),
    dingtalk: t("DingTalk"),
    wecom: t("WeCom"),
    bark: "Bark",
    ntfy: "ntfy",
  })[k] ?? k

const PLACEHOLDERS: Record<string, string> = {
  webhook: "https://",
  slack: "https://hooks.slack.com/services/…",
  discord: "https://discord.com/api/webhooks/…",
  feishu: "https://open.feishu.cn/open-apis/bot/v2/hook/…",
  dingtalk: "https://oapi.dingtalk.com/robot/send?access_token=…",
  wecom: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…",
  bark: "https://api.day.app/…",
  ntfy: "https://ntfy.sh/…",
}

const takesHeaders = (k: string) => k === "webhook" || k === "ntfy"

type Row = { name: string; value: string; kept: boolean }

/** Adding a channel (channel null), or changing one. */
export function ChannelDialog({ open, channel, onClose, onSaved }: { open: boolean; channel: Channel | null; onClose: () => void; onSaved: (c: Channel) => void }) {
  const [kind, setKind] = useState("webhook")
  const [name, setName] = useState("")
  const [url, setUrl] = useState("")
  const [rows, setRows] = useState<Row[]>([])
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) return
    setKind(channel?.kind ?? "webhook")
    setName(channel?.name ?? "")
    setUrl(channel?.url ?? "")
    setRows(channel?.headers.map((h) => ({ name: h, value: "", kept: true })) ?? [])
  }, [open, channel])

  const set = (i: number, r: Partial<Row>) => setRows((rs) => rs.map((x, j) => (j === i ? { ...x, ...r } : x)))
  const save = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const input = { kind, name, url, headers: takesHeaders(kind) ? rows.filter((r) => r.name.trim()).map(({ name, value }) => ({ name, value })) : [] }
      onSaved(channel ? await api.updateChannel(channel.id, input) : await api.addChannel(input))
      onClose()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="gap-0 rounded-[20px] border border-rule-2 bg-sheet p-0 shadow-[0_16px_48px_rgb(0_0_0/.3)] ring-0 sm:max-w-[520px]">
        <form onSubmit={save}>
          <div className="border-b border-rule py-3.5 pr-12 pl-5">
            <DialogTitle className="text-[16px] font-semibold">{channel ? t("Edit channel") : t("New channel")}</DialogTitle>
            <DialogDescription className="sr-only">{t("Where Paylist sends you messages.")}</DialogDescription>
          </div>
          <div className="grid gap-4 px-5 pt-4 pb-5">
            <div className="grid gap-3 sm:grid-cols-[150px_minmax(0,1fr)]">
              <label className="grid gap-1">
                <span className="label">{t("Kind")}</span>
                <select className="field" value={kind} onChange={(e) => setKind(e.target.value)}>
                  {CHANNEL_KINDS.map((k) => (
                    <option key={k} value={k}>{kindName(k)}</option>
                  ))}
                </select>
              </label>
              <label className="grid gap-1">
                <span className="label">{t("Name")}</span>
                <input className="field" value={name} maxLength={60} onChange={(e) => setName(e.target.value)} placeholder={kindName(kind)} />
              </label>
            </div>
            <label className="grid gap-1">
              <span className="label">{t("Address")}</span>
              <input className="field font-mono !text-[13px]" type="url" inputMode="url" required value={url} onChange={(e) => setUrl(e.target.value)} placeholder={PLACEHOLDERS[kind]} />
            </label>
            {takesHeaders(kind) && (
              <div className="grid gap-1">
                <span className="label">{t("Headers")}</span>
                {rows.map((r, i) => (
                  <div key={i} className="flex gap-2">
                    <input className="field !w-[40%] font-mono !text-[13px]" value={r.name} onChange={(e) => set(i, { name: e.target.value, kept: false })} placeholder="Authorization" aria-label={t("Header name")} />
                    <input
                      className="field min-w-0 flex-1 font-mono !text-[13px]"
                      type="password"
                      autoComplete="off"
                      value={r.value}
                      onChange={(e) => set(i, { value: e.target.value })}
                      placeholder={r.kept ? t("Unchanged") : "Bearer …"}
                      aria-label={t("Header value")}
                    />
                    <button type="button" className="grid size-[34px] shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-wash hover:text-red" aria-label={t("Remove")} title={t("Remove")} onClick={() => setRows((rs) => rs.filter((_, j) => j !== i))}>
                      <XIcon className="size-4" />
                    </button>
                  </div>
                ))}
                <button type="button" className="btn ghost !h-8 justify-self-start !px-2.5 text-[13px]" onClick={() => setRows((rs) => [...rs, { name: "", value: "", kept: false }])}>
                  <PlusIcon /> {t("Header")}
                </button>
              </div>
            )}
          </div>
          <div className="flex justify-end gap-2 border-t border-rule px-5 py-3">
            <button type="button" className="btn quiet" onClick={onClose}>{t("Cancel")}</button>
            <button type="submit" className="btn" disabled={busy || !url.trim()}>{t("Save")}</button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
