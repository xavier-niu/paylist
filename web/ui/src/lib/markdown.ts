import DOMPurify from "dompurify"
import { marked } from "marked"

/** Markdown (the agent's replies) as safe HTML. Links open in a new tab. */
export function renderMarkdown(src: string): string {
  return DOMPurify.sanitize(marked.parse(src, { gfm: true, breaks: true, async: false }) as string)
}

DOMPurify.addHook("afterSanitizeAttributes", (node) => {
  if (node.tagName === "A") {
    node.setAttribute("target", "_blank")
    node.setAttribute("rel", "noopener noreferrer")
  }
  if (node.tagName === "IMG") node.setAttribute("referrerpolicy", "no-referrer")
})
