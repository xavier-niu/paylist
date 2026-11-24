import { useRef, useState, type ReactNode } from "react"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { t } from "@/lib/i18n"

interface Ask {
  title: string
  body?: ReactNode
  action: string
  /** "danger" (the default) for deleting and the like; "default" for a plain yes. */
  tone?: "danger" | "default"
  run: () => void | Promise<void>
}

/** A confirmation dialog: call ask({...}) and render the returned element. */
export function useConfirm(): [(a: Ask) => void, ReactNode] {
  const [pending, setPending] = useState<Ask | null>(null)
  const actionRef = useRef<HTMLButtonElement>(null)
  const el = (
    <AlertDialog open={!!pending} onOpenChange={(o) => !o && setPending(null)}>
      <AlertDialogContent
        onOpenAutoFocus={(e) => {
          // A plain yes (creating something) is the likely answer, so Enter gives it; for deleting, focus stays on Cancel.
          if (pending?.tone === "default") {
            e.preventDefault()
            actionRef.current?.focus()
          }
        }}
      >
        <AlertDialogHeader>
          <AlertDialogTitle>{pending?.title}</AlertDialogTitle>
          {pending?.body && <AlertDialogDescription>{pending.body}</AlertDialogDescription>}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("Cancel")}</AlertDialogCancel>
          <AlertDialogAction
            ref={actionRef}
            variant={pending?.tone === "default" ? "default" : "destructive"}
            onClick={() => {
              const p = pending
              setPending(null)
              void p?.run()
            }}
          >
            {pending?.action}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
  return [setPending, el]
}
