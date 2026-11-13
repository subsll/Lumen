"use client"

import { useState } from "react"
import { CheckIcon, CopyIcon } from "lucide-react"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

export function CopyButton({ value, label = "Value" }: { value: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <Button
      variant="ghost"
      size="icon-xs"
      aria-label={`Copy ${label}`}
      onClick={async () => {
        await navigator.clipboard.writeText(value)
        setCopied(true)
        toast.success(`${label} copied`)
        setTimeout(() => setCopied(false), 1500)
      }}
    >
      {copied ? <CheckIcon /> : <CopyIcon />}
    </Button>
  )
}

export function HashValue({
  algorithm,
  value,
  truncate = false,
  className,
}: {
  algorithm?: string
  value: string
  truncate?: boolean
  className?: string
}) {
  return (
    <div className={cn("flex min-w-0 items-center gap-2", className)}>
      {algorithm && (
        <span className="w-14 shrink-0 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
          {algorithm}
        </span>
      )}
      <code className={cn("min-w-0 font-mono text-xs", truncate ? "truncate" : "break-all")} title={value}>
        {value}
      </code>
      <CopyButton value={value} label={algorithm ?? "Hash"} />
    </div>
  )
}
