import { useEffect, useState, type FormEvent, type ReactNode } from "react"
import { CheckIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { useConfirm } from "@/components/confirm"
import { PageHeader } from "@/components/month-picker"
import { Checkbox } from "@/components/ui/checkbox"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Spinner } from "@/components/ui/spinner"
import { api, type AgentToken, type Option, type User } from "@/lib/api"
import { PRESETS } from "@/lib/presets"
import { applyTheme, useApp } from "@/lib/state"
import { cn } from "@/lib/utils"
import { locale, setLanguage, t, tn, tParts, type LangPref } from "@/lib/i18n"

/** One group of settings: its name above, the controls in a card. */
function Section({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return (
    <section id={id} className="scroll-mt-6">
      <h2 className="px-1 pb-2 text-[13px] text-ink-2">{title}</h2>
      <div className="settings-card p-4 sm:p-5">{children}</div>
    </section>
  )
}

function Field({ label, htmlFor, children }: { label: string; htmlFor?: string; children: ReactNode }) {
  return (
    <div className="grid content-start gap-1">
      <label htmlFor={htmlFor} className="label">{label}</label>
      {children}
    </div>
  )
}

function CopyButton({ text, label = t("Copy") }: { text: string; label?: string }) {
  const [done, setDone] = useState(false)
  return (
    <button
      type="button"
      className="btn quiet shrink-0"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setDone(true)
          setTimeout(() => setDone(false), 1500)
        } catch {
          toast.error(t("Copying isn't allowed here. Select the text and copy it."))
        }
      }}
    >
      {done ? <CheckIcon /> : null}
      {done ? t("Copied") : label}
    </button>
  )
}

// ---------------------------------------------------------------- AI model

function ModelSection() {
  const { model, setModel, config } = useApp()
  const presets = Object.entries(PRESETS).filter(([, p]) => !p.devOnly || config.allow_fake)
  const [preset, setPreset] = useState(model.active || "anthropic")
  const p = PRESETS[preset] ?? PRESETS.custom
  const saved = model.providers[preset]
  const [baseURL, setBaseURL] = useState(saved?.base_url ?? p.base_url)
  const [name, setName] = useState(saved?.model ?? p.model)
  const [key, setKey] = useState("")
  const [busy, setBusy] = useState(false)
  const hasKey = !!model.has_key[preset]

  const choose = (id: string) => {
    setPreset(id)
    const s = model.providers[id]
    setBaseURL(s?.base_url ?? PRESETS[id].base_url)
    setName(s?.model ?? PRESETS[id].model)
    setKey("")
  }

  const save = async (e: FormEvent, removeKey = false) => {
    e.preventDefault()
    setBusy(true)
    try {
      const next = await api.saveModel(preset, { kind: p.kind, base_url: baseURL.trim(), model: name.trim() }, removeKey ? null : key.trim() ? key.trim() : undefined)
      setModel(next)
      setKey("")
      toast.success(removeKey ? t("Removed the key.") : t("The agent now uses {provider}.", { provider: t(p.label) }))
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Section
      id="model"
      title={t("AI model")}
    >
      <form onSubmit={save} className="grid gap-5">
        <div className="overflow-hidden rounded-[12px] shadow-[inset_0_0_0_1px_var(--rule)]" role="radiogroup" aria-label={t("Provider")}>
          {presets.map(([id, pr]) => (
            <button
              key={id}
              type="button"
              role="radio"
              aria-checked={preset === id}
              onClick={() => choose(id)}
              className={cn("flex w-full items-center gap-3 border-b border-rule px-3.5 py-2.5 text-left last:border-b-0 hover:bg-wash", preset === id && "bg-wash")}
            >
              <span className={cn("grid size-4 shrink-0 place-items-center rounded-full border", preset === id ? "border-ink" : "border-rule-2")}>
                {preset === id && <span className="size-2 rounded-full bg-ink" />}
              </span>
              <span className="min-w-0 flex-1 truncate text-[14px] font-medium">{t(pr.label)}</span>
              <span className="hidden text-[12.5px] text-muted-foreground sm:inline">
                {pr.kind === "anthropic" ? "Anthropic API" : pr.kind === "fake" ? t("offline") : t("OpenAI-compatible")}
              </span>
              <span className={cn("w-20 text-right text-[12.5px]", model.active === id ? "font-medium text-good" : "text-muted-foreground")}>
                {model.active === id ? t("in use") : model.has_key[id] ? t("key saved") : ""}
              </span>
            </button>
          ))}
        </div>
        {p.note && <p className="-mt-2 text-[13px] text-ink-2">{t(p.note)}</p>}
        {p.kind !== "fake" && (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("Model")} htmlFor="model-name">
              <input id="model-name" className="field" list="model-list" value={name} onChange={(e) => setName(e.target.value)} placeholder={t("Model name")} required={p.kind === "openai"} />
              <datalist id="model-list">
                {p.models.map((m) => (
                  <option key={m} value={m} />
                ))}
              </datalist>
            </Field>
            <Field label={t("Base URL")} htmlFor="model-base">
              <input id="model-base" className="field" value={baseURL} onChange={(e) => setBaseURL(e.target.value)} placeholder={p.kind === "anthropic" ? "https://api.anthropic.com" : "https://…/v1"} />
            </Field>
            <div className="sm:col-span-2">
              <Field label={t("API key")} htmlFor="model-key">
                <div className="flex gap-2">
                  <input id="model-key" className="field" type="password" autoComplete="off" value={key} onChange={(e) => setKey(e.target.value)} placeholder={hasKey ? "••••••••••••" : p.keyOptional ? t("Optional") : t("Paste your key")} />
                  {hasKey && (
                    <button type="button" className="btn danger !h-[34px]" onClick={(e) => void save(e, true)} disabled={busy}>
                      {t("Remove key")}
                    </button>
                  )}
                </div>
              </Field>
            </div>
          </div>
        )}
        <div>
          <button type="submit" className="btn" disabled={busy}>
            {busy && <Spinner className="size-3.5" />} {model.active === preset ? t("Save") : t("Use {provider}", { provider: t(p.label) })}
          </button>
        </div>
      </form>
    </Section>
  )
}

// ---------------------------------------------------------------- MCP access

function AgentAccessSection() {
  const [tokens, setTokens] = useState<AgentToken[] | null>(null)
  const [name, setName] = useState("")
  const [fresh, setFresh] = useState<{ token: string; endpoint: string } | null>(null)
  const [ask, confirmEl] = useConfirm()
  const endpoint = `${location.origin}/mcp`

  useEffect(() => {
    api.tokens().then(setTokens).catch((e) => toast.error((e as Error).message))
  }, [])

  const create = async (e: FormEvent) => {
    e.preventDefault()
    try {
      const res = await api.createToken(name || t("Agent"))
      setFresh({ token: res.token, endpoint: res.endpoint })
      setTokens((l) => [res.info, ...(l ?? [])])
      setName("")
    } catch (err) {
      toast.error((err as Error).message)
    }
  }
  const command = fresh ? `claude mcp add --transport http paylist ${fresh.endpoint} --header "Authorization: Bearer ${fresh.token}"` : ""

  return (
    <Section id="mcp" title={t("Agent access")}>
      <div className="grid gap-5">
        <Field label={t("Server address")}>
          <div className="flex gap-2">
            <code className="field flex items-center overflow-x-auto !bg-side font-mono text-[12.5px] whitespace-nowrap">{endpoint}</code>
            <CopyButton text={endpoint} />
          </div>
        </Field>
        <form onSubmit={create} className="grid gap-1">
          <label htmlFor="token-name" className="label">{t("New token")}</label>
          <div className="flex gap-2">
            <input id="token-name" className="field" value={name} onChange={(e) => setName(e.target.value)} placeholder={t("Name it after the agent, such as Claude Code")} maxLength={60} />
            <button type="submit" className="btn shrink-0 !h-[34px]">{t("Create token")}</button>
          </div>
        </form>
        {fresh && (
          <div className="grid gap-3 rounded-[12px] bg-side p-4 shadow-[inset_0_0_0_1px_var(--rule-2)]">
            <p className="text-[13.5px] font-semibold">{t("Copy this token now. It won't be shown again.")}</p>
            <div className="flex gap-2">
              <code className="field flex items-center overflow-x-auto !bg-sheet font-mono text-[12.5px] whitespace-nowrap">{fresh.token}</code>
              <CopyButton text={fresh.token} />
            </div>
            <p className="text-[13px] text-ink-2">{t("To connect Claude Code, run:")}</p>
            <div className="flex items-start gap-2">
              <code className="min-w-0 flex-1 rounded-[10px] bg-sheet px-3 py-2 font-mono text-[12px] break-all shadow-[inset_0_0_0_1px_var(--rule-2)]">{command}</code>
              <CopyButton text={command} />
            </div>
            <p className="text-[12.5px] text-muted-foreground">{tParts("Other agents take the same address, with the header {header} and the token.", { header: <code className="font-mono">Authorization: Bearer</code> })}</p>
          </div>
        )}
        {tokens && tokens.length > 0 && (
          <table className="w-full text-[13.5px]">
            <thead>
              <tr className="border-b border-rule">
                <th className="label py-1.5 text-left font-normal">{t("Token")}</th>
                <th className="label hidden py-1.5 text-left font-normal sm:table-cell">{t("Last used")}</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {tokens.map((tk) => (
                <tr key={tk.id} className="border-b border-rule">
                  <td className="py-2">
                    <div className="font-medium">{tk.name}</div>
                    <div className="figure text-[12px] text-muted-foreground">{tk.hint}</div>
                  </td>
                  <td className="figure hidden py-2 text-[12.5px] text-ink-2 sm:table-cell">{tk.last_used_at ? new Date(tk.last_used_at).toLocaleString(locale()) : t("never")}</td>
                  <td className="py-2 text-right">
                    <button
                      type="button"
                      className="text-[13px] font-medium text-red hover:underline"
                      onClick={() =>
                        ask({
                          title: t("Revoke {name}?", { name: tk.name }),
                          body: t("Agents using this token lose access at once."),
                          action: t("Revoke"),
                          run: async () => {
                            await api.deleteToken(tk.id).catch((e) => toast.error((e as Error).message))
                            setTokens((l) => (l ?? []).filter((x) => x.id !== tk.id))
                          },
                        })
                      }
                    >
                      {t("Revoke")}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      {confirmEl}
    </Section>
  )
}

// ---------------------------------------------------------------- options

function OptionRow({ o, colors, onChanged, onDelete }: { o: Option; colors: string[]; onChanged: () => void; onDelete: () => void }) {
  const [name, setName] = useState(o.name)
  useEffect(() => setName(o.name), [o.name])
  const save = async (change: { name?: string; color?: string }) => {
    try {
      await api.updateOption(o.id, change)
      onChanged()
    } catch (e) {
      toast.error((e as Error).message)
      setName(o.name)
    }
  }
  return (
    <tr className="group border-b border-rule">
      <td className="w-9 py-1">
        <Popover>
          <PopoverTrigger asChild>
            <button type="button" className="grid size-7 place-items-center hover:bg-wash" aria-label={t("{name}'s color: {color}. Change it", { name: o.name, color: t(o.color) })} data-color={o.color}>
              <span className="dot !size-2.5" />
            </button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-44 gap-0 rounded-[12px] border-0 p-1 shadow-[0_16px_48px_rgb(0_0_0/.18),0_0_0_1px_var(--rule-2)] ring-0">
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
          className="h-8 w-full rounded-md bg-transparent px-1.5 text-[14px] outline-none hover:bg-wash focus:bg-sheet focus:shadow-[inset_0_0_0_1.5px_var(--ink)]"
        />
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

function OptionList({ kind, title }: { kind: "type" | "card"; title: string }) {
  const { options, reloadOptions, config, bumpTable } = useApp()
  const list = kind === "type" ? options.types : options.cards
  const [name, setName] = useState("")
  const [ask, confirmEl] = useConfirm()
  const add = async (e: FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return
    try {
      await api.createOption(kind, name)
      setName("")
      await reloadOptions()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }
  const changed = () => {
    void reloadOptions()
    bumpTable()
  }
  return (
    <div className="min-w-0">
      <table className="w-full">
        <thead>
          <tr className="border-b border-rule">
            <th className="label py-1.5 text-left font-normal" colSpan={2}>{title}</th>
            <th className="label py-1.5 text-right font-normal">{t("Used")}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {list.map((o) => (
            <OptionRow
              key={o.id}
              o={o}
              colors={config.colors}
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
                    await api.deleteOption(o.id).catch((e) => toast.error((e as Error).message))
                    changed()
                  },
                })
              }
            />
          ))}
        </tbody>
      </table>
      <form onSubmit={add} className="mt-2 flex gap-2">
        <input id={`new-${kind}`} className="field !h-8" value={name} onChange={(e) => setName(e.target.value)} placeholder={kind === "type" ? t("Add a type") : t("Add a card")} maxLength={40} aria-label={kind === "type" ? t("New type") : t("New card")} />
        <button type="submit" className="btn quiet" disabled={!name.trim()}>{t("Add")}</button>
      </form>
      {confirmEl}
    </div>
  )
}

// ---------------------------------------------------------------- preferences, password, members

const CURRENCIES = ["CNY", "USD", "EUR", "GBP", "JPY", "HKD", "TWD", "SGD", "KRW", "AUD", "CAD", "CHF", "INR"]

function PreferencesSection() {
  const { user, setUser } = useApp()
  const save = async (p: Partial<User["prefs"]>) => {
    try {
      const prefs = await api.savePrefs(p)
      setUser({ ...user, prefs })
      if (p.theme) applyTheme(p.theme)
      if (p.language) setLanguage(p.language)
    } catch (e) {
      toast.error((e as Error).message)
    }
  }
  const currencies = CURRENCIES.includes(user.prefs.currency) ? CURRENCIES : [user.prefs.currency, ...CURRENCIES]
  return (
    <Section id="prefs" title={t("Display")}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("Currency")} htmlFor="pref-currency">
          <select id="pref-currency" className="field" value={user.prefs.currency} onChange={(e) => void save({ currency: e.target.value })}>
            {currencies.map((c) => (
              <option key={c} value={c}>{c}</option>
            ))}
          </select>
        </Field>
        <Field label={t("Theme")} htmlFor="pref-theme">
          <select id="pref-theme" className="field" value={user.prefs.theme} onChange={(e) => void save({ theme: e.target.value as User["prefs"]["theme"] })}>
            <option value="system">{t("Same as the device")}</option>
            <option value="light">{t("Light")}</option>
            <option value="dark">{t("Dark")}</option>
          </select>
        </Field>
        <Field label={t("Language")} htmlFor="pref-language">
          <select id="pref-language" className="field" value={user.prefs.language} onChange={(e) => void save({ language: e.target.value as LangPref })}>
            <option value="system">{t("Same as the browser")}</option>
            <option value="en" lang="en">English</option>
            <option value="zh" lang="zh-CN">中文</option>
          </select>
        </Field>
      </div>
    </Section>
  )
}

function PasswordSection() {
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.changePassword(current, next)
      setCurrent("")
      setNext("")
      toast.success(t("Changed your password. Your other devices are signed out."))
    } catch (err) {
      toast.error((err as Error).message)
    }
  }
  return (
    <Section id="password" title={t("Password")}>
      <form onSubmit={submit} className="grid gap-4 sm:grid-cols-2">
        <Field label={t("Current password")} htmlFor="pw-current">
          <input id="pw-current" className="field" type="password" autoComplete="current-password" value={current} onChange={(e) => setCurrent(e.target.value)} required />
        </Field>
        <Field label={t("New password")} htmlFor="pw-new">
          <input id="pw-new" className="field" type="password" autoComplete="new-password" minLength={8} value={next} onChange={(e) => setNext(e.target.value)} required />
        </Field>
        <div>
          <button type="submit" className="btn quiet">{t("Change password")}</button>
        </div>
      </form>
    </Section>
  )
}

function MembersSection() {
  const { user } = useApp()
  const [users, setUsers] = useState<User[]>([])
  const [form, setForm] = useState({ email: "", name: "", password: "", is_admin: false })
  const [resetFor, setResetFor] = useState<{ id: number; pw: string } | null>(null)
  const [ask, confirmEl] = useConfirm()
  const load = () => api.users().then(setUsers).catch((e) => toast.error((e as Error).message))
  useEffect(() => {
    void load()
  }, [])
  const add = async (e: FormEvent) => {
    e.preventDefault()
    try {
      await api.createUser(form)
      setForm({ email: "", name: "", password: "", is_admin: false })
      toast.success(t("Created the account. Give them the email and password."))
      void load()
    } catch (err) {
      toast.error((err as Error).message)
    }
  }
  const reset = async (e: FormEvent) => {
    e.preventDefault()
    if (!resetFor) return
    try {
      await api.resetPassword(resetFor.id, resetFor.pw)
      setResetFor(null)
      toast.success(t("Changed their password."))
    } catch (err) {
      toast.error((err as Error).message)
    }
  }
  return (
    <Section id="members" title={t("Members")}>
      <div className="grid gap-6">
        <table className="w-full text-[13.5px]">
          <thead>
            <tr className="border-b border-rule">
              <th className="label py-1.5 text-left font-normal">{t("Account")}</th>
              <th className="label py-1.5 text-left font-normal">{t("Role")}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id} className="border-b border-rule align-top">
                <td className="py-2">
                  <div className="font-medium">{u.name || u.email}</div>
                  {u.name && <div className="text-[12.5px] text-muted-foreground">{u.email}</div>}
                  {resetFor?.id === u.id && (
                    <form onSubmit={reset} className="mt-2 flex gap-2">
                      <input id={`reset-${u.id}`} className="field !h-8" type="password" autoComplete="new-password" minLength={8} placeholder={t("New password")} autoFocus value={resetFor.pw} onChange={(e) => setResetFor({ id: u.id, pw: e.target.value })} />
                      <button type="submit" className="btn">{t("Set")}</button>
                      <button type="button" className="btn quiet" onClick={() => setResetFor(null)}>{t("Cancel")}</button>
                    </form>
                  )}
                </td>
                <td className="py-2 font-mono text-[12px] text-ink-2">{u.is_admin ? t("admin") : t("member")}</td>
                <td className="py-2 text-right whitespace-nowrap">
                  <button type="button" className="text-[13px] text-ink-2 hover:text-ink hover:underline" onClick={() => setResetFor({ id: u.id, pw: "" })}>
                    {t("Reset password")}
                  </button>
                  {u.id !== user.id && (
                    <button
                      type="button"
                      className="ml-4 text-[13px] font-medium text-red hover:underline"
                      onClick={() =>
                        ask({
                          title: t("Delete {name}?", { name: u.email }),
                          body: t("Their ledger, types, cards, conversations and keys are deleted for good."),
                          action: t("Delete account"),
                          run: async () => {
                            await api.deleteUser(u.id).catch((e) => toast.error((e as Error).message))
                            void load()
                          },
                        })
                      }
                    >
                      {t("Delete")}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <form onSubmit={add} className="grid gap-3 sm:grid-cols-2">
          <div className="label sm:col-span-2">{t("Add someone")}</div>
          <input id="member-email" className="field" type="email" placeholder={t("Email")} required value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} aria-label={t("Email")} />
          <input id="member-name" className="field" placeholder={t("Name (optional)")} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} aria-label={t("Name")} />
          <input id="member-password" className="field" type="password" placeholder={t("Password, 8 characters or more")} minLength={8} required value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} aria-label={t("Password")} autoComplete="new-password" />
          <label className="flex items-center gap-2 text-[13.5px]">
            <Checkbox id="member-admin" checked={form.is_admin} onCheckedChange={(c) => setForm({ ...form, is_admin: !!c })} /> {t("Can manage accounts (admin)")}
          </label>
          <div>
            <button type="submit" className="btn quiet">{t("Add member")}</button>
          </div>
        </form>
      </div>
      {confirmEl}
    </Section>
  )
}

export function SettingsView() {
  const { user } = useApp()
  return (
    <div className="mx-auto w-full max-w-[1200px] px-4 pt-5 pb-16 sm:px-8">
      <PageHeader title={t("Settings")} />
      <div className="mx-auto mt-6 grid max-w-[700px] gap-8">
        <ModelSection />
        <AgentAccessSection />
        <Section id="options" title={t("Types and cards")}>
          <div className="grid gap-8 sm:grid-cols-2">
            <OptionList kind="type" title={t("Types")} />
            <OptionList kind="card" title={t("Cards")} />
          </div>
        </Section>
        <PreferencesSection />
        <PasswordSection />
        {user.is_admin && <MembersSection />}
      </div>
    </div>
  )
}
