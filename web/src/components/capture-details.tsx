import { ArrowDownIcon, GlobeIcon, LockIcon, ServerIcon } from "lucide-react"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { formatUTC } from "@/lib/format"
import type { CaptureSummary } from "@/lib/types"

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-1 sm:grid-cols-[140px_1fr] sm:gap-4">
      <dt className="text-xs text-muted-foreground sm:pt-0.5">{label}</dt>
      <dd className="min-w-0 text-sm break-words">{children}</dd>
    </div>
  )
}

/** Navigation, HTTP and TLS context recorded at capture time. */
export function CaptureNetworkCard({ capture }: { capture: CaptureSummary }) {
  const { http, tls, redirects = [] } = capture
  return (
    <Card className="h-fit">
      <CardHeader>
        <CardTitle>Capture details</CardTitle>
        <CardDescription>Network context recorded by the capture browser.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <div>
          <div className="mb-2 flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
            <GlobeIcon className="size-3.5" /> Navigation
          </div>
          <ol className="space-y-1 font-mono text-xs">
            <li className="break-all">{capture.requested_url}</li>
            {redirects.map((r, i) => (
              <li key={i} className="space-y-1">
                <div className="flex items-center gap-1.5 text-muted-foreground">
                  <ArrowDownIcon className="size-3" /> {r.status} redirect
                </div>
                <div className="break-all">{r.location}</div>
              </li>
            ))}
            {capture.final_url !== (redirects.at(-1)?.location ?? capture.requested_url) && (
              <li className="space-y-1">
                <div className="flex items-center gap-1.5 text-muted-foreground">
                  <ArrowDownIcon className="size-3" /> final
                </div>
                <div className="break-all">{capture.final_url}</div>
              </li>
            )}
          </ol>
        </div>

        <dl className="grid gap-3">
          <Row label="Started">{formatUTC(capture.started_at, true)}</Row>
          <Row label="Completed">{formatUTC(capture.completed_at, true)}</Row>
          <Row label="Browser">{capture.browser}</Row>
          <Row label="Viewport">{capture.viewport}</Row>
        </dl>

        {http && (
          <div>
            <div className="mb-2 flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
              <ServerIcon className="size-3.5" /> HTTP response
            </div>
            <dl className="grid gap-3">
              <Row label="Status">
                {http.status} {http.status_text} <span className="text-muted-foreground">· {http.protocol}</span>
              </Row>
              <Row label="Server address">
                <span className="font-mono text-xs">
                  {http.remote_ip}:{http.remote_port}
                </span>
              </Row>
              <Row label="Content type">{http.mime_type}</Row>
            </dl>
            {Object.keys(http.headers ?? {}).length > 0 && (
              <details className="mt-3">
                <summary className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
                  {Object.keys(http.headers).length} response headers
                </summary>
                <pre className="mt-2 max-h-72 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap">
                  {Object.entries(http.headers)
                    .sort(([a], [b]) => a.localeCompare(b))
                    .map(([k, v]) => `${k}: ${v}`)
                    .join("\n")}
                </pre>
              </details>
            )}
          </div>
        )}

        {tls && (
          <div>
            <div className="mb-2 flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase">
              <LockIcon className="size-3.5" /> TLS certificate
            </div>
            <dl className="grid gap-3">
              <Row label="Subject">{tls.subject_name}</Row>
              <Row label="Issuer">{tls.issuer}</Row>
              <Row label="Valid">
                {tls.valid_from?.slice(0, 10)} → {tls.valid_to?.slice(0, 10)}
              </Row>
              <Row label="Connection">
                {tls.protocol} · {tls.cipher}
              </Row>
              {tls.sans?.length > 0 && (
                <Row label={`Names (${tls.sans.length})`}>
                  <span className="font-mono text-xs">
                    {tls.sans.slice(0, 8).join(", ")}
                    {tls.sans.length > 8 && ` +${tls.sans.length - 8} more`}
                  </span>
                </Row>
              )}
            </dl>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
