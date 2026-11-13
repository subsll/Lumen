"use client"

import { useMemo, useState } from "react"
import { SearchIcon, XIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatUTC } from "@/lib/format"
import type { CaptureSummary, DnsRecord } from "@/lib/types"
import { cn } from "@/lib/utils"

const TYPE_ORDER = ["A", "AAAA", "CNAME", "NS", "SOA", "MX", "TXT", "CAA", "PTR"]
const VISIBLE = 20
const rank = (t: string) => TYPE_ORDER.indexOf(t) + 1 || 99

function ttl(seconds: number) {
  if (seconds >= 86400 && seconds % 86400 === 0) return `${seconds / 86400}d`
  if (seconds >= 3600 && seconds % 3600 === 0) return `${seconds / 3600}h`
  if (seconds >= 60 && seconds % 60 === 0) return `${seconds / 60}m`
  return `${seconds}s`
}

/** DNS records captured alongside the page, filterable by record type. */
export function DnsRecordsCard({ dns }: { dns: NonNullable<CaptureSummary["dns"]> }) {
  const queries = useMemo(() => dns.queries ?? [], [dns])
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [text, setText] = useState("")
  const [expanded, setExpanded] = useState(false)

  // CNAME chains repeat in the answer to every query on a host, so show each
  // unique record once. The hashed .dns.txt artifact keeps the raw answers.
  const records = useMemo(() => {
    const unique = new Map<string, DnsRecord>()
    for (const q of queries)
      for (const r of q.records ?? []) {
        const key = `${r.name.toLowerCase()}|${r.type}|${r.value}`
        if (!unique.has(key)) unique.set(key, r)
      }
    return [...unique.values()].sort(
      (a, b) => a.name.localeCompare(b.name) || rank(a.type) - rank(b.type) || a.value.localeCompare(b.value),
    )
  }, [queries])

  const counts = useMemo(() => {
    const m = new Map<string, number>()
    for (const r of records) m.set(r.type, (m.get(r.type) ?? 0) + 1)
    return [...m.entries()].sort(([a], [b]) => rank(a) - rank(b))
  }, [records])

  const needle = text.trim().toLowerCase()
  const filtered = records.filter(
    (r) =>
      (selected.size === 0 || selected.has(r.type)) &&
      (!needle || r.name.toLowerCase().includes(needle) || r.value.toLowerCase().includes(needle)),
  )
  const filtering = selected.size > 0 || needle !== ""
  const shown = expanded ? filtered : filtered.slice(0, VISIBLE)
  const empty = queries.filter((q) => !q.records?.length && (selected.size === 0 || selected.has(q.type)))

  const toggle = (t: string) =>
    setSelected((cur) => {
      const next = new Set(cur)
      if (next.has(t)) next.delete(t)
      else next.add(t)
      return next
    })

  return (
    <Card className="min-w-0">
      <CardHeader>
        <CardTitle>DNS records</CardTitle>
        <CardDescription>
          {records.length} unique records from {queries.length} queries via {dns.resolver} at {formatUTC(dns.queried_at, true)}. The raw
          answers are preserved in the exhibit&apos;s hashed <span className="font-mono">.dns.txt</span> file.
        </CardDescription>
        {(dns.hosts?.length ?? 0) > 0 && (
          <div className="flex flex-wrap gap-1 pt-1">
            {(dns.hosts ?? []).map((h) => (
              <Badge key={`h-${h}`} variant="secondary" className="font-mono font-normal">
                {h}
              </Badge>
            ))}
            {(dns.registered_domains ?? [])
              .filter((d) => !(dns.hosts ?? []).includes(d))
              .map((d) => (
                <Badge key={`d-${d}`} variant="outline" className="font-mono font-normal">
                  {d}
                </Badge>
              ))}
          </div>
        )}
      </CardHeader>

      <CardContent className="space-y-4 px-0">
        <div className="flex flex-col gap-3 px-6 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex flex-wrap gap-1" role="group" aria-label="Filter by record type">
            <button
              type="button"
              onClick={() => setSelected(new Set())}
              aria-pressed={selected.size === 0}
              className={cn(
                "rounded-sm border px-2 py-0.5 text-xs transition-colors hover:bg-muted",
                selected.size === 0 && "border-primary bg-primary text-primary-foreground hover:bg-primary/90",
              )}
            >
              All <span className="tabular-nums opacity-70">{records.length}</span>
            </button>
            {counts.map(([type, n]) => {
              const on = selected.has(type)
              return (
                <button
                  key={type}
                  type="button"
                  onClick={() => toggle(type)}
                  aria-pressed={on}
                  className={cn(
                    "rounded-sm border px-2 py-0.5 font-mono text-xs transition-colors hover:bg-muted",
                    on && "border-primary bg-primary text-primary-foreground hover:bg-primary/90",
                  )}
                >
                  {type} <span className="tabular-nums opacity-70">{n}</span>
                </button>
              )
            })}
          </div>
          <div className="relative sm:w-56">
            <SearchIcon className="absolute top-1/2 left-2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input value={text} onChange={(e) => setText(e.target.value)} placeholder="Filter name or value" className="h-7 pl-7 text-xs" />
          </div>
        </div>

        {filtering && (
          <div className="flex items-center gap-2 px-6 text-xs text-muted-foreground">
            Showing {filtered.length} of {records.length} records
            <Button
              variant="ghost"
              size="xs"
              onClick={() => {
                setSelected(new Set())
                setText("")
              }}
            >
              <XIcon /> Clear
            </Button>
          </div>
        )}

        {filtered.length === 0 ? (
          <p className="px-6 py-4 text-center text-sm text-muted-foreground">No records match this filter.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">Name</TableHead>
                <TableHead>Type</TableHead>
                <TableHead className="text-right">TTL</TableHead>
                <TableHead className="pr-6">Value</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {shown.map((r, i) => {
                const sameName = i > 0 && shown[i - 1].name === r.name
                return (
                  <TableRow key={`${r.name}|${r.type}|${r.value}`} className={i > 0 && !sameName ? "border-t-2" : undefined}>
                    <TableCell className="max-w-56 truncate pl-6 font-mono text-xs" title={r.name}>
                      {sameName ? <span className="text-muted-foreground/50">〃</span> : r.name}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline" className="font-mono font-normal">
                        {r.type}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs text-muted-foreground tabular-nums" title={`${r.ttl} seconds`}>
                      {ttl(r.ttl)}
                    </TableCell>
                    <TableCell className="max-w-md pr-6 font-mono text-xs break-all whitespace-normal">{r.value}</TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}

        {filtered.length > VISIBLE && (
          <button type="button" onClick={() => setExpanded((e) => !e)} className="px-6 text-sm text-primary hover:underline">
            {expanded ? "Show fewer" : `Show all ${filtered.length} records`}
          </button>
        )}

        {empty.length > 0 && (
          <p className="px-6 text-xs text-muted-foreground">
            No records:{" "}
            {empty.map((q, i) => (
              <span key={`${q.name}-${q.type}`}>
                {i > 0 && ", "}
                <span className="font-mono">
                  {q.name} {q.type}
                </span>
                {q.error ? " (error)" : q.rcode && q.rcode !== "NOERROR" ? ` (${q.rcode})` : ""}
              </span>
            ))}
          </p>
        )}
      </CardContent>
    </Card>
  )
}
