"use client"

import { useRef, useState } from "react"
import { useRouter } from "next/navigation"
import { CameraIcon, FileIcon, GlobeIcon, Loader2Icon, ShieldCheckIcon, UploadIcon, XIcon } from "lucide-react"
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
import { Progress } from "@/components/ui/progress"
import { Textarea } from "@/components/ui/textarea"
import { send, sha256File, uploadWithProgress } from "@/lib/client"
import { formatBytes } from "@/lib/format"
import type { Evidence } from "@/lib/types"
import { cn } from "@/lib/utils"

function nowLocal() {
  const d = new Date()
  d.setMinutes(d.getMinutes() - d.getTimezoneOffset())
  return d.toISOString().slice(0, 16)
}

type Stage = { file: string; phase: "hashing" | "uploading" | "done"; progress: number; hash?: string }

export function UploadEvidenceDialog({ caseId, disabled }: { caseId: string; disabled?: boolean }) {
  const router = useRouter()
  const inputRef = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(false)
  const [files, setFiles] = useState<File[]>([])
  const [dragging, setDragging] = useState(false)
  const [stage, setStage] = useState<Stage | null>(null)

  const busy = stage !== null && stage.phase !== "done"

  function addFiles(list: FileList | null) {
    if (!list) return
    setFiles((cur) => [...cur, ...Array.from(list)])
  }

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!files.length) return toast.error("Choose at least one file")
    const f = new FormData(e.currentTarget)
    const acquired = String(f.get("acquired_at") || "")
    const base = {
      description: String(f.get("description") ?? ""),
      source: String(f.get("source") ?? ""),
      location: String(f.get("location") ?? ""),
      tags: String(f.get("tags") ?? ""),
      acquired_at: acquired ? new Date(acquired).toISOString() : "",
    }
    let last: Evidence | null = null
    for (const file of files) {
      try {
        setStage({ file: file.name, phase: "hashing", progress: 0 })
        const hash = await sha256File(file, (p) => setStage({ file: file.name, phase: "hashing", progress: p }))
        setStage({ file: file.name, phase: "uploading", progress: 0, hash })
        last = await uploadWithProgress<Evidence>(
          `/api/cases/${caseId}/evidence`,
          { ...base, title: files.length === 1 ? String(f.get("title") ?? "") : "", client_sha256: hash },
          file,
          (p) => setStage({ file: file.name, phase: "uploading", progress: p, hash }),
        )
        toast.success(`${last.exhibit_ref} registered`, {
          description: `SHA-256 verified end to end: ${hash.slice(0, 16)}…`,
        })
      } catch (err) {
        toast.error(`${file.name}: ${(err as Error).message}`)
        setStage(null)
        router.refresh()
        return
      }
    }
    setStage(null)
    setFiles([])
    setOpen(false)
    router.refresh()
    if (files.length === 1 && last) router.push(`/evidence/${last.id}`)
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && setOpen(o)}>
      <DialogTrigger asChild>
        <Button disabled={disabled}>
          <UploadIcon /> Add evidence
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-xl">
        <form onSubmit={onSubmit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>Register evidence</DialogTitle>
            <DialogDescription>
              Files are hashed in your browser before upload, hashed again by the vault, and only registered if the two SHA-256 values match.
            </DialogDescription>
          </DialogHeader>

          <div
            onDragOver={(e) => {
              e.preventDefault()
              setDragging(true)
            }}
            onDragLeave={() => setDragging(false)}
            onDrop={(e) => {
              e.preventDefault()
              setDragging(false)
              addFiles(e.dataTransfer.files)
            }}
            onClick={() => inputRef.current?.click()}
            className={cn(
              "flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border border-dashed p-6 text-center transition-colors hover:bg-muted/50",
              dragging && "border-primary bg-primary/5",
            )}
          >
            <UploadIcon className="size-5 text-muted-foreground" />
            <div className="text-sm">
              <span className="font-medium">Drop files here</span> or click to browse
            </div>
            <div className="text-xs text-muted-foreground">Any file type and size. Each file becomes its own exhibit.</div>
            <input ref={inputRef} type="file" multiple hidden onChange={(e) => addFiles(e.target.files)} />
          </div>

          {files.length > 0 && (
            <ul className="max-h-32 space-y-1 overflow-auto">
              {files.map((file, i) => (
                <li key={`${file.name}-${i}`} className="flex items-center gap-2 rounded-md border px-2 py-1.5 text-sm">
                  <FileIcon className="size-4 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate">{file.name}</span>
                  <span className="text-xs text-muted-foreground">{formatBytes(file.size)}</span>
                  {!busy && (
                    <Button type="button" variant="ghost" size="icon-xs" onClick={() => setFiles(files.filter((_, j) => j !== i))}>
                      <XIcon />
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          )}

          <FieldGroup>
            {files.length <= 1 && (
              <Field>
                <FieldLabel htmlFor="ev-title">Title</FieldLabel>
                <Input id="ev-title" name="title" placeholder="Defaults to the filename" />
              </Field>
            )}
            <div className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="ev-source">Source</FieldLabel>
                <Input id="ev-source" name="source" placeholder="URL, platform, device, person…" />
              </Field>
              <Field>
                <FieldLabel htmlFor="ev-acquired">Acquired at (your local time)</FieldLabel>
                <Input id="ev-acquired" name="acquired_at" type="datetime-local" defaultValue={nowLocal()} />
              </Field>
            </div>
            <Field>
              <FieldLabel htmlFor="ev-desc">Description</FieldLabel>
              <Textarea id="ev-desc" name="description" rows={2} placeholder="What is this and why is it relevant?" />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="ev-location">Original location</FieldLabel>
                <Input id="ev-location" name="location" placeholder="e.g. Seized laptop, /Users/x/Downloads" />
              </Field>
              <Field>
                <FieldLabel htmlFor="ev-tags">Tags</FieldLabel>
                <Input id="ev-tags" name="tags" placeholder="comma separated" />
              </Field>
            </div>
          </FieldGroup>

          {stage && (
            <div className="space-y-2 rounded-lg border bg-muted/40 p-3">
              <div className="flex items-center gap-2 text-sm">
                {stage.phase === "hashing" ? <ShieldCheckIcon className="size-4 text-primary" /> : <UploadIcon className="size-4 text-primary" />}
                <span className="min-w-0 flex-1 truncate">
                  {stage.phase === "hashing" ? "Computing SHA-256 locally" : "Uploading to the vault"} · {stage.file}
                </span>
                <span className="text-xs text-muted-foreground tabular-nums">{Math.round(stage.progress * 100)}%</span>
              </div>
              <Progress value={stage.progress * 100} />
              {stage.hash && <code className="block truncate font-mono text-[11px] text-muted-foreground">{stage.hash}</code>}
            </div>
          )}

          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline" disabled={busy}>
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={busy || !files.length}>
              {busy && <Loader2Icon className="animate-spin" />}
              Register {files.length > 1 ? `${files.length} files` : "evidence"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function CaptureDialog({ caseId, disabled }: { caseId: string; disabled?: boolean }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    setBusy(true)
    try {
      const ev = await send<Evidence>("POST", `/api/cases/${caseId}/captures`, {
        url: f.get("url"),
        title: f.get("title"),
        description: f.get("description"),
        tags: String(f.get("tags") ?? "").split(",").map((t) => t.trim()).filter(Boolean),
      })
      toast.success(`${ev.exhibit_ref} captured`, { description: ev.source })
      setOpen(false)
      router.push(`/evidence/${ev.id}`)
      router.refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && setOpen(o)}>
      <DialogTrigger asChild>
        <Button variant="outline" disabled={disabled}>
          <CameraIcon /> Capture URL
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={onSubmit} className="grid gap-6">
          <DialogHeader>
            <DialogTitle>Capture a web page</DialogTitle>
            <DialogDescription>
              Lumen opens the page in a fresh, isolated browser and preserves a full-page screenshot, the rendered HTML, an MHTML archive,
              HTTP headers, redirects, TLS certificate and DNS records. Everything is hashed and sealed as one exhibit.
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="cap-url">URL</FieldLabel>
              <div className="relative">
                <GlobeIcon className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
                <Input id="cap-url" name="url" type="url" required placeholder="https://" className="pl-8" autoFocus />
              </div>
            </Field>
            <Field>
              <FieldLabel htmlFor="cap-title">Title</FieldLabel>
              <Input id="cap-title" name="title" placeholder="Defaults to the page title" />
            </Field>
            <Field>
              <FieldLabel htmlFor="cap-desc">Description</FieldLabel>
              <Textarea id="cap-desc" name="description" rows={2} placeholder="Why this page matters" />
            </Field>
            <Field>
              <FieldLabel htmlFor="cap-tags">Tags</FieldLabel>
              <Input id="cap-tags" name="tags" placeholder="comma separated" />
              <FieldDescription>Captures run without your cookies or logins.</FieldDescription>
            </Field>
          </FieldGroup>
          {busy && (
            <div className="flex items-center gap-2 rounded-lg border bg-muted/40 p-3 text-sm">
              <Loader2Icon className="size-4 animate-spin text-primary" />
              Launching isolated browser and capturing. This usually takes 5–30 seconds.
            </div>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline" disabled={busy}>
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={busy}>
              {busy ? <Loader2Icon className="animate-spin" /> : <CameraIcon />} Capture
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
