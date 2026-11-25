import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react"
import { CheckIcon } from "lucide-react"

import { useConfirm } from "@/components/confirm"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { dateLabel, type Option } from "@/lib/api"
import { isInactive, sharesWord } from "@/lib/options"
import { matches } from "@/lib/search"
import { cn } from "@/lib/utils"
import { t } from "@/lib/i18n"

/** Moves the focus to the next (or, with -1, the previous) field after el, within its window or the page. */
function focusNext(el: HTMLElement, by: 1 | -1) {
  const scope = el.closest<HTMLElement>("[role=dialog]") ?? document.body
  const all = [...scope.querySelectorAll<HTMLElement>("a[href], button:not([disabled]), input:not([disabled]):not([type=hidden]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])")].filter(
    (x) => x.offsetParent !== null || x === el,
  )
  const i = all.indexOf(el)
  all[i + by]?.focus()
}

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
 * name (which the server makes an option when the entry is saved). Options
 * unused for twelve months (inactive) stay out of the list until a search finds them,
 * after the others, so they're used again rather than made anew.
 */
export function OptionSelect({
  kind,
  value,
  options,
  onChange,
  className,
  label,
  placeholder = "",
  openOnFocus = true,
}: {
  /** Open the list when the field is reached with Tab, and Tab on to the next field from it. */
  openOnFocus?: boolean
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
  const listRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  // Closing hands the focus back to the field: it mustn't open the list again then.
  const justClosed = useRef(false)
  // Tab out of the list moves the focus on itself, rather than back to the field.
  const tabbing = useRef(false)
  const close = () => {
    justClosed.current = true
    setOpen(false)
    setQuery("")
  }
  // Keep the active row in view as the arrows move it, wrapping round included.
  useEffect(() => {
    listRef.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: "nearest" })
  }, [active])

  const current = options.find((o) => o.name.toLowerCase() === value.toLowerCase())
  const q = query.trim().toLowerCase()
  const found = useMemo(() => {
    const idle = (o: Option) => isInactive(o) && o.name.toLowerCase() !== value.toLowerCase()
    const hit = options.filter((o) => matches(o.name, q) || (idle(o) && sharesWord(o.name, q)))
    return q ? [...hit.filter((o) => !idle(o)), ...hit.filter(idle).map((o) => ({ ...o, idle: true }))] : hit.filter((o) => !idle(o))
  }, [options, q, value])
  const canCreate = q !== "" && !options.some((o) => o.name.toLowerCase() === q)
  const rows: ({ t: "opt"; o: Option & { idle?: boolean } } | { t: "create" } | { t: "none" })[] = [
    ...found.map((o) => ({ t: "opt" as const, o })),
    ...(canCreate ? [{ t: "create" as const }] : []),
    ...(value && !q ? [{ t: "none" as const }] : []),
  ]

  const [ask, confirmEl] = useConfirm()
  const pick = (name: string) => {
    close()
    if (name !== value) onChange(name)
  }
  // A new type or card is easy to make by a slip of the keyboard, so it's confirmed first.
  const create = (name: string) => {
    close()
    ask({
      title: kind === "type" ? t("Create the type “{name}”?", { name }) : t("Create the card “{name}”?", { name }),
      action: t("Create"),
      tone: "default",
      run: () => onChange(name),
    })
  }
  const choose = (i: number) => {
    const r = rows[i]
    if (!r) return
    if (r.t === "opt") pick(r.o.name)
    else if (r.t === "create") create(query.trim())
    else pick("")
  }
  const onKey = (e: KeyboardEvent) => {
    // The arrows go round: up from the first row to the last, down from the last to the first.
    if (e.key === "ArrowDown" && rows.length) {
      e.preventDefault()
      setActive((a) => (a + 1) % rows.length)
    } else if (e.key === "ArrowUp" && rows.length) {
      e.preventDefault()
      setActive((a) => (a - 1 + rows.length) % rows.length)
    } else if (e.key === "Enter") {
      e.preventDefault()
      choose(active)
    } else if (e.key === "Tab" && openOnFocus) {
      // On to the next field (back, with Shift), taking the match typed for, if any.
      e.preventDefault()
      const r = q ? rows[active] : undefined
      tabbing.current = true
      if (r?.t === "opt") pick(r.o.name)
      else close()
      const from = triggerRef.current
      requestAnimationFrame(() => {
        tabbing.current = false
        // The focus went on, not back to the field: coming back to it later opens the list again.
        justClosed.current = false
        if (from) focusNext(from, e.shiftKey ? -1 : 1)
      })
    }
  }

  return (
    <>
      <Popover
        open={open}
        onOpenChange={(o) => {
          if (!o) justClosed.current = true
          setOpen(o)
          setQuery("")
          setActive(0)
        }}
      >
        <PopoverTrigger asChild>
          <button
            ref={triggerRef}
            type="button"
            className={cn("cell", className)}
            aria-label={`${label}: ${value || t("none")}`}
            onFocus={(e) => {
              // Reached by keyboard (not a click, not the focus coming back from the list): open.
              if (openOnFocus && !open && !justClosed.current && e.currentTarget.matches(":focus-visible")) {
                setActive(0)
                setOpen(true)
              }
            }}
            onBlur={() => (justClosed.current = false)}
          >
            {value ? <Tag name={value} color={current?.color ?? "gray"} className="!shadow-none !bg-transparent !px-0" /> : <span className="text-muted-foreground/70">{placeholder}</span>}
          </button>
        </PopoverTrigger>
        <PopoverContent
          data-sheet
          align="start"
          sideOffset={2}
          className="w-64 gap-0 overflow-hidden rounded-[14px] border border-rule-2 p-0 shadow-[0_16px_48px_rgb(0_0_0/.18)] ring-0"
          onOpenAutoFocus={(e) => {
            e.preventDefault()
            inputRef.current?.focus()
          }}
          onCloseAutoFocus={(e) => tabbing.current && e.preventDefault()}
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
          <div ref={listRef} className="max-h-64 overflow-y-auto py-1" role="listbox" aria-label={label}>
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
                    <span className="flex min-w-0 flex-1 items-center gap-2" data-color={r.o.color}><span className="dot" /><span className="truncate">{r.o.name}</span></span>
                    {r.o.idle && r.o.last_used && <span className="shrink-0 text-[12px] text-muted-foreground">{t("last used {when}", { when: dateLabel(r.o.last_used, { month: "short", year: "numeric" }) })}</span>}
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
      {confirmEl}
    </>
  )
}
