import { cn } from "cn"

import { t } from "@/lib/i18n"

/**
 * A small busy sign, for a button or a step in progress: a faint ring with an
 * arc going round it. (Pages and panels show skeletons instead.)
 */
function Spinner({ className, ...props }: React.ComponentProps<"svg">) {
  return (
    <svg
      data-slot="spinner"
      role="status"
      aria-label={t("Loading")}
      viewBox="0 0 16 16"
      fill="none"
      className={cn("size-4 shrink-0 animate-[spin-arc_.75s_linear_infinite] motion-reduce:animate-[spin-arc_2.4s_linear_infinite]", className)}
      {...props}
    >
      <circle cx="8" cy="8" r="6.25" stroke="currentColor" strokeWidth="1.75" opacity=".2" />
      <path d="M8 1.75A6.25 6.25 0 0 1 14.25 8" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" />
    </svg>
  )
}

export { Spinner }
