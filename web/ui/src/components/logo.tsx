import { cn } from "@/lib/utils"

/** The mark: a small table, its header row filled. */
export function Logo({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={cn("shrink-0", className)} aria-hidden="true">
      <rect x="2" y="2" width="20" height="20" rx="5" fill="var(--ink)" />
      <rect x="6" y="7" width="12" height="2.4" rx="1.2" fill="var(--paper)" />
      <rect x="6" y="11.4" width="7.5" height="1.8" rx=".9" fill="var(--paper)" opacity=".55" />
      <rect x="6" y="15" width="9.5" height="1.8" rx=".9" fill="var(--paper)" opacity=".55" />
    </svg>
  )
}

export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-2 text-[15px] font-semibold", className)}>
      <Logo className="size-[22px]" />
      Paylist
    </span>
  )
}
