import type { Metadata } from "next"

import { ChainVerifier } from "@/components/chain-verifier"
import { CustodyTimeline } from "@/components/custody-timeline"
import { PageHeader } from "@/components/page-header"
import { Pagination, readPage } from "@/components/pagination"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api"
import type { CustodyEvent, Dashboard, Paged } from "@/lib/types"

export const metadata: Metadata = { title: "Chain of Custody" }

export default async function CustodyPage({ searchParams }: PageProps<"/custody">) {
  const sp = await searchParams
  const { page, size, query } = readPage(sp, "page", "size", 50)
  const [data, d] = await Promise.all([api<Paged<CustodyEvent>>(`/api/custody?${query}`), api<Dashboard>("/api/dashboard")])

  return (
    <>
      <PageHeader
        title="Chain of custody"
        description="The complete, append-only record of every action in Lumen. Each event is sealed with SHA-256 and linked to the one before it."
      />
      <div className="grid gap-6 xl:grid-cols-[1fr_360px]">
        <Card className="min-w-0">
          <CardHeader>
            <CardTitle>Events</CardTitle>
            <CardDescription>Newest first</CardDescription>
          </CardHeader>
          <CardContent className="space-y-6">
            <CustodyTimeline events={data.items} showCase />
            <Pagination className="border-t pt-4" total={data.total} page={page} size={size} pathname="/custody" searchParams={sp} label="events" />
          </CardContent>
        </Card>
        <div className="flex flex-col gap-6">
          <Card className="h-fit">
            <CardHeader>
              <CardTitle>Verification</CardTitle>
              <CardDescription>Recomputes every event hash and every link.</CardDescription>
            </CardHeader>
            <CardContent>
              <ChainVerifier headSeq={d.chain_head.seq} headHash={d.chain_head.hash} />
            </CardContent>
          </Card>
          <Card className="h-fit">
            <CardHeader>
              <CardTitle>How it works</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm text-muted-foreground">
              <p>
                Every event stores the SHA-256 of the previous event. Editing, deleting or reordering any record changes its hash and breaks
                every link after it.
              </p>
              <p>
                The database also refuses updates and deletes on custody records. The hash chain still catches tampering by anyone who bypasses
                that.
              </p>
              <p>
                <span className="font-medium text-foreground">Tip:</span> write down or email yourself the head hash at key moments, such as
                when you close a case. That external record proves the history up to that point has not been rewritten.
              </p>
            </CardContent>
          </Card>
        </div>
      </div>
    </>
  )
}
