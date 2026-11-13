import type { Metadata } from "next"
import Link from "next/link"
import { FileStackIcon, LockIcon } from "lucide-react"

import { ExportButton, NotesPanel } from "@/components/actions"
import { PriorityLabel, StatusBadge, TagList } from "@/components/badges"
import { CaseProgress, EditCaseDialog } from "@/components/case-dialogs"
import { CustodyTimeline } from "@/components/custody-timeline"
import { CaptureDialog, UploadEvidenceDialog } from "@/components/evidence-dialogs"
import { EvidenceTable } from "@/components/evidence-table"
import { PageHeader } from "@/components/page-header"
import { Pagination, readPage } from "@/components/pagination"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api } from "@/lib/api"
import { formatBytes, formatUTC } from "@/lib/format"
import type { Case, CustodyEvent, Evidence, Note, Paged } from "@/lib/types"

export async function generateMetadata({ params }: PageProps<"/cases/[id]">): Promise<Metadata> {
  const { id } = await params
  const c = await api<Case>(`/api/cases/${id}`)
  return { title: `${c.reference} · ${c.title}` }
}

export default async function CasePage({ params, searchParams }: PageProps<"/cases/[id]">) {
  const { id } = await params
  const sp = await searchParams
  const tab = typeof sp.tab === "string" && ["evidence", "timeline", "notes"].includes(sp.tab) ? sp.tab : "evidence"
  const ev = readPage(sp, "ep", "es", 25)
  const tl = readPage(sp, "tp", "ts", 50)
  const [c, evidencePage, eventPage, notes] = await Promise.all([
    api<Case>(`/api/cases/${id}`),
    api<Paged<Evidence>>(`/api/cases/${id}/evidence?${ev.query}`),
    api<Paged<CustodyEvent>>(`/api/cases/${id}/custody?${tl.query}`),
    api<Note[]>(`/api/cases/${id}/notes`),
  ])
  const evidence = evidencePage.items
  const events = eventPage.items
  const pathname = `/cases/${id}`
  const locked = c.status === "closed" || c.status === "archived"

  return (
    <>
      <PageHeader
        eyebrow={
          <span className="flex items-center gap-2">
            <Link href="/cases" className="hover:text-foreground">
              Cases
            </Link>
            <span>/</span>
            <span className="font-mono">{c.reference}</span>
          </span>
        }
        title={c.title}
        description={
          <span className="flex flex-wrap items-center gap-3">
            <StatusBadge status={c.status} />
            <span>
              Priority <PriorityLabel priority={c.priority} />
            </span>
            <span>Opened {formatUTC(c.created_at)}</span>
          </span>
        }
        actions={
          <>
            <EditCaseDialog initial={c} />
            <ExportButton caseId={c.id} />
            <CaptureDialog caseId={c.id} disabled={locked} />
            <UploadEvidenceDialog caseId={c.id} disabled={locked} />
          </>
        }
      />

      <Card className="gap-3 py-4">
        <CardContent className="px-4">
          <div className="mb-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">Case progression</div>
          <CaseProgress caseId={c.id} status={c.status} />
        </CardContent>
      </Card>

      {locked && (
        <Alert>
          <LockIcon />
          <AlertTitle>This case is {c.status}</AlertTitle>
          <AlertDescription>Evidence can no longer be added. Move the case back to Active to continue collecting.</AlertDescription>
        </Alert>
      )}

      <div className="grid gap-6 xl:grid-cols-[1fr_320px]">
        <Tabs defaultValue={tab} className="min-w-0">
          <TabsList>
            <TabsTrigger value="evidence">Evidence ({evidencePage.total})</TabsTrigger>
            <TabsTrigger value="timeline">Custody timeline ({eventPage.total})</TabsTrigger>
            <TabsTrigger value="notes">Notes ({notes.length})</TabsTrigger>
          </TabsList>
          <TabsContent value="evidence" className="mt-2">
            {evidencePage.total === 0 ? (
              <Empty className="border">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <FileStackIcon />
                  </EmptyMedia>
                  <EmptyTitle>No evidence yet</EmptyTitle>
                  <EmptyDescription>Upload files or capture a web page. Each item gets an exhibit number and a SHA-256 fingerprint.</EmptyDescription>
                </EmptyHeader>
                {!locked && (
                  <EmptyContent className="flex-row justify-center gap-2">
                    <CaptureDialog caseId={c.id} />
                    <UploadEvidenceDialog caseId={c.id} />
                  </EmptyContent>
                )}
              </Empty>
            ) : (
              <Card className="py-0">
                <EvidenceTable items={evidence} />
                <Pagination
                  className="border-t px-4 py-3"
                  total={evidencePage.total}
                  page={ev.page}
                  size={ev.size}
                  pathname={pathname}
                  searchParams={sp}
                  pageParam="ep"
                  sizeParam="es"
                  extra={{ tab: "evidence" }}
                  label="exhibits"
                />
              </Card>
            )}
          </TabsContent>
          <TabsContent value="timeline" className="mt-2">
            <Card>
              <CardContent className="space-y-6">
                <CustodyTimeline events={events} />
                <Pagination
                  className="border-t pt-4"
                  total={eventPage.total}
                  page={tl.page}
                  size={tl.size}
                  pathname={pathname}
                  searchParams={sp}
                  pageParam="tp"
                  sizeParam="ts"
                  extra={{ tab: "timeline" }}
                  label="events"
                />
              </CardContent>
            </Card>
          </TabsContent>
          <TabsContent value="notes" className="mt-2">
            <Card>
              <CardContent>
                <NotesPanel caseId={c.id} notes={notes} readOnly={c.status === "archived"} />
              </CardContent>
            </Card>
          </TabsContent>
        </Tabs>

        <Card className="h-fit">
          <CardHeader>
            <CardTitle>Details</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-4 text-sm">
              <div>
                <dt className="text-xs text-muted-foreground">Description</dt>
                <dd className="mt-1 whitespace-pre-wrap">{c.description || <span className="text-muted-foreground">—</span>}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Tags</dt>
                <dd className="mt-1">
                  <TagList tags={c.tags} />
                </dd>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <dt className="text-xs text-muted-foreground">Evidence</dt>
                  <dd className="mt-1 tabular-nums">{c.evidence_count} items</dd>
                </div>
                <div>
                  <dt className="text-xs text-muted-foreground">Stored</dt>
                  <dd className="mt-1 tabular-nums">{formatBytes(c.total_bytes)}</dd>
                </div>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Last updated</dt>
                <dd className="mt-1">{formatUTC(c.updated_at, true)}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      </div>
    </>
  )
}
