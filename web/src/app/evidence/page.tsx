import type { Metadata } from "next"
import Link from "next/link"
import { FileStackIcon } from "lucide-react"

import { EvidenceTable } from "@/components/evidence-table"
import { PageHeader } from "@/components/page-header"
import { Pagination, readPage } from "@/components/pagination"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import { api } from "@/lib/api"
import type { Evidence, Paged } from "@/lib/types"

export const metadata: Metadata = { title: "Evidence" }

export default async function EvidenceIndex({ searchParams }: PageProps<"/evidence">) {
  const sp = await searchParams
  const { page, size, query } = readPage(sp)
  const data = await api<Paged<Evidence>>(`/api/evidence?${query}`)
  const items = data.items
  return (
    <>
      <PageHeader title="Evidence" description="Every exhibit across all cases, newest first. Press ⌘K to search by title, URL, filename or hash." />
      {data.total === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FileStackIcon />
            </EmptyMedia>
            <EmptyTitle>No evidence registered</EmptyTitle>
            <EmptyDescription>Evidence is always registered within a case.</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button asChild>
              <Link href="/cases">Go to cases</Link>
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <Card className="py-0">
          <EvidenceTable items={items} showCase />
          <Pagination className="border-t px-4 py-3" total={data.total} page={page} size={size} pathname="/evidence" searchParams={sp} label="exhibits" />
        </Card>
      )}
    </>
  )
}
