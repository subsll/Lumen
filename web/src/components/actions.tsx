"use client"

import { useState } from "react"
import { useRouter } from "next/navigation"
import { ArchiveIcon, DownloadIcon, EyeIcon, Loader2Icon, SendIcon, ShieldCheckIcon } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { send } from "@/lib/client"
import { formatUTC } from "@/lib/format"
import type { Artifact, Note, Settings } from "@/lib/types"

export function ExportButton({ caseId }: { caseId: string }) {
  const router = useRouter()
  return (
    <Button variant="outline" asChild>
      <a
        href={`/api/cases/${caseId}/export`}
        onClick={() => {
          toast.info("Preparing export package", { description: "Evidence, manifest, custody log and report, with SHA256SUMS." })
          setTimeout(() => router.refresh(), 1500)
        }}
      >
        <ArchiveIcon /> Export
      </a>
    </Button>
  )
}

export function VerifyEvidenceButton({ evidenceId }: { evidenceId: string }) {
  const router = useRouter()
  const [busy, setBusy] = useState(false)
  return (
    <Button
      variant="outline"
      disabled={busy}
      onClick={async () => {
        setBusy(true)
        try {
          const r = await send<{ status: string; artifacts: { ok: boolean }[] }>("POST", `/api/evidence/${evidenceId}/verify`)
          if (r.status === "verified") toast.success("Integrity verified", { description: `All ${r.artifacts.length} files match their recorded SHA-256.` })
          else toast.error("Integrity check FAILED", { description: "One or more stored files do not match their recorded hash." })
          router.refresh()
        } catch (err) {
          toast.error((err as Error).message)
        } finally {
          setBusy(false)
        }
      }}
    >
      {busy ? <Loader2Icon className="animate-spin" /> : <ShieldCheckIcon />} Verify integrity
    </Button>
  )
}

const previewable = (mime: string) => /^(image\/(?!svg)|video\/|audio\/|application\/pdf|text\/plain)/.test(mime)

export function ArtifactActions({ artifact }: { artifact: Artifact }) {
  const router = useRouter()
  const url = `/api/artifacts/${artifact.id}/content`
  return (
    <div className="flex items-center gap-1">
      {previewable(artifact.mime_type) && (
        <Button variant="ghost" size="sm" asChild>
          <a href={`${url}?disposition=inline`} target="_blank" rel="noopener noreferrer" onClick={() => setTimeout(() => router.refresh(), 800)}>
            <EyeIcon /> View
          </a>
        </Button>
      )}
      <Button variant="ghost" size="sm" asChild>
        <a href={url} onClick={() => setTimeout(() => router.refresh(), 800)}>
          <DownloadIcon /> Download
        </a>
      </Button>
    </div>
  )
}

/** Shows an image preview only on request, because every view is a custody event. */
export function ArtifactPreview({ artifact }: { artifact: Artifact }) {
  const router = useRouter()
  const [shown, setShown] = useState(false)
  if (!artifact.mime_type.startsWith("image/") || artifact.mime_type === "image/svg+xml") return null
  if (!shown) {
    return (
      <button
        type="button"
        onClick={() => {
          setShown(true)
          setTimeout(() => router.refresh(), 800)
        }}
        className="flex h-48 w-full flex-col items-center justify-center gap-2 rounded-lg border border-dashed bg-muted/30 text-sm text-muted-foreground transition-colors hover:bg-muted/60"
      >
        <EyeIcon className="size-5" />
        <span>Show preview</span>
        <span className="text-xs">Viewing is recorded in the chain of custody</span>
      </button>
    )
  }
  return (
    <div className="max-h-[70vh] overflow-auto rounded-lg border bg-muted/30">
      {/* eslint-disable-next-line @next/next/no-img-element */}
      <img src={`/api/artifacts/${artifact.id}/content?disposition=inline`} alt={artifact.filename} className="w-full" />
    </div>
  )
}

export function NotesPanel({ caseId, notes, readOnly }: { caseId: string; notes: Note[]; readOnly?: boolean }) {
  const router = useRouter()
  const [body, setBody] = useState("")
  const [busy, setBusy] = useState(false)

  async function add(e: React.FormEvent) {
    e.preventDefault()
    if (!body.trim()) return
    setBusy(true)
    try {
      await send("POST", `/api/cases/${caseId}/notes`, { body })
      setBody("")
      router.refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      {!readOnly && (
        <form onSubmit={add} className="space-y-2">
          <Textarea
            value={body}
            onChange={(e) => setBody(e.target.value)}
            rows={3}
            placeholder="Record an observation, lead or decision. Notes are timestamped and cannot be edited afterwards."
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) add(e)
            }}
          />
          <div className="flex items-center justify-between">
            <span className="text-xs text-muted-foreground">⌘ + Enter to add</span>
            <Button type="submit" size="sm" disabled={busy || !body.trim()}>
              {busy ? <Loader2Icon className="animate-spin" /> : <SendIcon />} Add note
            </Button>
          </div>
        </form>
      )}
      {notes.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">No notes yet.</p>
      ) : (
        <ul className="space-y-3">
          {notes.map((n) => (
            <li key={n.id} className="rounded-lg border p-3">
              <p className="text-sm whitespace-pre-wrap">{n.body}</p>
              <p className="mt-2 text-xs text-muted-foreground">{formatUTC(n.created_at, true)}</p>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export function SettingsForm({ initial }: { initial: Settings }) {
  const router = useRouter()
  const [busy, setBusy] = useState(false)
  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const f = new FormData(e.currentTarget)
    setBusy(true)
    try {
      await send("PUT", "/api/settings", {
        investigator_name: f.get("investigator_name"),
        organization: f.get("organization"),
        case_prefix: f.get("case_prefix"),
      })
      toast.success("Settings saved")
      router.refresh()
    } catch (err) {
      toast.error((err as Error).message)
    } finally {
      setBusy(false)
    }
  }
  return (
    <form onSubmit={onSubmit} className="space-y-6">
      <FieldGroup>
        <Field>
          <FieldLabel htmlFor="investigator_name">Investigator name</FieldLabel>
          <Input id="investigator_name" name="investigator_name" defaultValue={initial.investigator_name} placeholder="Your full name" />
          <FieldDescription>Recorded as the actor on every custody event you create.</FieldDescription>
        </Field>
        <Field>
          <FieldLabel htmlFor="organization">Organisation</FieldLabel>
          <Input id="organization" name="organization" defaultValue={initial.organization} placeholder="Optional" />
        </Field>
        <Field>
          <FieldLabel htmlFor="case_prefix">Case reference prefix</FieldLabel>
          <Input id="case_prefix" name="case_prefix" defaultValue={initial.case_prefix} className="max-w-32 font-mono uppercase" maxLength={8} />
          <FieldDescription>New cases are numbered {initial.case_prefix || "LUM"}-{new Date().getFullYear()}-0001, -0002, …</FieldDescription>
        </Field>
      </FieldGroup>
      <Button type="submit" disabled={busy}>
        {busy && <Loader2Icon className="animate-spin" />} Save settings
      </Button>
    </form>
  )
}
