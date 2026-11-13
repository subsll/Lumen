import Link from "next/link"
import { FileIcon, GlobeIcon } from "lucide-react"

import { IntegrityBadge } from "@/components/badges"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatBytes, formatUTC, shortHash } from "@/lib/format"
import type { Evidence } from "@/lib/types"

export function EvidenceTable({ items, showCase = false }: { items: Evidence[]; showCase?: boolean }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="pl-4">Exhibit</TableHead>
          <TableHead>Title</TableHead>
          {showCase && <TableHead className="hidden md:table-cell">Case</TableHead>}
          <TableHead className="hidden lg:table-cell">SHA-256</TableHead>
          <TableHead className="hidden sm:table-cell">Integrity</TableHead>
          <TableHead className="hidden text-right md:table-cell">Size</TableHead>
          <TableHead className="hidden pr-4 text-right md:table-cell">Acquired</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((e) => (
          <TableRow key={e.id} className="relative">
            <TableCell className="pl-4 font-mono text-xs whitespace-nowrap">
              <Link href={`/evidence/${e.id}`} className="after:absolute after:inset-0">
                E{String(e.number).padStart(3, "0")}
              </Link>
            </TableCell>
            <TableCell className="max-w-xs">
              <div className="flex items-center gap-2">
                {e.kind === "web_capture" ? (
                  <GlobeIcon className="size-4 shrink-0 text-primary" />
                ) : (
                  <FileIcon className="size-4 shrink-0 text-muted-foreground" />
                )}
                <span className="truncate font-medium">{e.title}</span>
              </div>
              {e.source && <div className="truncate pl-6 text-xs text-muted-foreground">{e.source}</div>}
            </TableCell>
            {showCase && (
              <TableCell className="hidden font-mono text-xs text-muted-foreground md:table-cell">{e.case_reference}</TableCell>
            )}
            <TableCell className="hidden font-mono text-xs text-muted-foreground lg:table-cell" title={e.primary_sha256}>
              {shortHash(e.primary_sha256, 16)}
            </TableCell>
            <TableCell className="hidden sm:table-cell">
              <IntegrityBadge status={e.integrity_status} />
            </TableCell>
            <TableCell className="hidden text-right text-xs tabular-nums md:table-cell">
              {formatBytes(e.total_bytes)}
              {e.artifact_count > 1 && <div className="text-muted-foreground">{e.artifact_count} files</div>}
            </TableCell>
            <TableCell className="hidden pr-4 text-right text-xs text-muted-foreground md:table-cell">{formatUTC(e.acquired_at)}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
