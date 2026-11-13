import { ShieldAlertIcon, ShieldCheckIcon, ShieldQuestionIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { INTEGRITY_LABEL, PRIORITY_STYLE, STATUS_STYLE, statusLabel } from "@/lib/format"
import type { CaseStatus, IntegrityStatus, Priority } from "@/lib/types"
import { cn } from "@/lib/utils"

export function StatusBadge({ status, className }: { status: CaseStatus; className?: string }) {
  return (
    <Badge variant="outline" className={cn("font-medium", STATUS_STYLE[status], className)}>
      {statusLabel(status)}
    </Badge>
  )
}

export function PriorityLabel({ priority }: { priority: Priority }) {
  return <span className={cn("text-sm capitalize", PRIORITY_STYLE[priority])}>{priority}</span>
}

export function IntegrityBadge({ status }: { status: IntegrityStatus }) {
  const Icon = status === "verified" ? ShieldCheckIcon : status === "failed" ? ShieldAlertIcon : ShieldQuestionIcon
  return (
    <Badge
      variant="outline"
      className={cn(
        "gap-1",
        status === "verified" && "border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
        status === "failed" && "border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300",
        status === "unverified" && "text-muted-foreground",
      )}
    >
      <Icon className="size-3" />
      {INTEGRITY_LABEL[status]}
    </Badge>
  )
}

export function TagList({ tags }: { tags: string[] }) {
  if (!tags?.length) return <span className="text-sm text-muted-foreground">—</span>
  return (
    <div className="flex flex-wrap gap-1">
      {tags.map((t) => (
        <Badge key={t} variant="secondary" className="font-normal">
          {t}
        </Badge>
      ))}
    </div>
  )
}
