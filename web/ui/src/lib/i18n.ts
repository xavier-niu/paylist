// English and Chinese. The English text is the key: t("New entry") gives
// "新记录" in Chinese, and English (or anything missing) passes through.
import { createElement, Fragment, useSyncExternalStore, type ReactNode } from "react"

import { ZH } from "@/lib/zh"

export type Lang = "en" | "zh"
/** What the user chose: a language, or "system" to follow the browser. */
export type LangPref = "system" | Lang

const KEY = "paylist-lang"

function browserLangs(): string[] {
  return (navigator.languages?.length ? navigator.languages : [navigator.language || "en"]).map((l) => l.toLowerCase())
}

function resolve(pref: LangPref): Lang {
  if (pref === "en" || pref === "zh") return pref
  return browserLangs()[0]?.startsWith("zh") ? "zh" : "en"
}

function storedPref(): LangPref {
  try {
    const v = localStorage.getItem(KEY)
    if (v === "en" || v === "zh" || v === "system") return v
  } catch {
    /* no storage */
  }
  return "system"
}

let current: Lang = resolve(storedPref())
const listeners = new Set<() => void>()

function apply() {
  document.documentElement.lang = current === "zh" ? "zh-CN" : "en"
}
apply()

/** Switches the language (and remembers it on this browser, for the sign-in page). */
export function setLanguage(pref: LangPref) {
  try {
    localStorage.setItem(KEY, pref)
  } catch {
    /* no storage */
  }
  const next = resolve(pref)
  if (next === current) return
  current = next
  apply()
  listeners.forEach((l) => l())
}

/** The language in use; components calling it re-render when it changes. */
export function useLang(): Lang {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => current,
  )
}

export function lang(): Lang {
  return current
}

/** The locale for dates and numbers: the browser's own variant of the language when it has one. */
export function locale(): string {
  const own = browserLangs().find((l) => l.startsWith(current))
  return own ?? (current === "zh" ? "zh-CN" : "en-US")
}

/** Translates English text, filling {name} placeholders. */
export function t(text: string, vars?: Record<string, string | number>): string {
  let out = current === "zh" ? (ZH[text] ?? text) : text
  if (vars) out = out.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m))
  return out
}

/** t() with a count: tn(3, "{n} entry", "{n} entries"). */
export function tn(n: number, one: string, other: string, vars?: Record<string, string | number>): string {
  return t(n === 1 ? one : other, { n, ...vars })
}

/** t() where placeholders are elements: a link or code inside a sentence. */
export function tParts(text: string, parts: Record<string, ReactNode>): ReactNode {
  const pieces = t(text).split(/\{(\w+)\}/)
  return createElement(Fragment, null, ...pieces.map((p, i) => (i % 2 ? (parts[p] ?? `{${p}}`) : p)))
}
