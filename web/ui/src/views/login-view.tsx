import { useState, type FormEvent } from "react"
import { LanguagesIcon } from "lucide-react"

import { Wordmark } from "@/components/logo"
import { Spinner } from "@/components/ui/spinner"
import { api } from "@/lib/api"
import { setLanguage, t, useLang } from "@/lib/i18n"

/** Sign-in, or first-run setup (creating the admin) on a new server. */
export function LoginView({ setup, onSignedIn }: { setup: boolean; onSignedIn: () => void }) {
  const [email, setEmail] = useState("")
  const [name, setName] = useState("")
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const language = useLang()

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError("")
    try {
      if (setup) await api.setup(email, name, password)
      else await api.login(email, password)
      onSignedIn()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto flex min-h-dvh max-w-[1180px] flex-col px-4 py-6 sm:px-10">
      <div className="flex items-center justify-between">
        <Wordmark />
        <button
          type="button"
          className="pill soft !h-8 text-[13px]"
          onClick={() => setLanguage(language === "zh" ? "en" : "zh")}
          lang={language === "zh" ? "en" : "zh-CN"}
        >
          <LanguagesIcon /> {language === "zh" ? "English" : "中文"}
        </button>
      </div>
      <div className="flex flex-1 items-center justify-center py-10">
        <form onSubmit={submit} className="card w-full max-w-[400px] p-8">
          <h1 className="text-[22px] leading-tight font-semibold">{setup ? t("Set up Paylist") : t("Sign in to Paylist")}</h1>
          <p className="mt-2 text-[14px] text-ink-2">
            {setup ? t("Create the admin account for this server. You can add the rest of the household in Settings.") : t("Your spending, month by month, on this server.")}
          </p>
          <div className="mt-8 grid gap-4">
            {setup && (
              <label className="grid gap-1">
                <span className="label">{t("Name")}</span>
                <input id="name" className="field !h-9" value={name} onChange={(e) => setName(e.target.value)} autoComplete="name" placeholder={t("Optional")} />
              </label>
            )}
            <label className="grid gap-1">
              <span className="label">{t("Email")}</span>
              <input id="email" className="field !h-9" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="email" autoFocus />
            </label>
            <label className="grid gap-1">
              <span className="label">{t("Password")}</span>
              <input
                id="password"
                className="field !h-9"
                type="password"
                required
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete={setup ? "new-password" : "current-password"}
                minLength={setup ? 8 : undefined}
              />
              {setup && <span className="text-[12px] text-muted-foreground">{t("At least 8 characters.")}</span>}
            </label>
            {error && <p role="alert" className="border-l-2 border-red pl-3 text-[13px] text-red">{error}</p>}
            <button type="submit" disabled={busy} className="btn mt-2 !h-9 w-full">
              {busy && <Spinner className="size-3.5" />}
              {setup ? t("Create account") : t("Sign in")}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
