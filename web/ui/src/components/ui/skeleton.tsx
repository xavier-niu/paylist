import { cn } from "cn"
import type { ReactNode } from "react"

import { t } from "@/lib/i18n"

/** A placeholder bar or block in the shape of what's loading. */
function Skeleton({ className, ...props }: React.ComponentProps<"span">) {
  return <span aria-hidden="true" data-slot="skeleton" className={cn("skeleton h-3.5", className)} {...props} />
}

/** A region of placeholders: announced as loading, and shown only after a beat. */
function Loading({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div role="status" aria-label={t("Loading")} className={cn("loading-in", className)}>
      {children}
    </div>
  )
}

export { Loading, Skeleton }
