"use client"

import { ServerCrashIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"

export default function ErrorPage({ error, reset }: { error: Error; reset: () => void }) {
  return (
    <Empty className="border">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <ServerCrashIcon />
        </EmptyMedia>
        <EmptyTitle>Something went wrong</EmptyTitle>
        <EmptyDescription>
          {error.message || "Lumen could not load this page."} If you just started Lumen, the API may still be starting. Check{" "}
          <code className="font-mono">docker compose logs api</code>.
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button onClick={reset}>Try again</Button>
      </EmptyContent>
    </Empty>
  )
}
