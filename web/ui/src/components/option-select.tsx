import { useMemo, useRef, useState, type KeyboardEvent } from "react"
import { CheckIcon } from "lucide-react"

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import type { Option } from "@/lib/api"
import { matches } from "@/lib/search"
import { cn } from "@/lib/utils"
import { t } from "@/lib/i18n"

/** A type or card: its color's dot and its name, in an outlined pill. */
export function Tag({ name, color, className }: { name: string; color: string; className?: string }) {
  return (
    <span className={cn("tag", className)} data-color={color} title={name}>
      <span className="dot" />
      <span>{name}</span>
    </span>
  )
}

/**
 * A dropdown cell for a type or card: pick an option, clear it, or type a new
 * name (which the server makes an option when the entry is saved).
 */
export function OptionSelect({
  kind,
  value,
  options,
  onChange,
  className,
  label,
  placeholder = "",
}: {
  placeholder?: string
  kind: "type" | "card"
  value: string
  options: Option[]
  onChange: (name: string) => void
  className?: string
  label: string
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)

  const current = options.find((o) => o.name.toLowerCase() === value.toLowerCase())
  const q = query.trim().toLowerCase()
  const found = useMemo(() => options.filter((o) => matches(o.name, q)), [options, q])
  const canCreate = q !== "" && !options.some((o) => o.name.toLowerCase() === q)
  const rows: ({ t: "opt"; o: Option } | { t: "create" } | { t: "none" })[] = [
    ...found.map((o) => ({ t: "opt" as const, o })),
    ...(canCreate ? [{ t: "create" as const }] : []),
    ...(value && !q ? [{ t: "none" as const }] : []),
  ]

  const pick = (name: string) => {
    setOpen(false)
    setQuery("")
    if (name !== value) onChange(name)
  }
  const choose = (i: number) => {
    const r = rows[i]
    if (!r) return
    if (r.t === "opt") pick(r.o.name)
    else if (r.t === "create") pick(query.trim())
    else pick("")
  }
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault()
      setActive((a) => Math.min(a + 1, rows.length - 1))
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      setActive((a) => Math.max(a - 1, 0))
    } else if (e.key === "Enter") {
      e.preventDefault()
      choose(active)
    }
  }

  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        setQuery("")
        setActive(0)
      }}
    >
      <PopoverTrigger asChild>
        <button type="button" className={cn("cell", className)} aria-label={`${label}: ${value || t("none")}`}>
          {value ? <Tag name={value} color={current?.color ?? "gray"} className="!shadow-none !bg-transparent !px-0" /> : <span className="text-muted-foreground/70">{placeholder}</span>}
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        sideOffset={2}
        className="w-64 gap-0 overflow-hidden rounded-[14px] border-0 p-0 shadow-[0_16px_48px_rgb(0_0_0/.18),0_0_0_1px_var(--rule-2)] ring-0"
        onOpenAutoFocus={(e) => {
          e.preventDefault()
          inputRef.current?.focus()
        }}
      >
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => {
            setQuery(e.target.value)
            setActive(0)
          }}
          onKeyDown={onKey}
          placeholder={t("Search or create")}
          className="h-10 w-full border-b border-rule bg-transparent px-3.5 text-[14px] outline-none placeholder:text-muted-foreground"
          maxLength={40}
        />
        <div className="max-h-64 overflow-y-auto py-1" role="listbox" aria-label={label}>
                    {rows.length === 0 && <p className="px-3 py-2 text-[13px] text-muted-foreground">{kind === "type" ? t("No types yet. Type a name.") : t("No cards yet. Type a name.")}</p>}
          {rows.map((r, i) => (
            <button
              key={r.t === "opt" ? r.o.id : r.t}
              type="button"
              role="option"
              aria-selected={i === active}
              onMouseEnter={() => setActive(i)}
              onClick={() => choose(i)}
              className={cn("mx-1 flex h-9 w-[calc(100%-8px)] items-center gap-2 rounded-[8px] px-2.5 text-left text-[14px]", i === active && "bg-wash")}
            >
              {r.t === "opt" && (
                <>
                  <span className="flex flex-1 items-center gap-2" data-color={r.o.color}><span className="dot" />{r.o.name}</span>
                  {r.o.name.toLowerCase() === value.toLowerCase() && <CheckIcon className="size-4 text-ink" />}
                </>
              )}
              {r.t === "create" && (
                <span className="truncate">
                  {t("Create “{name}”", { name: query.trim() })}
                </span>
              )}
              {r.t === "none" && <span className="text-muted-foreground">{t("Clear")}</span>}
            </button>
          ))}
        </div>
      </PopoverContent>
    </Popover>
  )
}
