"use client"

import { useState } from "react"
import { useRouter } from "next/navigation"
import { CheckIcon, Loader2Icon, PencilIcon, PlusIcon } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { send } from "@/lib/client"
import { STATUSES } from "@/lib/format"
import type { Case, CaseStatus, Priority } from "@/lib/types"
import { cn } from "@/lib/utils"

const PRIORITIES: Priority[] = ["low", "medium", "high", "critical"]
const splitTags = (s: string) => s.split(",").map((t) => t.trim()).filter(Boolean)

function CaseFields({ initial }: { initial?: Case }) {
  return (
    <FieldGroup>
      <Field>
        <FieldLabel htmlFor="title">Title</FieldLabel>
        <Input id="title" name="title" required defaultValue={initial?.title} placeholder="e.g. Operation Nightjar" autoFocus />
      </Field>
      <Field>
        <FieldLabel htmlFor="description">Description</FieldLabel>
        <Textarea id="description" name="description" rows={3} defaultValue={initial?.description} placeholder="Scope, subject and objectives of the investigation" />
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field>
          <FieldLabel htmlFor="priority">Priority</FieldLabel>
          <Select name="priority" defaultValue={initial?.priority ?? "medium"}>
            <SelectTrigger id="priority" className="w-full capitalize">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PRIORITIES.map((p) => (
                <SelectItem key={p} value={p} className="capitalize">
                  {p}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field>
          <FieldLabel htmlFor="tags">Tags</FieldLabel>
          <Input id="tags" name="tags" defaultValue={initial?.tags.join(", ")} placeholder="fraud, telegram" />
        </Field>
      </div>
    </FieldGroup>
  )
}

export function NewCaseDialog({ trigger }: { trigger?: React.ReactNode }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    setBusy(true)
    try {
      const c = await send<Case>("POST", "/api/cases", {
        title: f.get("title"),
        description: f.get("description"),
        priority: f.get("priority"),
        tags: splitTags(String(f.get("tags") ?? "")),
        reference: f.get("reference") || undefined,
      })
      toast.success(`Case ${c.reference} opened`)
      setOpen(false)
      router.push(`/cases/${c.id}`)
      router.refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        {trigger ?? (
          <Button>
            <PlusIcon /> New case
          </Button>
        )}
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={onSubmit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>Open a new case</DialogTitle>
            <DialogDescription>A case reference is assigned automatically. Opening the case is the first entry in its chain of custody.</DialogDescription>
          </DialogHeader>
          <CaseFields />
          <Field>
            <FieldLabel htmlFor="reference">Custom reference (optional)</FieldLabel>
            <Input id="reference" name="reference" placeholder="Leave empty for automatic, e.g. LUM-2026-0001" />
            <FieldDescription>Use this if your organisation already assigned a case number.</FieldDescription>
          </Field>
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={busy}>
              {busy && <Loader2Icon className="animate-spin" />} Open case
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function EditCaseDialog({ initial }: { initial: Case }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    setBusy(true)
    try {
      await send("PATCH", `/api/cases/${initial.id}`, {
        title: f.get("title"),
        description: f.get("description"),
        priority: f.get("priority"),
        tags: splitTags(String(f.get("tags") ?? "")),
      })
      toast.success("Case updated")
      setOpen(false)
      router.refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline">
          <PencilIcon /> Edit
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={onSubmit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>Edit case details</DialogTitle>
            <DialogDescription>Changes are recorded in the chain of custody with their previous values.</DialogDescription>
          </DialogHeader>
          <CaseFields initial={initial} />
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={busy}>
              {busy && <Loader2Icon className="animate-spin" />} Save changes
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** Visual case progression. Clicking a stage asks for a reason, then moves the case. */
export function CaseProgress({ caseId, status }: { caseId: string; status: CaseStatus }) {
  const router = useRouter()
  const [target, setTarget] = useState<CaseStatus | null>(null)
  const [reason, setReason] = useState("")
  const [busy, setBusy] = useState(false)
  const current = STATUSES.findIndex((s) => s.value === status)

  async function confirm() {
    if (!target) return
    setBusy(true)
    try {
      await send("POST", `/api/cases/${caseId}/status`, { status: target, reason })
      toast.success(`Case moved to ${STATUSES.find((s) => s.value === target)?.label}`)
      setTarget(null)
      setReason("")
      router.refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <ol className="grid grid-cols-5 gap-1.5">
        {STATUSES.map((s, i) => {
          const done = i < current
          const isCurrent = i === current
          return (
            <li key={s.value}>
              <button
                type="button"
                disabled={isCurrent}
                onClick={() => setTarget(s.value)}
                className={cn(
                  "group flex w-full flex-col gap-1.5 rounded-md p-1.5 text-left transition-colors hover:bg-muted disabled:cursor-default disabled:hover:bg-transparent",
                )}
                title={isCurrent ? "Current stage" : `Move case to ${s.label}`}
              >
                <span
                  className={cn(
                    "h-1.5 rounded-full bg-muted",
                    done && "bg-primary/40",
                    isCurrent && "bg-primary",
                  )}
                />
                <span className={cn("flex items-center gap-1 text-xs", isCurrent ? "font-medium" : "text-muted-foreground")}>
                  {done && <CheckIcon className="size-3" />}
                  {s.label}
                </span>
              </button>
            </li>
          )
        })}
      </ol>
      <Dialog open={target !== null} onOpenChange={(o) => !o && setTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Move case to {STATUSES.find((s) => s.value === target)?.label}?</DialogTitle>
            <DialogDescription>{STATUSES.find((s) => s.value === target)?.description}. The change and your reason are recorded in the chain of custody.</DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="reason">Reason</FieldLabel>
            <Textarea id="reason" value={reason} onChange={(e) => setReason(e.target.value)} rows={3} placeholder="Why is the case moving to this stage?" />
            {(target === "closed" || target === "archived") && (
              <FieldDescription>New evidence cannot be added while a case is {target}.</FieldDescription>
            )}
          </Field>
          <DialogFooter>
            <Button variant="outline" onClick={() => setTarget(null)}>
              Cancel
            </Button>
            <Button onClick={confirm} disabled={busy}>
              {busy && <Loader2Icon className="animate-spin" />} Confirm
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
