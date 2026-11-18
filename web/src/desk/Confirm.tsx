import { useEffect, useState } from "react"
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
import { setConfirm } from "./store"

interface Question {
  title: string
  message?: string
  confirmLabel?: string
  danger?: boolean
  resolve: (yes: boolean) => void
}

/** The desk's yes/no dialog. Views reach it through desk.confirm. */
export function ConfirmHost() {
  const [q, setQ] = useState<Question | null>(null)
  useEffect(() => {
    setConfirm((question) => new Promise<boolean>((resolve) => setQ({ ...question, resolve })))
  }, [])
  const answer = (yes: boolean) => {
    q?.resolve(yes)
    setQ(null)
  }
  return (
    <AlertDialog
      open={!!q}
      onOpenChange={(open) => {
        if (!open) answer(false)
      }}
    >
      <AlertDialogContent size="sm">
        <AlertDialogHeader>
          <AlertDialogTitle>{q?.title}</AlertDialogTitle>
          {q?.message && <AlertDialogDescription>{q.message}</AlertDialogDescription>}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant={q?.danger ? "destructive" : "default"} onClick={() => answer(true)}>
            {q?.confirmLabel ?? "OK"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
