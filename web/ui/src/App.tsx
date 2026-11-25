import { useCallback, useEffect, useRef, useState } from "react"
import { BotIcon, CheckIcon, ChevronsUpDownIcon, LayoutDashboardIcon, LogOutIcon, PlusIcon, UsersIcon, MenuIcon, NotebookTextIcon, PanelLeftCloseIcon, PanelLeftOpenIcon, SettingsIcon, type LucideIcon } from "lucide-react"
import { toast } from "sonner"

import { EntryDialog } from "@/components/entry-dialog"
import { NewLedgerDialog } from "@/components/ledger-dialog"
import { LedgerSettingsDialog } from "@/components/ledger-settings"
import { Logo, Wordmark } from "@/components/logo"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Toaster } from "@/components/ui/sonner"
import { api, money, monthLabel, plainAmount, setCurrentLedger, setSignedOutHandler, thisMonth, type Config, type Entry, type GroupTotal, type Ledger, type ModelSettings, type MonthTotal, type Options, type User } from "@/lib/api"
import { AppContext, applyTheme, type AppState } from "@/lib/state"
import { loadPinyin } from "@/lib/search"
import { cn } from "@/lib/utils"
import { AgentView } from "@/views/agent-view"
import { LoginView } from "@/views/login-view"
import { SettingsView } from "@/views/settings-view"
import { OverviewView } from "@/views/overview-view"
import { LedgerView } from "@/views/ledger-view"
import { setLanguage, t, tn, useLang } from "@/lib/i18n"

type Route = "overview" | "ledger" | "agent" | "settings"

const SIDEBAR_KEY = "paylist-sidebar"

/** How many months the sidebar lists before "Show all". */
const RECENT_MONTHS = 5

const NAV: { route: Route; label: () => string; icon: LucideIcon }[] = [
  { route: "overview", label: () => t("Overview"), icon: LayoutDashboardIcon },
  { route: "ledger", label: () => t("Ledger"), icon: NotebookTextIcon },
  { route: "agent", label: () => t("Agent"), icon: BotIcon },
]

function readRoute(): { route: Route; arg: string; arg2: string } {
  const [, route = "", arg = "", arg2 = ""] = location.hash.split("/")
  const known = ["overview", "ledger", "agent", "settings"]
  // Old addresses still work: #/table/… was the ledger, #/stats the overview.
  const r = ({ table: "ledger", stats: "overview" } as Record<string, string>)[route] ?? route
  // The overview is the start page.
  return { route: (known.includes(r) ? r : "overview") as Route, arg: decodeURIComponent(arg), arg2: decodeURIComponent(arg2) }
}

export function App() {
  const language = useLang()
  const [config, setConfig] = useState<Config | null>(null)
  const [session, setSession] = useState<{ user: User; model: ModelSettings } | null>(null)
  const [options, setOptions] = useState<Options>({ types: [], cards: [] })
  // The user's ledgers and the one open; requests carry it (api.setCurrentLedger).
  const [ledgers, setLedgers] = useState<Ledger[]>([])
  const [ledgerID, setLedgerID] = useState(0)
  const [newLedger, setNewLedger] = useState(false)
  const [ledgerSettings, setLedgerSettings] = useState<number | null>(null)
  const closeLedgerSettings = useCallback(() => setLedgerSettings(null), [])
  const [booting, setBooting] = useState(true)
  const [loc, setLoc] = useState(readRoute)
  const [tableVersion, setTableVersion] = useState(0)
  const [menuOpen, setMenuOpen] = useState(false)
  // The phone menu stays a moment after closing, to slide away.
  const [menuShown, setMenuShown] = useState(false)
  useEffect(() => {
    if (menuOpen) return setMenuShown(true)
    const id = setTimeout(() => setMenuShown(false), 200)
    return () => clearTimeout(id)
  }, [menuOpen])
  // Hiding the sidebar (from tablets up) is remembered on this browser.
  const [sideHidden, setSideHidden] = useState(() => {
    try {
      return localStorage.getItem(SIDEBAR_KEY) === "hidden"
    } catch {
      return false
    }
  })
  const hideSidebar = (hidden: boolean) => {
    setSideHidden(hidden)
    try {
      localStorage.setItem(SIDEBAR_KEY, hidden ? "hidden" : "shown")
    } catch {
      /* no storage */
    }
  }
  const [byType, setByType] = useState<GroupTotal[]>([])
  const [months, setMonths] = useState<MonthTotal[]>([])
  const [allMonths, setAllMonths] = useState(false)
  const [form, setForm] = useState<{ open: boolean; entry?: Entry | null }>({ open: false })
  const [refunding, setRefunding] = useState(false)

  useEffect(() => {
    // A new page closes the phone menu, whatever led there.
    const on = () => {
      setLoc(readRoute())
      setMenuOpen(false)
    }
    window.addEventListener("hashchange", on)
    return () => window.removeEventListener("hashchange", on)
  }, [])

  const boot = useCallback(async () => {
    try {
      const cfg = await api.config()
      setConfig(cfg)
      if (cfg.needs_setup) {
        setSession(null)
        return
      }
      try {
        const me = await api.me()
        setSession(me)
        applyTheme(me.user.prefs.theme)
        // "system" on the account leaves whatever this browser was set to (the sign-in page has its own switch).
        if (me.user.prefs.language !== "system") setLanguage(me.user.prefs.language)
        const lg = await api.ledgers()
        setLedgers(lg.ledgers)
        setLedgerID(lg.current)
        setCurrentLedger(lg.current)
        setOptions(await api.options())
      } catch {
        setSession(null)
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t("Can't reach the server."))
    } finally {
      setBooting(false)
    }
  }, [])

  useEffect(() => {
    setSignedOutHandler(() => setSession(null))
    void boot()
  }, [boot])

  // Pinyin matching (lib/search) loads once the app is up.
  useEffect(() => {
    const id = setTimeout(() => void loadPinyin(), 1500)
    return () => clearTimeout(id)
  }, [])

  // N opens a new entry and R a refund, both on the ledger, unless typing somewhere (or only viewing the ledger).
  const canWriteRef = useRef(true)
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement
      const key = e.key.toLowerCase()
      if ((key !== "n" && key !== "r") || e.metaKey || e.ctrlKey || e.altKey || !canWriteRef.current) return
      if (t.closest("input, textarea, select, [contenteditable], [role=dialog]")) return
      e.preventDefault()
      // Both open over the ledger, where what they change shows up.
      if (readRoute().route !== "ledger") location.hash = "#/ledger"
      if (key === "n") setForm({ open: true })
      else setRefunding(true)
    }
    window.addEventListener("keydown", on)
    return () => window.removeEventListener("keydown", on)
  }, [])

  const signedIn = !!session
  useEffect(() => {
    if (!signedIn) return
    api
      .stats(thisMonth(), 1)
      .then((st) => setByType(st.by_type))
      .catch(() => {})
    api
      .months()
      .then((list) => {
        // This month is always listed, even before its first entry.
        const now = thisMonth()
        setMonths(list[0]?.month === now ? list : [{ month: now, total: 0, count: 0 }, ...list])
      })
      .catch(() => {})
  }, [signedIn, tableVersion, ledgerID])

  const reloadOptions = useCallback(async () => {
    setOptions(await api.options())
  }, [])

  if (booting) {
    return (
      // The mark, breathing, where the page will be; shown after a beat so a quick start doesn't flash it.
      <div className="grid min-h-dvh place-items-center" role="status" aria-label={t("Loading")}>
        <div className="loading-in">
          <Logo className="breathe size-10" />
        </div>
      </div>
    )
  }
  if (!config) {
    return (
      <div className="grid min-h-dvh place-items-center p-6 text-muted-foreground">
        <div>
          <p>{t("Paylist can't reach its server.")}</p>
          <button type="button" className="btn quiet mt-4" onClick={() => location.reload()}>
            {t("Try again")}
          </button>
        </div>
        <Toaster />
      </div>
    )
  }
  if (!session) {
    return (
      <>
        <LoginView setup={config.needs_setup} onSignedIn={() => { setBooting(true); void boot() }} />
        <Toaster />
      </>
    )
  }

  const ledger = ledgers.find((l) => l.id === ledgerID) ?? ledgers[0]
  const canWrite = ledger?.role !== "viewer"
  canWriteRef.current = canWrite
  const switchLedger = async (id: number) => {
    setCurrentLedger(id)
    setLedgerID(id)
    setTableVersion((v) => v + 1)
    try {
      setOptions(await api.options())
      const prefs = await api.savePrefs({ ledger_id: id })
      setSession((s) => (s ? { ...s, user: { ...s.user, prefs } } : s))
    } catch (e) {
      toast.error((e as Error).message)
    }
  }
  const reloadLedgers = async (open?: number) => {
    const lg = await api.ledgers()
    setLedgers(lg.ledgers)
    const next = open ?? (lg.ledgers.some((l) => l.id === ledgerID) ? ledgerID : lg.current)
    if (next !== ledgerID) await switchLedger(next)
  }
  if (!ledger) {
    return (
      <div className="grid min-h-dvh place-items-center" role="status" aria-label={t("Loading")}>
        <Logo className="breathe size-10" />
      </div>
    )
  }

  const state: AppState = {
    config,
    ledger,
    ledgers,
    switchLedger,
    reloadLedgers,
    canWrite,
    user: session.user,
    setUser: (user) => setSession((s) => (s ? { ...s, user } : s)),
    model: session.model,
    setModel: (model) => setSession((s) => (s ? { ...s, model } : s)),
    options,
    reloadOptions,
    tableVersion,
    bumpTable: () => setTableVersion((v) => v + 1),
    quickAdd: () => setForm({ open: true }),
    editEntry: (entry) => setForm({ open: true, entry }),
    refund: () => setRefunding(true),
  }

  const signOut = async () => {
    await api.logout().catch(() => {})
    setSession(null)
    location.hash = ""
  }
  const go = () => setMenuOpen(false)
  const ledgerMonth = /^\d{4}-\d{2}$/.test(loc.arg) && loc.route === "ledger" && loc.arg <= thisMonth() ? loc.arg : thisMonth()
  // The latest few months, plus the one open in the ledger if it's further back.
  const shownMonths = allMonths ? months : months.filter((m, i) => i < RECENT_MONTHS || (loc.route === "ledger" && m.month === ledgerMonth))
  const sidebar = (
    <div className="flex h-full flex-col px-2.5 pt-3 pb-3">
      <div className="flex items-center gap-1">
        {/* The ledger open, and the way to the others, a new one, its settings and the account. */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <button type="button" className="flex h-9 min-w-0 flex-1 items-center gap-2.5 rounded-[10px] px-2.5 text-left hover:bg-wash" aria-label={t("Ledgers and account")}>
              <Logo className="size-[20px]" />
              <span className="min-w-0 flex-1 truncate text-[15px] font-semibold">{ledger.name}</span>
              <ChevronsUpDownIcon className="size-3.5 shrink-0 text-ink-2" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-64 rounded-[12px] border-0 shadow-[0_16px_48px_rgb(0_0_0/.18),0_0_0_1px_var(--rule-2)] ring-0">
            <DropdownMenuLabel className="font-normal text-muted-foreground">{t("Ledgers")}</DropdownMenuLabel>
            {ledgers.map((l) => (
              <DropdownMenuItem key={l.id} onSelect={() => {
                  go()
                  if (l.id !== ledger.id) void switchLedger(l.id)
                }}>
                <CheckIcon className={cn(l.id !== ledger.id && "invisible")} />
                <span className="min-w-0 flex-1 truncate">{l.name}</span>
                {l.role !== "owner" ? (
                  <span className="text-[11.5px] text-muted-foreground">{l.role === "viewer" ? t("Viewer") : t("Editor")}</span>
                ) : (
                  l.members > 1 && <UsersIcon className="!size-3.5 text-muted-foreground" />
                )}
              </DropdownMenuItem>
            ))}
            <DropdownMenuItem onSelect={() => (go(), setNewLedger(true))}><PlusIcon /> {t("New ledger")}</DropdownMenuItem>
            <DropdownMenuItem onSelect={() => (go(), setLedgerSettings(ledger.id))}><SettingsIcon /> {t("Ledger settings")}</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuLabel className="truncate font-normal text-muted-foreground">{session.user.email}</DropdownMenuLabel>
            <DropdownMenuItem onSelect={signOut}><LogOutIcon /> {t("Sign out")}</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        {/* Hides the sidebar; on phones, where it slides in, closes it. */}
        <button
          type="button"
          aria-label={menuOpen ? t("Close the menu") : t("Hide sidebar")}
          title={menuOpen ? t("Close the menu") : t("Hide sidebar")}
          className="grid size-9 shrink-0 place-items-center rounded-[10px] text-ink-2 hover:bg-wash hover:text-ink"
          onClick={() => (menuOpen ? setMenuOpen(false) : hideSidebar(true))}
        >
          <PanelLeftCloseIcon className="size-[18px]" />
        </button>
      </div>

      <nav className="mt-3 grid gap-0.5" aria-label={t("Pages")}>
        {NAV.map(({ route, label, icon: Icon }) => (
          <a key={route} href={`#/${route}`} onClick={go} className="side-item" aria-current={loc.route === route ? "page" : undefined}>
            <Icon /> {label()}
          </a>
        ))}
      </nav>

      <div className="-mx-1 mt-5 min-h-0 flex-1 overflow-y-auto px-1">
        {byType.length > 0 && (
          <section>
            <div className="px-2.5 pb-1.5 text-[12.5px] text-ink-2">{t("This month")}</div>
            <ul className="grid gap-0.5">
              {byType.map((g) => {
                const href = `#/ledger/${thisMonth()}/${encodeURIComponent(g.name)}`
                const current = loc.route === "ledger" && ledgerMonth === thisMonth() && loc.arg2 === g.name && !!g.name
                return (
                  <li key={g.name || "(none)"}>
                    <a
                      href={g.name ? href : `#/ledger/${thisMonth()}`}
                      onClick={go}
                      aria-current={current ? "page" : undefined}
                      className="flex h-8 items-center gap-2 rounded-[10px] px-2.5 text-[13.5px] hover:bg-wash aria-[current=page]:bg-fill aria-[current=page]:font-medium"
                      data-color={g.name ? g.color : "gray"}
                      title={`${g.name || t("No type")}: ${money(g.total, ledger.currency)}, ${tn(g.count, "{n} entry", "{n} entries")}`}
                    >
                      <span className="dot" />
                      <span className={cn("min-w-0 flex-1 truncate", !g.name && "text-ink-2")}>{g.name || t("No type")}</span>
                      <span className="shrink-0 text-[12px] font-normal text-ink-2 tabular-nums">{plainAmount(g.total)}</span>
                    </a>
                  </li>
                )
              })}
            </ul>
          </section>
        )}
        {months.length > 0 && (
          <section className="mt-5">
            <div className="flex items-center justify-between px-2.5 pb-1.5 text-[12.5px] text-ink-2">
              {t("Months")}
              {months.length > RECENT_MONTHS && (
                <button type="button" onClick={() => setAllMonths((v) => !v)} aria-expanded={allMonths} className="-mr-1.5 rounded-md px-1.5 hover:bg-wash hover:text-ink">
                  {allMonths ? t("Show less") : t("Show all ({n})", { n: months.length })}
                </button>
              )}
            </div>
            <ul className="grid gap-0.5">
              {shownMonths.map((m) => (
                <li key={m.month}>
                  <a
                    href={`#/ledger/${m.month}`}
                    onClick={go}
                    aria-current={loc.route === "ledger" && ledgerMonth === m.month ? "page" : undefined}
                    className="flex h-8 items-center gap-2 rounded-[10px] px-2.5 text-[13.5px] hover:bg-wash aria-[current=page]:bg-fill aria-[current=page]:font-medium"
                    title={`${monthLabel(m.month)}: ${tn(m.count, "{n} entry", "{n} entries")}`}
                  >
                    <span className="min-w-0 flex-1 truncate">{monthLabel(m.month)}</span>
                    <span className="shrink-0 text-[12px] font-normal text-ink-2 tabular-nums">{plainAmount(m.total)}</span>
                  </a>
                </li>
              ))}
            </ul>
          </section>
        )}
      </div>

      <div className="grid gap-0.5 border-t border-rule pt-3 mt-3">
        <a href="#/settings" onClick={go} className="side-item" aria-current={loc.route === "settings" ? "page" : undefined}>
          <SettingsIcon /> {t("Settings")}
        </a>
      </div>
    </div>
  )

  return (
    <AppContext.Provider value={state}>
      {/* --bar: the top bar's height, which pages that fill the screen (the agent) leave room for. */}
      <div className={cn("flex min-h-dvh [--bar:48px]", !sideHidden && "md:[--bar:0px]")} key={language}>
        {/* Sidebar, from tablets up, unless hidden */}
        {/* From tablets up: the sidebar folds away to the left, and back. */}
        <aside
          inert={sideHidden}
          className={cn(
            "sticky top-0 hidden h-dvh shrink-0 overflow-hidden bg-side transition-[width] duration-300 ease-[cubic-bezier(.2,.8,.2,1)] motion-reduce:transition-none md:block",
            sideHidden ? "w-0" : "w-[272px]",
          )}
        >
          <div className={cn("h-full w-[272px] transition-[translate,opacity] duration-300 ease-[cubic-bezier(.2,.8,.2,1)] motion-reduce:transition-none", sideHidden && "-translate-x-8 opacity-0")}>{sidebar}</div>
        </aside>

        {/* Phones: the same sidebar, slid in from the menu button */}
        {(menuOpen || menuShown) && (
          <div className="fixed inset-0 z-40 md:hidden" role="dialog" aria-modal="true" aria-label={t("Menu")} inert={!menuOpen}>
            <button
              type="button"
              aria-label={t("Close the menu")}
              className={cn("absolute inset-0 bg-black/30 duration-200", menuOpen ? "animate-in fade-in-0" : "animate-out fade-out-0 fill-mode-forwards")}
              onClick={go}
            />
            <aside
              className={cn(
                "absolute inset-y-0 left-0 w-[284px] bg-side pt-[env(safe-area-inset-top,0px)] shadow-xl duration-200 ease-out",
                menuOpen ? "animate-in slide-in-from-left" : "animate-out slide-out-to-left fill-mode-forwards",
              )}
            >
              {sidebar}
            </aside>
          </div>
        )}

        <div className="flex min-w-0 flex-1 flex-col">
          {/* The top bar: on phones always; from tablets up only while the sidebar is hidden, to bring it back. */}
          <header
            className={cn(
              "sticky top-0 z-30 flex h-12 items-center gap-2 overflow-hidden border-b border-rule bg-paper px-3 transition-[height,visibility] duration-300 ease-[cubic-bezier(.2,.8,.2,1)] motion-reduce:transition-none",
              !sideHidden && "md:invisible md:h-0 md:border-b-0",
            )}
          >
            <button type="button" aria-label={t("Open the menu")} className="grid size-8 place-items-center rounded-md text-ink-2 hover:bg-wash md:hidden" onClick={() => setMenuOpen(true)}>
              <MenuIcon className="size-5" />
            </button>
            <button type="button" aria-label={t("Show sidebar")} title={t("Show sidebar")} className="hidden size-8 place-items-center rounded-md text-ink-2 hover:bg-wash hover:text-ink md:grid" onClick={() => hideSidebar(false)}>
              <PanelLeftOpenIcon className="size-[18px]" />
            </button>
            <Wordmark />
          </header>
          <main className="flex min-h-0 flex-1 flex-col">
            {loc.route === "ledger" && <LedgerView month={ledgerMonth} typeParam={loc.arg2} setMonth={(m) => (location.hash = `#/ledger/${m}`)} />}
            {loc.route === "overview" && <OverviewView />}
            {loc.route === "agent" && <AgentView chatId={loc.arg} />}
            {loc.route === "settings" && <SettingsView />}
          </main>
        </div>
      </div>
      <EntryDialog open={form.open} entry={form.entry} onOpenChange={(open) => setForm((f) => ({ ...f, open }))} />
      <EntryDialog refund open={refunding} onOpenChange={setRefunding} />
      <LedgerSettingsDialog id={ledgerSettings} onClose={closeLedgerSettings} />
      <NewLedgerDialog open={newLedger} onOpenChange={setNewLedger} currency={ledger.currency} onCreated={(l) => void reloadLedgers(l.id)} />
      <Toaster position="bottom-center" />
    </AppContext.Provider>
  )
}
