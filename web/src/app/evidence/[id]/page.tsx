import type { Metadata } from "next"
import Link from "next/link"
import { CheckCircle2Icon, FileIcon, GlobeIcon } from "lucide-react"

import { ArtifactActions, ArtifactPreview, VerifyEvidenceButton } from "@/components/actions"
import { IntegrityBadge, TagList } from "@/components/badges"
import { CustodyTimeline } from "@/components/custody-timeline"
import { HashValue } from "@/components/hash-value"
import { CaptureNetworkCard } from "@/components/capture-details"
import { DnsRecordsCard } from "@/components/dns-records-card"
import { PageHeader } from "@/components/page-header"
import { Pagination, readPage } from "@/components/pagination"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api"
import { formatBytes, formatUTC, roleLabel } from "@/lib/format"
import type { CustodyEvent, Evidence, Paged } from "@/lib/types"

export async function generateMetadata({ params }: PageProps<"/evidence/[id]">): Promise<Metadata> {
  const { id } = await params
  const e = await api<Evidence>(`/api/evidence/${id}`)
  return { title: `${e.exhibit_ref} · ${e.title}` }
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-1 sm:grid-cols-[160px_1fr] sm:gap-4">
      <dt className="text-xs text-muted-foreground sm:pt-0.5">{label}</dt>
      <dd className="min-w-0 text-sm break-words">{children}</dd>
    </div>
  )
}

export default async function EvidencePage({ params, searchParams }: PageProps<"/evidence/[id]">) {
  const { id } = await params
  const sp = await searchParams
  const tl = readPage(sp, "page", "size", 25)
  const [e, eventPage] = await Promise.all([
    api<Evidence>(`/api/evidence/${id}`),
    api<Paged<CustodyEvent>>(`/api/evidence/${id}/custody?${tl.query}`),
  ])
  const events = eventPage.items
  const capture = e.capture ?? null
  const artifacts = e.artifacts ?? []
  const preview = artifacts.find((a) => a.role === "screenshot") ?? artifacts.find((a) => a.role === "original")

  return (
    <>
      <PageHeader
        eyebrow={
          <span className="flex flex-wrap items-center gap-2">
            <Link href="/cases" className="hover:text-foreground">
              Cases
            </Link>
            <span>/</span>
            <Link href={`/cases/${e.case_id}`} className="font-mono hover:text-foreground">
              {e.case_reference}
            </Link>
            <span>/</span>
            <span className="font-mono">E{String(e.number).padStart(3, "0")}</span>
          </span>
        }
        title={
          <span className="flex items-center gap-2">
            {e.kind === "web_capture" ? <GlobeIcon className="size-5 text-primary" /> : <FileIcon className="size-5 text-muted-foreground" />}
            {e.title}
          </span>
        }
        description={
          <span className="flex flex-wrap items-center gap-3">
            <span className="font-mono">{e.exhibit_ref}</span>
            <IntegrityBadge status={e.integrity_status} />
            <span>Last verified {e.last_verified_at ? formatUTC(e.last_verified_at) : "never"}</span>
          </span>
        }
        actions={<VerifyEvidenceButton evidenceId={e.id} />}
      />

      <div className="grid gap-6 xl:grid-cols-[1fr_380px]">
        <div className="flex min-w-0 flex-col gap-6">
          {preview && <ArtifactPreview artifact={preview} />}

          <Card>
            <CardHeader>
              <CardTitle>Files and hashes</CardTitle>
              <CardDescription>
                Hashed on ingestion by the vault. SHA-256 is authoritative; SHA-1 and MD5 are provided for compatibility with legacy tools.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              {artifacts.map((a) => (
                <div key={a.id} className="rounded-lg border p-4">
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate font-medium">{a.filename}</span>
                        <Badge variant="secondary" className="font-normal">
                          {roleLabel(a.role)}
                        </Badge>
                      </div>
                      <div className="mt-0.5 text-xs text-muted-foreground">
                        {a.mime_type} · {formatBytes(a.size_bytes)} ({a.size_bytes.toLocaleString("en-US")} bytes)
                      </div>
                    </div>
                    <ArtifactActions artifact={a} />
                  </div>
                  <div className="mt-3 space-y-1 rounded-md bg-muted/40 p-3">
                    <HashValue algorithm="SHA-256" value={a.sha256} />
                    <HashValue algorithm="SHA-1" value={a.sha1} />
                    <HashValue algorithm="MD5" value={a.md5} />
                  </div>
                  {a.client_sha256 && (
                    <p className="mt-2 flex items-center gap-1.5 text-xs text-emerald-700 dark:text-emerald-400">
                      <CheckCircle2Icon className="size-3.5" />
                      SHA-256 computed in the browser before upload matched the vault, so the file was not altered in transit.
                    </p>
                  )}
                </div>
              ))}
            </CardContent>
          </Card>

          {capture?.dns && <DnsRecordsCard dns={capture.dns} />}

          <Card>
            <CardHeader>
              <CardTitle>Chain of custody</CardTitle>
              <CardDescription>Every action on this exhibit, newest first. Views and downloads are recorded too.</CardDescription>
            </CardHeader>
            <CardContent>
              <CustodyTimeline events={events} showEvidence={false} />
              <Pagination
                className="mt-6 border-t pt-4"
                total={eventPage.total}
                page={tl.page}
                size={tl.size}
                pathname={`/evidence/${id}`}
                searchParams={sp}
                label="events"
              />
            </CardContent>
          </Card>
        </div>

        <div className="flex flex-col gap-6">
          <Card className="h-fit">
            <CardHeader>
              <CardTitle>Provenance</CardTitle>
            </CardHeader>
            <CardContent>
              <dl className="grid gap-4">
                <Row label="Case">
                  <Link href={`/cases/${e.case_id}`} className="text-primary hover:underline">
                    {e.case_reference} · {e.case_title}
                  </Link>
                </Row>
                <Row label="Source">
                  {e.source ? (
                    /^https?:\/\//.test(e.source) ? (
                      <span className="font-mono text-xs">{e.source}</span>
                    ) : (
                      e.source
                    )
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </Row>
                <Row label="Acquired">{formatUTC(e.acquired_at, true)}</Row>
                <Row label="Method">{e.acquisition_method}</Row>
                <Row label="Original location">{e.location || <span className="text-muted-foreground">—</span>}</Row>
                <Row label="Registered">{formatUTC(e.created_at, true)}</Row>
                <Row label="Description">
                  <span className="whitespace-pre-wrap">{e.description || <span className="text-muted-foreground">—</span>}</span>
                </Row>
                <Row label="Tags">
                  <TagList tags={e.tags} />
                </Row>
              </dl>
            </CardContent>
          </Card>

          {capture && <CaptureNetworkCard capture={capture} />}
        </div>
      </div>
    </>
  )
}
