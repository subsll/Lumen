import { cn } from "@/lib/utils"

/** Lumen mark: a geometric "L" in a square. */
export function LumenMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" fill="none" className={cn("size-8", className)} aria-hidden>
      <rect width="32" height="32" rx="5" className="fill-primary" />
      <path d="M10 7h4.5v13.5H23V25H10V7Z" className="fill-primary-foreground" />
    </svg>
  )
}
