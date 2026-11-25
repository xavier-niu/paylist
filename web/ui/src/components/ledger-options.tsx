import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react"
import { CheckIcon, GripVerticalIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { useConfirm } from "@/components/confirm"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { api, dateLabel, type Ledger, type Option, type Options } from "@/lib/api"
import { isInactive } from "@/lib/options"
import { t, tn } from "@/lib/i18n"
import { useApp } from "@/lib/state"
import { cn } from "@/lib/utils"

/**
 * A ledger's types and cards, side by side: their colors, names and uses,
 * changed in place (unless the ledger is only viewed). Works on any of the
 * user's ledgers, not only the one open.
 */
export function LedgerOptions({ ledger }: { ledger: Ledger }) {
  const { ledger: open, reloadOptions, bumpTable } = useApp()
  const [options, setOptions] = useState<Options | null>(null)
  const load = useCallback(() => {
    api.options(ledger.id).then(setOptions).catch((e) => toast.error((e as Error).message))
  }, [ledger.id])
  useEffect(load, [load])
  const changed = () => {
    load()
    if (ledger.id === open.id) {
      void reloadOptions()
      bumpTable()
    }
  }
  if (!options) return <div className="h-24" />
  return (
    <div className="grid gap-6 sm:grid-cols-2">
      <OptionList kind="type" title={t("Types")} list={options.types} ledger={ledger} onChanged={changed} />
      <OptionList kind="card" title={t("Cards")} list={options.cards} ledger={ledger} onChanged={changed} />
    </div>
  )
}

/** "Inactive": no entry has it in the last twelve months; when it was last used, on hover. */
function InactiveMark({ o }: { o: Option }) {
  if (!isInactive(o)) return null
  return (
    <span className="rounded-full bg-wash px-2 py-0.5 text-[11.5px] text-ink-2" title={t("Last used {when}", { when: dateLabel(o.last_used!, { month: "short", day: "numeric", year: "numeric" }) })}>
      {t("Inactive")}
    </span>
  )
}

function OptionRow({ o, ledgerID, colors, onChanged, onDelete, readOnly, handle, dragging }: { o: Option; ledgerID: number; colors: string[]; onChanged: () => void; onDelete: () => void; readOnly: boolean; handle?: ReactNode; dragging?: boolean }) {
  const [name, setName] = useState(o.name)
  useEffect(() => setName(o.name), [o.name])
  const save = async (change: { name?: string; color?: string }) => {
    try {
      await api.updateOption(o.id, change, ledgerID)
      onChanged()
    } catch (e) {
      toast.error((e as Error).message)
      setName(o.name)
    }
  }
  if (readOnly) {
    return (
      <tr className="border-b border-rule" data-color={o.color}>
        <td className="w-9 py-1">
          <span className="grid size-7 place-items-center">
            <span className="dot !size-2.5" />
          </span>
        </td>
        <td className="h-9 px-1.5 py-1 text-[14px]">{o.name}</td>
        <td className="w-px py-1 pl-1 whitespace-nowrap">
          <InactiveMark o={o} />
        </td>
        <td className="figure w-14 py-1 text-right text-[12.5px] text-muted-foreground">{o.uses}</td>
        <td className="w-9" />
      </tr>
    )
  }
  return (
    <tr data-id={o.id} className={cn("group border-b border-rule", dragging && "relative z-10 bg-sheet shadow-[0_6px_20px_rgb(0_0_0/.14)]")}>
      <td className="w-6 py-1">{handle}</td>
      <td className="w-9 py-1">
        <Popover>
          <PopoverTrigger asChild>
            <button type="button" className="grid size-7 place-items-center hover:bg-wash" aria-label={t("{name}'s color: {color}. Change it", { name: o.name, color: t(o.color) })} data-color={o.color}>
              <span className="dot !size-2.5" />
            </button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-44 gap-0 rounded-[12px] border border-rule-2 p-1 shadow-[0_16px_48px_rgb(0_0_0/.18)] ring-0">
            {colors.map((c) => (
              <button key={c} type="button" onClick={() => void save({ color: c })} className="flex h-8 w-full items-center gap-2 rounded-[4px] px-2 text-[13px] hover:bg-wash" data-color={c}>
                <span className="dot" /> <span className="capitalize">{t(c)}</span>
                {c === o.color && <CheckIcon className="ml-auto size-3.5 text-blue" />}
              </button>
            ))}
          </PopoverContent>
        </Popover>
      </td>
      <td className="py-1">
        <input
          id={`option-${o.id}`}
          value={name}
          onChange={(e) => setName(e.target.value)}
          onBlur={() => name.trim() && name !== o.name && void save({ name })}
          onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
          aria-label={t("Rename {name}", { name: o.name })}
          maxLength={40}
          className="h-8 w-full min-w-0 rounded-md bg-transparent px-1.5 text-[14px] outline-none hover:bg-wash focus:bg-sheet focus:shadow-[inset_0_0_0_1.5px_var(--ink)]"
        />
      </td>
      <td className="w-px py-1 pl-1 whitespace-nowrap">
        <InactiveMark o={o} />
      </td>
      <td className="figure w-14 py-1 text-right text-[12.5px] text-muted-foreground">{o.uses}</td>
      <td className="w-9 py-1 text-right">
        <button type="button" className="grid size-7 place-items-center text-muted-foreground opacity-0 group-hover:opacity-100 hover:text-red focus-visible:opacity-100" aria-label={t("Delete {name}", { name: o.name })} onClick={onDelete}>
          <Trash2Icon className="size-3.5" />
        </button>
      </td>
    </tr>
  )
}

function OptionList({ kind, title, list, ledger, onChanged }: { kind: "type" | "card"; title: string; list: Option[]; ledger: Ledger; onChanged: () => void }) {
  const { config } = useApp()
  const canWrite = ledger.role !== "viewer"
  const [name, setName] = useState("")
  const [ask, confirmEl] = useConfirm()
  const add = async (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return
    try {
      await api.createOption(kind, name, undefined, ledger.id)
      setName("")
      onChanged()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }
  const changed = onChanged

  // The order: dragged by a row's handle (mouse or touch), or moved a place
  // with the arrow keys on it, then saved; shown as it changes, until the
  // saved list comes back.
  const [order, setOrder] = useState<Option[] | null>(null)
  useEffect(() => setOrder(null), [list])
  const rows = order ?? list
  const bodyRef = useRef<HTMLTableSectionElement>(null)
  const [dragId, setDragId] = useState<number | null>(null)
  const saveOrder = (next: Option[]) => {
    if (next.every((o, i) => o.id === list[i]?.id)) return setOrder(null)
    setOrder(next)
    api
      .reorderOptions(kind, next.map((o) => o.id), ledger.id)
      .then(onChanged)
      .catch((e) => {
        toast.error((e as Error).message)
        setOrder(null)
      })
  }
  const dragTo = (y: number) => {
    if (dragId === null || !bodyRef.current) return
    setOrder((cur) => {
      const now = cur ?? list
      const moving = now.find((o) => o.id === dragId)!
      const rest = now.filter((o) => o.id !== dragId)
      // The others' rows, in the order shown: the dragged one goes before the first whose middle is below the pointer.
      const others = [...bodyRef.current!.querySelectorAll<HTMLTableRowElement>("tr[data-id]")].filter((r) => Number(r.dataset.id) !== dragId)
      let at = others.findIndex((r) => {
        const b = r.getBoundingClientRect()
        return y < b.top + b.height / 2
      })
      if (at < 0) at = rest.length
      return [...rest.slice(0, at), moving, ...rest.slice(at)]
    })
  }
  const handle = (o: Option, i: number) => (
    <button
      type="button"
      data-handle={o.id}
      aria-label={t("Move {name}", { name: o.name })}
      title={t("Drag to reorder")}
      className="grid h-7 w-5 cursor-grab touch-none place-items-center rounded text-muted-foreground opacity-40 group-hover:opacity-100 hover:text-ink focus-visible:opacity-100 active:cursor-grabbing max-md:opacity-100"
      onPointerDown={(e) => {
        e.preventDefault()
        e.currentTarget.setPointerCapture(e.pointerId)
        setDragId(o.id)
        setOrder(rows)
      }}
      onPointerMove={(e) => dragId === o.id && dragTo(e.clientY)}
      onPointerUp={() => {
        if (dragId !== o.id) return
        setDragId(null)
        saveOrder(order ?? list)
      }}
      onPointerCancel={() => {
        setDragId(null)
        setOrder(null)
      }}
      onKeyDown={(e) => {
        const by = e.key === "ArrowUp" ? -1 : e.key === "ArrowDown" ? 1 : 0
        if (!by || i + by < 0 || i + by >= rows.length) return
        e.preventDefault()
        const next = [...rows]
        ;[next[i], next[i + by]] = [next[i + by], next[i]]
        saveOrder(next)
        requestAnimationFrame(() => bodyRef.current?.querySelector<HTMLButtonElement>(`button[data-handle="${o.id}"]`)?.focus())
      }}
    >
      <GripVerticalIcon className="size-3.5" />
    </button>
  )

  return (
    <div className="min-w-0">
      <table className="w-full">
        <thead>
          <tr className="border-b border-rule">
            {canWrite && <th className="w-6" />}
            <th className="label py-1.5 text-left font-normal" colSpan={3}>{title}</th>
            <th className="label py-1.5 text-right font-normal">{t("Used")}</th>
            <th />
          </tr>
        </thead>
        <tbody ref={bodyRef}>
          {rows.map((o, i) => (
            <OptionRow
              key={o.id}
              o={o}
              ledgerID={ledger.id}
              colors={config.colors}
              readOnly={!canWrite}
              handle={canWrite ? handle(o, i) : undefined}
              dragging={dragId === o.id}
              onChanged={changed}
              onDelete={() =>
                ask({
                  title: kind === "type" ? t("Delete the type {name}?", { name: o.name }) : t("Delete the card {name}?", { name: o.name }),
                  body: !o.uses
                    ? t("No entry uses it.")
                    : kind === "type"
                      ? tn(o.uses, "{n} entry has it, and will have no type afterwards.", "{n} entries have it, and will have no type afterwards.")
                      : tn(o.uses, "{n} entry has it, and will have no card afterwards.", "{n} entries have it, and will have no card afterwards."),
                  action: t("Delete"),
                  run: async () => {
                    await api.deleteOption(o.id, ledger.id).catch((e) => toast.error((e as Error).message))
                    changed()
                  },
                })
              }
            />
          ))}
        </tbody>
      </table>
      <form onSubmit={add} className={cn("mt-2 flex gap-2", !canWrite && "hidden")}>
        <input id={`new-${kind}-${ledger.id}`} className="field !h-8" value={name} onChange={(e) => setName(e.target.value)} placeholder={kind === "type" ? t("Add a type") : t("Add a card")} maxLength={40} aria-label={kind === "type" ? t("New type") : t("New card")} />
        <button type="submit" className="btn quiet" disabled={!name.trim()}>{t("Add")}</button>
      </form>
      {confirmEl}
    </div>
  )
}
