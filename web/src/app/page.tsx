import Link from "next/link"
import { ArrowRightIcon, FileStackIcon, FolderOpenIcon, HardDriveIcon, ShieldAlertIcon, ShieldCheckIcon } from "lucide-react"

import { StatusBadge } from "@/components/badges"
import { NewCaseDialog } from "@/components/case-dialogs"
import { ChainVerifier } from "@/components/chain-verifier"
import { CustodyTimeline } from "@/components/custody-timeline"
import { LumenMark } from "@/components/logo"
import { PageHeader } from "@/components/page-header"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { api } from "@/lib/api"
import { formatBytes, formatUTC, STATUSES } from "@/lib/format"
import type { Dashboard, Settings } from "@/lib/types"

export default async function DashboardPage() {
  const [d, settings] = await Promise.all([api<Dashboard>("/api/dashboard"), api<Settings>("/api/settings")])
  const open = d.cases_by_status.intake + d.cases_by_status.active + d.cases_by_status.review

  if (d.total_cases === 0) {
    return (
      <Empty className="border">
        <EmptyHeader>
          <EmptyMedia>
            <LumenMark className="size-12" />
          </EmptyMedia>
          <EmptyTitle>Welcome to Lumen</EmptyTitle>
          <EmptyDescription>
            Open your first case to start registering evidence. Every file is hashed with SHA-256, encrypted in the vault, and
            tracked in a tamper-evident chain of custody.
          </EmptyDescription>
        </EmptyHeader>
        <EmptyContent className="flex-row justify-center gap-2">
          {!settings.investigator_name && (
            <Button variant="outline" asChild>
              <Link href="/settings">Set your name first</Link>
            </Button>
          )}
          <NewCaseDialog />
        </EmptyContent>
      </Empty>
    )
  }

  const stats = [
    { label: "Open cases", value: open, hint: `${d.total_cases} total`, icon: FolderOpenIcon },
    { label: "Evidence items", value: d.total_evidence, hint: "across all cases", icon: FileStackIcon },
    { label: "Stored in vault", value: formatBytes(d.total_bytes), hint: "encrypted at rest", icon: HardDriveIcon },
    {
      label: "Integrity verified",
      value: `${d.integrity.verified}/${d.total_evidence}`,
      hint: d.integrity.failed ? `${d.integrity.failed} failed` : `${d.integrity.unverified} pending`,
      icon: d.integrity.failed ? ShieldAlertIcon : ShieldCheckIcon,
    },
  ]

  return (
    <>
      <PageHeader
        title={settings.investigator_name ? `Welcome back, ${settings.investigator_name}` : "Dashboard"}
        description="Overview of your cases, evidence and chain of custody."
        actions={<NewCaseDialog />}
      />

      {d.integrity.failed > 0 && (
        <Alert variant="destructive">
          <ShieldAlertIcon />
          <AlertTitle>{d.integrity.failed} evidence item(s) failed integrity verification</AlertTitle>
          <AlertDescription>Stored bytes no longer match their recorded SHA-256. Investigate before relying on these items.</AlertDescription>
        </Alert>
      )}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {stats.map((s) => (
          <Card key={s.label} className="gap-2">
            <CardHeader>
              <CardDescription>{s.label}</CardDescription>
              <CardAction>
                <s.icon className="size-4 text-muted-foreground" />
              </CardAction>
              <CardTitle className="text-2xl font-semibold tabular-nums">{s.value}</CardTitle>
            </CardHeader>
            <CardContent className="text-xs text-muted-foreground">{s.hint}</CardContent>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <div className="flex flex-col gap-4 lg:col-span-2">
          <Card>
            <CardHeader>
              <CardTitle>Open cases</CardTitle>
              <CardDescription>Most recently updated</CardDescription>
              <CardAction>
                <Button variant="ghost" size="sm" asChild>
                  <Link href="/cases">
                    All cases <ArrowRightIcon />
                  </Link>
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent>
              {d.open_cases.length === 0 ? (
                <p className="text-sm text-muted-foreground">No open cases.</p>
              ) : (
                <ul className="divide-y">
                  {d.open_cases.map((c) => (
                    <li key={c.id}>
                      <Link href={`/cases/${c.id}`} className="-mx-2 flex items-center gap-3 rounded-md px-2 py-3 hover:bg-muted/50">
                        <div className="min-w-0 flex-1">
                          <div className="truncate font-medium">{c.title}</div>
                          <div className="text-xs text-muted-foreground">
                            <span className="font-mono">{c.reference}</span> · {c.evidence_count} evidence · updated {formatUTC(c.updated_at)}
                          </div>
                        </div>
                        <StatusBadge status={c.status} />
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
              <div className="mt-4 grid grid-cols-5 gap-2 border-t pt-4">
                {STATUSES.map((s) => (
                  <Link key={s.value} href={`/cases?status=${s.value}`} className="rounded-md p-2 text-center hover:bg-muted/50">
                    <div className="text-lg font-semibold tabular-nums">{d.cases_by_status[s.value]}</div>
                    <div className="text-xs text-muted-foreground">{s.label}</div>
                  </Link>
                ))}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Recent activity</CardTitle>
              <CardDescription>Latest entries in the chain of custody</CardDescription>
              <CardAction>
                <Button variant="ghost" size="sm" asChild>
                  <Link href="/custody">
                    Full log <ArrowRightIcon />
                  </Link>
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent>
              <CustodyTimeline events={d.recent_activity} showCase />
            </CardContent>
          </Card>
        </div>

        <Card className="h-fit">
          <CardHeader>
            <CardTitle>Chain of custody</CardTitle>
            <CardDescription>Tamper-evident, hash-linked event log</CardDescription>
          </CardHeader>
          <CardContent>
            <ChainVerifier headSeq={d.chain_head.seq} headHash={d.chain_head.hash} compact />
          </CardContent>
        </Card>
      </div>
    </>
  )
}
