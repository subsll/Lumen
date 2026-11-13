"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import {
  FileIcon,
  FolderIcon,
  GlobeIcon,
  LayoutDashboardIcon,
  LinkIcon,
  SearchIcon,
  SettingsIcon,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { get } from "@/lib/client"
import type { SearchResults } from "@/lib/types"

export function SearchCommand() {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [results, setResults] = useState<SearchResults>({ cases: [], evidence: [] })
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "k" && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        setOpen((o) => !o)
      }
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [])

  useEffect(() => {
    const q = query.trim()
    if (!q) return
    const ctrl = new AbortController()
    const t = setTimeout(() => {
      setLoading(true)
      get<SearchResults>(`/api/search?q=${encodeURIComponent(q)}`, ctrl.signal)
        .then(setResults)
        .catch(() => {})
        .finally(() => setLoading(false))
    }, 180)
    return () => {
      clearTimeout(t)
      ctrl.abort()
    }
  }, [query])

  const go = (href: string) => {
    setOpen(false)
    setQuery("")
    router.push(href)
  }
  const hasQuery = query.trim().length > 0
  const shown = hasQuery ? results : { cases: [], evidence: [] }

  return (
    <>
      <Button
        variant="outline"
        className="h-8 w-full max-w-sm justify-start gap-2 text-muted-foreground"
        onClick={() => setOpen(true)}
      >
        <SearchIcon />
        <span className="flex-1 text-left">Search cases, evidence, hashes…</span>
        <kbd className="pointer-events-none hidden rounded border bg-muted px-1.5 font-mono text-[10px] sm:inline">
          ⌘K
        </kbd>
      </Button>
      <CommandDialog open={open} onOpenChange={setOpen} className="sm:max-w-xl" title="Search Lumen" description="Search cases, evidence and hashes">
        <Command shouldFilter={false}>
          <CommandInput
            placeholder="Case reference, title, URL, filename, or a SHA-256/SHA-1/MD5 hash…"
            value={query}
            onValueChange={setQuery}
          />
          <CommandList>
            {hasQuery && !loading && <CommandEmpty>No matches.</CommandEmpty>}
            {shown.cases.length > 0 && (
              <CommandGroup heading="Cases">
                {shown.cases.map((c) => (
                  <CommandItem key={c.id} value={`case-${c.id}`} onSelect={() => go(`/cases/${c.id}`)}>
                    <FolderIcon />
                    <span className="font-mono text-xs text-muted-foreground">{c.reference}</span>
                    <span className="truncate">{c.title}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {shown.evidence.length > 0 && (
              <CommandGroup heading="Evidence">
                {shown.evidence.map((e) => (
                  <CommandItem key={e.id} value={`ev-${e.id}`} onSelect={() => go(`/evidence/${e.id}`)}>
                    {e.kind === "web_capture" ? <GlobeIcon /> : <FileIcon />}
                    <span className="font-mono text-xs text-muted-foreground">{e.exhibit_ref}</span>
                    <span className="truncate">{e.title}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
            {!hasQuery && (
              <CommandGroup heading="Go to">
                <CommandItem onSelect={() => go("/")}>
                  <LayoutDashboardIcon /> Dashboard
                </CommandItem>
                <CommandItem onSelect={() => go("/cases")}>
                  <FolderIcon /> Cases
                </CommandItem>
                <CommandItem onSelect={() => go("/evidence")}>
                  <FileIcon /> Evidence
                </CommandItem>
                <CommandItem onSelect={() => go("/custody")}>
                  <LinkIcon /> Chain of custody
                </CommandItem>
                <CommandItem onSelect={() => go("/settings")}>
                  <SettingsIcon /> Settings
                </CommandItem>
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </CommandDialog>
    </>
  )
}
