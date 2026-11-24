// Files sent to the agent with a message: what it takes, how big, and reading
// them for the request. The server checks the same rules (agentchat.go).
import { t } from "@/lib/i18n"

export interface Attachment {
  name: string
  type: string // MIME type
  data: string // base64
}

export const MAX_FILES = 5
const MAX_IMAGE = 5 << 20
const MAX_FILE = 10 << 20
const MAX_TOTAL = 25 << 20

const BY_EXTENSION: Record<string, string> = {
  png: "image/png", jpg: "image/jpeg", jpeg: "image/jpeg", gif: "image/gif", webp: "image/webp",
  pdf: "application/pdf",
  txt: "text/plain", csv: "text/csv", tsv: "text/tab-separated-values", md: "text/markdown", markdown: "text/markdown", json: "application/json",
}
const TYPES = new Set(Object.values(BY_EXTENSION))

/** What the file picker offers. */
export const ACCEPT = [...TYPES, ...Object.keys(BY_EXTENSION).map((e) => "." + e)].join(",")

/** The file's type as the agent knows it, or null if it doesn't take it. */
function typeOf(file: File): string | null {
  const own = file.type.toLowerCase()
  if (TYPES.has(own)) return own
  const ext = file.name.split(".").pop()?.toLowerCase() ?? ""
  return BY_EXTENSION[ext] ?? null
}

export function isImage(a: { type: string }): boolean {
  return a.type.startsWith("image/")
}

/** 120 KB, 1.4 MB */
export function sizeLabel(bytes: number): string {
  return bytes < 1 << 20 ? `${Math.max(1, Math.round(bytes / 1024))} KB` : `${(bytes / (1 << 20)).toFixed(1)} MB`
}

function base64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader()
    r.onload = () => resolve(String(r.result).replace(/^data:[^,]*,/, ""))
    r.onerror = () => reject(r.error)
    r.readAsDataURL(file)
  })
}

/**
 * Reads files to attach next to those already attached (sizes in bytes),
 * keeping what fits the rules; the rest come back as messages to show.
 */
export async function readAttachments(files: File[], have: number[]): Promise<{ ok: (Attachment & { size: number })[]; errors: string[] }> {
  const ok: (Attachment & { size: number })[] = []
  const errors: string[] = []
  let total = have.reduce((s, n) => s + n, 0)
  for (const file of files) {
    const type = typeOf(file)
    if (!type) {
      errors.push(t("{name}: the agent takes images, PDFs and text files.", { name: file.name }))
      continue
    }
    const limit = type.startsWith("image/") ? MAX_IMAGE : MAX_FILE
    if (file.size > limit) {
      errors.push(t("{name} is larger than {n} MB.", { name: file.name, n: limit >> 20 }))
      continue
    }
    if (have.length + ok.length >= MAX_FILES) {
      errors.push(t("Attach at most {n} files.", { n: MAX_FILES }))
      break
    }
    if (total + file.size > MAX_TOTAL) {
      errors.push(t("Files together can be at most {n} MB.", { n: MAX_TOTAL >> 20 }))
      break
    }
    total += file.size
    ok.push({ name: file.name || "file", type, data: await base64(file), size: file.size })
  }
  return { ok, errors }
}
