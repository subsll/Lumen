"use client"

import { useState } from "react"
import { LinkIcon, Loader2Icon, ShieldAlertIcon, ShieldCheckIcon } from "lucide-react"

import { CopyButton } from "@/components/hash-value"
import { Button } from "@/components/ui/button"
import { get } from "@/lib/client"
import { formatUTC } from "@/lib/format"
import type { ChainResult } from "@/lib/types"
import { cn } from "@/lib/utils"

export function ChainVerifier({ headSeq, headHash, compact = false }: { headSeq: number; headHash: string; compact?: boolean }) {
  const [result, setResult] = useState<ChainResult | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function verify() {
    setBusy(true)
    setError(null)
    try {
      setResult(await get<ChainResult>("/api/custody/verify"))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const hash = result?.head_hash ?? headHash
  const seq = result?.head_seq ?? headSeq

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-start gap-3">
        <div
          className={cn(
            "flex size-9 shrink-0 items-center justify-center rounded-lg border",
            result?.valid && "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
            result && !result.valid && "border-red-500/30 bg-red-500/10 text-red-600 dark:text-red-400",
          )}
        >
          {result ? result.valid ? <ShieldCheckIcon className="size-4" /> : <ShieldAlertIcon className="size-4" /> : <LinkIcon className="size-4" />}
        </div>
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">
            {result
              ? result.valid
                ? `Chain intact: ${result.checked} events verified`
                : `Chain broken at event #${result.broken_at}`
              : `${seq} events in the chain`}
          </div>
          <div className="text-xs text-muted-foreground">
            {result
              ? result.valid
                ? `Every hash recomputed and linked · ${formatUTC(result.verified_at, true)}`
                : result.reason
              : "Each event includes the SHA-256 of the one before it."}
          </div>
        </div>
      </div>
      {hash && (
        <div className="rounded-md border bg-muted/40 px-3 py-2">
          <div className="text-[11px] font-medium tracking-wide text-muted-foreground uppercase">Head hash (#{seq})</div>
          <div className="flex items-center gap-1">
            <code className={cn("min-w-0 font-mono text-xs", compact ? "truncate" : "break-all")}>{hash}</code>
            <CopyButton value={hash} label="Head hash" />
          </div>
        </div>
      )}
      {error && <p className="text-sm text-destructive">{error}</p>}
      <Button variant="outline" size="sm" onClick={verify} disabled={busy} className="self-start">
        {busy ? <Loader2Icon className="animate-spin" /> : <ShieldCheckIcon />}
        Verify entire chain
      </Button>
    </div>
  )
}
