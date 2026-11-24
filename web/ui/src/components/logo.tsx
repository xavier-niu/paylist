import { cn } from "@/lib/utils"

/**
 * The mark: a dollar sign over the ledger's three lines, faint behind it, in
 * a rounded square. A halo in the square's color cuts the lines where the
 * sign crosses them.
 */
export function Logo({ className }: { className?: string }) {
  const sign = (
    <>
      <path d="M12 2v20" />
      <path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" />
    </>
  )
  return (
    <svg viewBox="0 0 24 24" className={cn("shrink-0", className)} aria-hidden="true">
      <rect x="2" y="2" width="20" height="20" rx="5" fill="var(--ink)" />
      <rect x="5.5" y="6.6" width="13" height="2.2" rx="1.1" fill="var(--paper)" opacity=".32" />
      <rect x="5.5" y="10.9" width="9" height="2.2" rx="1.1" fill="var(--paper)" opacity=".22" />
      <rect x="5.5" y="15.2" width="11" height="2.2" rx="1.1" fill="var(--paper)" opacity=".22" />
      <g transform="translate(12 12) scale(.6) translate(-12 -12)" fill="none" strokeLinecap="round" strokeLinejoin="round">
        <g stroke="var(--ink)" strokeWidth="4.4">{sign}</g>
        <g stroke="var(--paper)" strokeWidth="2.6">{sign}</g>
      </g>
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
