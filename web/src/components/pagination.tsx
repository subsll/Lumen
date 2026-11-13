import Link from "next/link"
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react"

import { buttonVariants } from "@/components/ui/button"
import { cn } from "@/lib/utils"

export type SearchParams = Record<string, string | string[] | undefined>

const SIZES = [10, 25, 50, 100]

/** Reads ?page / ?size style params (names configurable) into limit + page. */
export function readPage(sp: SearchParams, pageParam = "page", sizeParam = "size", defaultSize = 25) {
  const num = (v: unknown, d: number) => {
    const n = Number(Array.isArray(v) ? v[0] : v)
    return Number.isFinite(n) && n > 0 ? Math.floor(n) : d
  }
  const size = Math.min(200, num(sp[sizeParam], defaultSize))
  const page = num(sp[pageParam], 1)
  return { size, page, query: `limit=${size}&page=${page}` }
}

/** Builds pages to show: 1 … 4 5 [6] 7 8 … 20 */
function pageList(current: number, last: number): (number | "…")[] {
  const pages = new Set([1, last, current - 1, current, current + 1])
  if (current <= 3) [2, 3, 4].forEach((p) => pages.add(p))
  if (current >= last - 2) [last - 1, last - 2, last - 3].forEach((p) => pages.add(p))
  const sorted = [...pages].filter((p) => p >= 1 && p <= last).sort((a, b) => a - b)
  const out: (number | "…")[] = []
  sorted.forEach((p, i) => {
    if (i > 0 && p - sorted[i - 1] > 1) out.push("…")
    out.push(p)
  })
  return out
}

export function Pagination({
  total,
  page,
  size,
  pathname,
  searchParams,
  pageParam = "page",
  sizeParam = "size",
  extra,
  label = "items",
  className,
}: {
  total: number
  page: number
  size: number
  pathname: string
  searchParams: SearchParams
  pageParam?: string
  sizeParam?: string
  extra?: Record<string, string>
  label?: string
  className?: string
}) {
  const last = Math.max(1, Math.ceil(total / size))
  const current = Math.min(page, last)
  const from = total === 0 ? 0 : (current - 1) * size + 1
  const to = Math.min(total, current * size)

  const href = (p: number, s = size) => {
    const params = new URLSearchParams()
    for (const [k, v] of Object.entries(searchParams)) if (typeof v === "string") params.set(k, v)
    for (const [k, v] of Object.entries(extra ?? {})) params.set(k, v)
    params.set(pageParam, String(p))
    params.set(sizeParam, String(s))
    if (p === 1) params.delete(pageParam)
    return `${pathname}?${params.toString()}`
  }

  if (total <= SIZES[0]) {
    return total > 0 ? (
      <p className={cn("text-xs text-muted-foreground", className)}>
        {total} {label}
      </p>
    ) : null
  }

  const btn = buttonVariants({ variant: "ghost", size: "icon-sm" })
  return (
    <nav aria-label="Pagination" className={cn("flex flex-col gap-3 text-sm sm:flex-row sm:items-center sm:justify-between", className)}>
      <div className="flex items-center gap-3 text-xs text-muted-foreground">
        <span className="tabular-nums">
          {from}–{to} of {total} {label}
        </span>
        <span className="hidden items-center gap-1 sm:flex">
          Per page
          {SIZES.map((s) => (
            <Link
              key={s}
              href={href(1, s)}
              scroll={false}
              className={cn("rounded-sm px-1.5 py-0.5 tabular-nums hover:bg-muted", s === size && "bg-muted font-medium text-foreground")}
            >
              {s}
            </Link>
          ))}
        </span>
      </div>
      {last > 1 && (
        <div className="flex items-center gap-1">
          {current > 1 ? (
            <Link href={href(current - 1)} scroll={false} className={btn} aria-label="Previous page">
              <ChevronLeftIcon />
            </Link>
          ) : (
            <span className={cn(btn, "pointer-events-none opacity-40")}>
              <ChevronLeftIcon />
            </span>
          )}
          {pageList(current, last).map((p, i) =>
            p === "…" ? (
              <span key={`e${i}`} className="px-1 text-muted-foreground">
                …
              </span>
            ) : (
              <Link
                key={p}
                href={href(p)}
                scroll={false}
                aria-current={p === current ? "page" : undefined}
                className={cn(buttonVariants({ variant: p === current ? "outline" : "ghost", size: "sm" }), "min-w-7 tabular-nums")}
              >
                {p}
              </Link>
            ),
          )}
          {current < last ? (
            <Link href={href(current + 1)} scroll={false} className={btn} aria-label="Next page">
              <ChevronRightIcon />
            </Link>
          ) : (
            <span className={cn(btn, "pointer-events-none opacity-40")}>
              <ChevronRightIcon />
            </span>
          )}
        </div>
      )}
    </nav>
  )
}
