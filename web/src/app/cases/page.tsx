import type { Metadata } from "next"
import Link from "next/link"
import { FolderOpenIcon } from "lucide-react"

import { PriorityLabel, StatusBadge, TagList } from "@/components/badges"
import { NewCaseDialog } from "@/components/case-dialogs"
import { PageHeader } from "@/components/page-header"
import { Pagination, readPage } from "@/components/pagination"
import { Card } from "@/components/ui/card"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { api } from "@/lib/api"
import { formatBytes, formatUTC, STATUSES } from "@/lib/format"
import type { CasePage } from "@/lib/types"
import { cn } from "@/lib/utils"

export const metadata: Metadata = { title: "Cases" }

export default async function CasesPage({ searchParams }: PageProps<"/cases">) {
  const sp = await searchParams
  const filter = typeof sp.status === "string" ? sp.status : ""
  const { page, size, query } = readPage(sp)
  const data = await api<CasePage>(`/api/cases?${query}${filter ? `&status=${encodeURIComponent(filter)}` : ""}`)
  const cases = data.items
  const count = (s: string) => data.counts[s as keyof typeof data.counts] ?? 0

  return (
    <>
      <PageHeader title="Cases" description="Every investigation, from intake to archive." actions={<NewCaseDialog />} />

      <nav className="flex flex-wrap gap-1 border-b">
        {[{ value: "", label: "All" }, ...STATUSES].map((s) => (
          <Link
            key={s.value}
            href={s.value ? `/cases?status=${s.value}` : "/cases"}
            className={cn(
              "-mb-px border-b-2 px-3 py-2 text-sm transition-colors",
              filter === s.value ? "border-primary font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
            )}
          >
            {s.label}
            <span className="ml-1.5 text-xs text-muted-foreground tabular-nums">{s.value ? count(s.value) : data.all}</span>
          </Link>
        ))}
      </nav>

      {cases.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FolderOpenIcon />
            </EmptyMedia>
            <EmptyTitle>{filter ? "No cases in this stage" : "No cases yet"}</EmptyTitle>
            <EmptyDescription>Open a case to start collecting and preserving evidence.</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <NewCaseDialog />
          </EmptyContent>
        </Empty>
      ) : (
        <Card className="py-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Reference</TableHead>
                <TableHead>Title</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Priority</TableHead>
                <TableHead className="hidden lg:table-cell">Tags</TableHead>
                <TableHead className="text-right">Evidence</TableHead>
                <TableHead className="hidden pr-4 text-right md:table-cell">Updated</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {cases.map((c) => (
                <TableRow key={c.id} className="relative">
                  <TableCell className="pl-4 font-mono text-xs">
                    <Link href={`/cases/${c.id}`} className="after:absolute after:inset-0">
                      {c.reference}
                    </Link>
                  </TableCell>
                  <TableCell className="max-w-xs truncate font-medium">{c.title}</TableCell>
                  <TableCell>
                    <StatusBadge status={c.status} />
                  </TableCell>
                  <TableCell>
                    <PriorityLabel priority={c.priority} />
                  </TableCell>
                  <TableCell className="hidden lg:table-cell">
                    <TagList tags={c.tags} />
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {c.evidence_count}
                    <div className="text-xs text-muted-foreground">{formatBytes(c.total_bytes)}</div>
                  </TableCell>
                  <TableCell className="hidden pr-4 text-right text-xs text-muted-foreground md:table-cell">{formatUTC(c.updated_at)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <Pagination
            className="border-t px-4 py-3"
            total={data.total}
            page={page}
            size={size}
            pathname="/cases"
            searchParams={sp}
            label="cases"
          />
        </Card>
      )}
    </>
  )
}
