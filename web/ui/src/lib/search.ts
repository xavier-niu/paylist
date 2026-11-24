// Matching what's typed against names (entries, types, cards): by plain text,
// ignoring case, and for Chinese names by pinyin too, full or initials, so
// "kf" and "kafei" both find 咖啡. The pinyin data is large, so it loads in the
// background after the app starts; until then matching is by plain text.

type PinyinMatch = (text: string, pinyin: string, options?: { continuous?: boolean }) => number[] | null

let pinyinMatch: PinyinMatch | null = null
let loading: Promise<void> | null = null

export function loadPinyin(): Promise<void> {
  loading ??= import("pinyin-pro")
    .then((m) => {
      pinyinMatch = m.match as PinyinMatch
    })
    .catch(() => {})
  return loading
}

const HAN = /[㐀-鿿]/

/** Where the query matches the text: -1 if it doesn't, otherwise where the match starts (0 at the start). */
export function matchAt(text: string, query: string): number {
  const q = query.trim().toLowerCase()
  if (!q) return 0
  const i = text.toLowerCase().indexOf(q)
  if (i >= 0) return i
  const letters = q.replace(/[\s']/g, "")
  if (pinyinMatch && HAN.test(text) && /^[a-z]+$/.test(letters)) {
    const hit = pinyinMatch(text, letters, { continuous: true })
    if (hit && hit.length > 0) return hit[0]
  }
  return -1
}

export function matches(text: string, query: string): boolean {
  return matchAt(text, query) >= 0
}
