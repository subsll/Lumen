import type { CaseStatus, IntegrityStatus, Priority } from "./types"

export function formatBytes(n: number): string {
  if (!n) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  const v = n / 1024 ** i
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

const pad = (n: number) => String(n).padStart(2, "0")

/** Forensic timestamps are always shown in UTC so they read the same everywhere. */
export function formatUTC(iso: string | null | undefined, withSeconds = false): string {
  if (!iso) return "—"
  const d = new Date(iso)
  const base = `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`
  return `${base}${withSeconds ? `:${pad(d.getUTCSeconds())}` : ""} UTC`
}

export function formatDate(iso: string): string {
  const d = new Date(iso)
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
}

export function shortHash(h: string, n = 12): string {
  return h ? `${h.slice(0, n)}…` : ""
}

export const STATUSES: { value: CaseStatus; label: string; description: string }[] = [
  { value: "intake", label: "Intake", description: "Case opened, scope being defined" },
  { value: "active", label: "Active", description: "Evidence is being collected" },
  { value: "review", label: "Review", description: "Findings under review" },
  { value: "closed", label: "Closed", description: "Investigation concluded" },
  { value: "archived", label: "Archived", description: "Retained, read-only" },
]

export const STATUS_STYLE: Record<CaseStatus, string> = {
  intake: "bg-slate-500/10 text-slate-700 dark:text-slate-300 border-slate-500/20",
  active: "bg-blue-500/10 text-blue-700 dark:text-blue-300 border-blue-500/20",
  review: "bg-amber-500/10 text-amber-700 dark:text-amber-300 border-amber-500/20",
  closed: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border-emerald-500/20",
  archived: "bg-zinc-500/10 text-zinc-600 dark:text-zinc-400 border-zinc-500/20",
}

export const PRIORITY_STYLE: Record<Priority, string> = {
  low: "text-muted-foreground",
  medium: "text-foreground",
  high: "text-orange-600 dark:text-orange-400",
  critical: "text-red-600 dark:text-red-400",
}

export const INTEGRITY_LABEL: Record<IntegrityStatus, string> = {
  verified: "Verified",
  unverified: "Not yet verified",
  failed: "Integrity failed",
}

export const ACTION_LABEL: Record<string, string> = {
  "case.opened": "Case opened",
  "case.updated": "Case details updated",
  "case.status_changed": "Status changed",
  "case.note_added": "Note added",
  "case.exported": "Case exported",
  "evidence.registered": "Evidence registered",
  "evidence.captured": "Web page captured",
  "evidence.metadata_updated": "Evidence details updated",
  "evidence.integrity_verified": "Integrity verified",
  "evidence.integrity_failed": "Integrity check failed",
  "evidence.upload_rejected": "Upload rejected: hash mismatch",
  "artifact.viewed": "Artifact viewed",
  "artifact.downloaded": "Artifact downloaded",
  "artifact.metadata_extracted": "File metadata extracted",
  "settings.updated": "Settings updated",
}

export function actionLabel(action: string): string {
  return ACTION_LABEL[action] ?? action
}

export function statusLabel(s: CaseStatus): string {
  return STATUSES.find((x) => x.value === s)?.label ?? s
}

export function roleLabel(role: string): string {
  return (
    { original: "Original", screenshot: "Screenshot", html: "Rendered HTML", mhtml: "MHTML archive", dns: "DNS records", metadata: "Capture metadata" }[
      role
    ] ?? role
  )
}
