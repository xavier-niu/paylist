import { today, type Option } from "@/lib/api"

/** How many months without an entry make a type or card inactive. */
export const INACTIVE_MONTHS = 12

/**
 * Whether a type or card is inactive: used before, but by no entry in the
 * last twelve months. An inactive one is marked in Ledger settings, and left
 * out of the lists elsewhere until a search finds it.
 */
export function isInactive(o: Option, now = new Date()): boolean {
  if (!o.uses || !o.last_used) return false
  const d = new Date(now)
  d.setMonth(d.getMonth() - INACTIVE_MONTHS)
  return o.last_used < today(d)
}

/**
 * Whether two names share a word: one word starting the other ("ski" and
 * "skiing"), three letters at least, ignoring case. For a new name, an
 * inactive option that may be the same thing.
 */
export function sharesWord(a: string, b: string): boolean {
  const words = (s: string) => s.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter((w) => w.length >= 3)
  const wb = words(b)
  return words(a).some((x) => wb.some((y) => x.startsWith(y) || y.startsWith(x)))
}

const lastCardKey = (ledger: number) => `paylist-last-card-${ledger}`

/**
 * The card a new entry starts with: the one last added with in this ledger
 * (remembered in this browser), or, before that, the one most recently used
 * there; "" for none.
 */
export function lastCard(ledger: number, cards: Option[]): string {
  try {
    const kept = localStorage.getItem(lastCardKey(ledger))
    if (kept && cards.some((c) => c.name === kept)) return kept
  } catch {
    /* no storage here */
  }
  let best = ""
  let latest = ""
  for (const c of cards) {
    if (c.last_used && c.last_used > latest && !isInactive(c)) {
      best = c.name
      latest = c.last_used
    }
  }
  return best
}

/** Remembers the card an entry was just added with, for the next one. */
export function rememberCard(ledger: number, card: string) {
  if (!card) return
  try {
    localStorage.setItem(lastCardKey(ledger), card)
  } catch {
    /* no storage here */
  }
}
