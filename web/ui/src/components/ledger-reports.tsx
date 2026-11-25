import { useEffect, useState } from "react"
import { PlusIcon, SendIcon } from "lucide-react"
import { toast } from "sonner"

import { ChannelDialog } from "@/components/channel-dialog"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { api, type Channel, type Ledger, type ReportPeriod, type Reports } from "@/lib/api"
import { lang, t } from "@/lib/i18n"
import { cn } from "@/lib/utils"

const PERIODS: ReportPeriod[] = ["daily", "weekly", "monthly"]

const periodName = (p: ReportPeriod) => ({ daily: t("Daily"), weekly: t("Weekly"), monthly: t("Monthly") })[p]

const when = (p: ReportPeriod) => ({ daily: t("Yesterday"), weekly: t("Last week · Mondays"), monthly: t("Last month · the 1st") })[p]

const timeZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone

/** Your reports of a ledger: which, when, in which language, on which of your channels. */
export function LedgerReports({ ledger }: { ledger: Ledger }) {
  const [reports, setReports] = useState<Reports | null>(null)
  const [channels, setChannels] = useState<Channel[] | null>(null)
  const [sending, setSending] = useState<ReportPeriod | null>(null)
  const [adding, setAdding] = useState(false)

  const load = () =>
    Promise.all([api.reports(ledger.id), api.channels()])
      .then(([r, c]) => {
        setReports(r)
        setChannels(c)
      })
      .catch((e) => toast.error((e as Error).message))
  useEffect(() => {
    void load()
  }, [ledger.id])

  // The language reports are written in: the one chosen, or the app's.
  const language = reports?.language || lang()
  const save = async (change: Partial<Reports>) => {
    if (!reports) return
    const next = { ...reports, timezone: timeZone(), language, ...change }
    // Turning a report on with nowhere to send it: all your channels.
    if ((change.daily || change.weekly || change.monthly) && next.channels.length === 0 && channels) next.channels = channels.map((c) => c.id)
    setReports(next)
    try {
      setReports(await api.saveReports(ledger.id, next))
    } catch (e) {
      toast.error((e as Error).message)
      void load()
    }
  }
  const send = async (p: ReportPeriod) => {
    setSending(p)
    try {
      const res = await api.sendReport(ledger.id, p, timeZone(), language)
      const failed = res.channels.filter((c) => c.last_error)
      if (failed.length) toast.error(failed.map((c) => `${c.name}: ${c.last_error}`).join("\n"))
      else toast.success(t("Sent."))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setSending(null)
    }
  }
  const toggle = (id: number) => {
    if (!reports) return
    const on = reports.channels.includes(id)
    void save({ channels: on ? reports.channels.filter((c) => c !== id) : [...reports.channels, id] })
  }

  const iconBtn = "grid size-8 shrink-0 place-items-center rounded-md text-muted-foreground hover:bg-wash hover:text-ink disabled:opacity-40"
  const row = "flex min-h-11 items-center gap-3 border-b border-rule py-1.5 pl-1.5 last:border-b-0"
  const none = !reports?.channels.length
  return (
    <div className="grid gap-4">
      <div className="grid gap-1">
        <span className="label">{t("Reports")}</span>
        <ul>
          {PERIODS.map((p) => (
            <li key={p} className={row}>
              <Switch id={`report-${p}`} checked={!!reports?.[p]} disabled={!reports} onCheckedChange={(on) => void save({ [p]: on })} />
              <label htmlFor={`report-${p}`} className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
                <span className="text-[14px] font-medium">{periodName(p)}</span>
                <span className="text-[12.5px] text-muted-foreground">{when(p)}</span>
              </label>
              <button type="button" className={iconBtn} disabled={none || sending !== null} aria-label={t("Send now")} title={t("Send now")} onClick={() => void send(p)}>
                {sending === p ? <Spinner className="size-4" /> : <SendIcon className="size-4" />}
              </button>
            </li>
          ))}
        </ul>
      </div>

      <div className="grid gap-1">
        <span className="label">{t("Delivery")}</span>
        <div>
          <div className={row}>
            <label htmlFor="report-time" className="flex-1 text-[14px]">{t("Send at")}</label>
            <input
              id="report-time"
              type="time"
              className="field !h-8 !w-[132px]"
              value={reports?.time ?? "09:00"}
              disabled={!reports}
              onChange={(e) => /^\d\d:\d\d$/.test(e.target.value) && void save({ time: e.target.value })}
            />
          </div>
          <div className={row}>
            <label htmlFor="report-language" className="flex-1 text-[14px]">{t("Language")}</label>
            <select id="report-language" className="field !h-8 !w-[132px]" value={language} disabled={!reports} onChange={(e) => void save({ language: e.target.value })}>
              <option value="en" lang="en">English</option>
              <option value="zh" lang="zh-CN">中文</option>
            </select>
          </div>
          <div className={row}>
            <label htmlFor="report-ai" className={cn("flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2", !reports?.ai_ready && "opacity-60")}>
              <span className="text-[14px]">{t("AI suggestion")}</span>
              {reports && !reports.ai_ready && <span className="text-[12.5px] text-muted-foreground">{t("No AI model")}</span>}
            </label>
            <Switch id="report-ai" checked={!!reports?.ai && reports.ai_ready} disabled={!reports?.ai_ready} onCheckedChange={(on) => void save({ ai: on })} />
          </div>
          <div className={cn(row, "items-start")}>
            <span className="flex-none pt-1.5 text-[14px]">{t("Send to")}</span>
            <div className="flex min-w-0 flex-1 flex-wrap justify-end gap-1.5 py-0.5">
              {channels?.map((c) => {
                const on = !!reports?.channels.includes(c.id)
                return (
                  <button key={c.id} type="button" aria-pressed={on} className={cn("pill !h-7 !px-3 text-[13px]", on ? "dark" : "soft")} onClick={() => toggle(c.id)} title={c.url}>
                    {c.name}
                  </button>
                )
              })}
              <button type="button" className="pill soft !h-7 !px-2.5 text-[13px]" onClick={() => setAdding(true)} aria-label={t("Add channel")} title={t("Add channel")}>
                <PlusIcon className="size-3.5" />
                {channels?.length === 0 && t("Add channel")}
              </button>
            </div>
          </div>
        </div>
      </div>
      <ChannelDialog
        open={adding}
        channel={null}
        onClose={() => setAdding(false)}
        onSaved={(c) => {
          setChannels((l) => [...(l ?? []), c])
          if (reports) void save({ channels: [...reports.channels, c.id] })
        }}
      />
    </div>
  )
}
