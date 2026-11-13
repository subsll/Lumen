import Link from "next/link"
import {
  ArchiveIcon,
  CameraIcon,
  DownloadIcon,
  EyeIcon,
  FilePlusIcon,
  FolderPlusIcon,
  ListTreeIcon,
  type LucideIcon,
  PencilIcon,
  ShieldAlertIcon,
  ShieldCheckIcon,
  SettingsIcon,
  StickyNoteIcon,
  WorkflowIcon,
  XCircleIcon,
} from "lucide-react"

import { actionLabel, formatUTC, shortHash, statusLabel } from "@/lib/format"
import type { CaseStatus, CustodyEvent } from "@/lib/types"
import { cn } from "@/lib/utils"

const ICONS: Record<string, { icon: LucideIcon; tone?: string }> = {
  "case.opened": { icon: FolderPlusIcon, tone: "text-primary" },
  "case.updated": { icon: PencilIcon },
  "case.status_changed": { icon: WorkflowIcon, tone: "text-blue-600 dark:text-blue-400" },
  "case.note_added": { icon: StickyNoteIcon },
  "case.exported": { icon: ArchiveIcon, tone: "text-violet-600 dark:text-violet-400" },
  "evidence.registered": { icon: FilePlusIcon, tone: "text-primary" },
  "evidence.captured": { icon: CameraIcon, tone: "text-primary" },
  "evidence.metadata_updated": { icon: PencilIcon },
  "evidence.integrity_verified": { icon: ShieldCheckIcon, tone: "text-emerald-600 dark:text-emerald-400" },
  "evidence.integrity_failed": { icon: ShieldAlertIcon, tone: "text-red-600 dark:text-red-400" },
  "evidence.upload_rejected": { icon: XCircleIcon, tone: "text-red-600 dark:text-red-400" },
  "artifact.viewed": { icon: EyeIcon },
  "artifact.downloaded": { icon: DownloadIcon },
  "artifact.metadata_extracted": { icon: ListTreeIcon },
  "settings.updated": { icon: SettingsIcon },
}

function summary(e: CustodyEvent): string | null {
  const d = e.details as Record<string, unknown>
  switch (e.action) {
    case "case.status_changed":
      return `${statusLabel(d.from as CaseStatus)} → ${statusLabel(d.to as CaseStatus)}${d.reason ? `: ${d.reason}` : ""}`
    case "case.note_added":
      return String(d.body ?? "")
    case "case.opened":
      return `${d.reference} · ${d.title}`
    case "case.updated":
    case "evidence.metadata_updated":
    case "settings.updated":
      return `Changed: ${Object.keys(d).join(", ")}`
    case "evidence.captured":
      return String(d.final_url ?? "")
    case "evidence.registered": {
      const a = (d.artifacts as { filename: string; sha256: string; client_hash_match?: boolean }[])?.[0]
      return a ? `${a.filename} · SHA-256 ${shortHash(a.sha256, 16)}${a.client_hash_match ? " · browser hash matched" : ""}` : null
    }
    case "artifact.viewed":
    case "artifact.downloaded":
      return String(d.filename ?? "")
    case "artifact.metadata_extracted":
      return d.status === "extracted"
        ? `${d.filename} · ${d.field_count} ${d.field_count === 1 ? "field" : "fields"} via ${d.tool} ${d.tool_version}`
        : `${d.filename} · ${d.status}${d.error ? `: ${d.error}` : ""}`
    case "evidence.integrity_verified":
    case "evidence.integrity_failed": {
      const arts = (d.artifacts as { ok: boolean }[]) ?? []
      return `${arts.filter((a) => a.ok).length}/${arts.length} files match their SHA-256`
    }
    case "case.exported":
      return `${d.evidence_count} evidence items, ${d.artifact_count} files`
    case "evidence.upload_rejected":
      return String(d.filename ?? "")
    default:
      return null
  }
}

export function CustodyTimeline({
  events,
  showCase = false,
  showEvidence = true,
}: {
  events: CustodyEvent[]
  showCase?: boolean
  showEvidence?: boolean
}) {
  if (!events.length) return <p className="py-6 text-center text-sm text-muted-foreground">No custody events yet.</p>
  return (
    <ol className="relative">
      {events.map((e, i) => {
        const meta = ICONS[e.action] ?? { icon: WorkflowIcon }
        const Icon = meta.icon
        const text = summary(e)
        return (
          <li key={e.id} className="relative flex gap-3 pb-5 last:pb-0">
            {i < events.length - 1 && <span className="absolute top-8 left-[15px] h-[calc(100%-2rem)] w-px bg-border" />}
            <span className="relative z-0 flex size-8 shrink-0 items-center justify-center rounded-full border bg-background">
              <Icon className={cn("size-4 text-muted-foreground", meta.tone)} />
            </span>
            <div className="min-w-0 flex-1 pt-1">
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                <span className="text-sm font-medium">{actionLabel(e.action)}</span>
                {showEvidence && e.exhibit_ref && e.evidence_id && (
                  <Link href={`/evidence/${e.evidence_id}`} className="font-mono text-xs text-primary hover:underline">
                    {e.exhibit_ref}
                  </Link>
                )}
                {showCase && e.case_reference && e.case_id && !e.exhibit_ref && (
                  <Link href={`/cases/${e.case_id}`} className="font-mono text-xs text-primary hover:underline">
                    {e.case_reference}
                  </Link>
                )}
              </div>
              {text && <p className="mt-0.5 line-clamp-2 text-sm break-words text-muted-foreground">{text}</p>}
              <div className="mt-1 flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                <span>{formatUTC(e.occurred_at, true)}</span>
                <span>{e.actor}</span>
                <span className="font-mono" title={`Event #${e.seq}\nhash ${e.hash}\nprev ${e.prev_hash}`}>
                  #{e.seq} · {shortHash(e.hash, 10)}
                </span>
              </div>
              <details className="group mt-1">
                <summary className="cursor-pointer list-none text-xs text-muted-foreground hover:text-foreground">
                  <span className="group-open:hidden">Show record</span>
                  <span className="hidden group-open:inline">Hide record</span>
                </summary>
                <pre className="mt-2 max-h-72 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-[11px] leading-relaxed">
                  {JSON.stringify(
                    { seq: e.seq, id: e.id, action: e.action, actor: e.actor, occurred_at: e.occurred_at, details: e.details, prev_hash: e.prev_hash, hash: e.hash },
                    null,
                    2,
                  )}
                </pre>
              </details>
            </div>
          </li>
        )
      })}
    </ol>
  )
}
