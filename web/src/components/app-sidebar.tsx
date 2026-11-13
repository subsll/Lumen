"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import {
  FileStackIcon,
  FolderKanbanIcon,
  LayoutDashboardIcon,
  LinkIcon,
  SettingsIcon,
} from "lucide-react"

import { LumenMark } from "@/components/logo"
import { SearchCommand } from "@/components/search-command"
import { ThemeToggle } from "@/components/theme-toggle"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar"
import type { Case } from "@/lib/types"
import { STATUS_STYLE } from "@/lib/format"
import { cn } from "@/lib/utils"

const NAV = [
  { href: "/", label: "Dashboard", icon: LayoutDashboardIcon },
  { href: "/cases", label: "Cases", icon: FolderKanbanIcon },
  { href: "/evidence", label: "Evidence", icon: FileStackIcon },
  { href: "/custody", label: "Chain of Custody", icon: LinkIcon },
]

export function AppSidebar({
  openCases,
  investigator,
  version,
}: {
  openCases: Case[]
  investigator: string
  version: string
}) {
  const pathname = usePathname()
  const isActive = (href: string) => (href === "/" ? pathname === "/" : pathname.startsWith(href))

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild tooltip="Lumen">
              <Link href="/">
                <span className="flex size-8 shrink-0 items-center justify-center">
                  <LumenMark className="size-8" />
                </span>
                <div className="grid flex-1 text-left leading-tight">
                  <span className="font-semibold tracking-tight">Lumen</span>
                  <span className="truncate text-xs text-muted-foreground">OSINT Evidence Management</span>
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem className="mt-1">
            <SearchCommand />
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {NAV.map((item) => (
                <SidebarMenuItem key={item.href}>
                  <SidebarMenuButton asChild isActive={isActive(item.href)} tooltip={item.label}>
                    <Link href={item.href}>
                      <item.icon />
                      <span>{item.label}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        {openCases.length > 0 && (
          <SidebarGroup className="group-data-[collapsible=icon]:hidden">
            <SidebarGroupLabel>Open cases</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {openCases.map((c) => (
                  <SidebarMenuItem key={c.id}>
                    <SidebarMenuButton asChild isActive={pathname === `/cases/${c.id}`}>
                      <Link href={`/cases/${c.id}`}>
                        <span className={cn("size-2 shrink-0 rounded-full border", STATUS_STYLE[c.status])} />
                        <span className="truncate">{c.title}</span>
                      </Link>
                    </SidebarMenuButton>
                    <SidebarMenuBadge>{c.evidence_count}</SidebarMenuBadge>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        )}
      </SidebarContent>

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <ThemeToggle />
          </SidebarMenuItem>
          <SidebarMenuItem>
            <SidebarMenuButton asChild isActive={isActive("/settings")} tooltip="Settings">
              <Link href="/settings">
                <SettingsIcon />
                <span>Settings</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <div className="px-2 pb-1 text-xs text-muted-foreground group-data-[collapsible=icon]:hidden">
          <div className="truncate font-medium text-foreground">{investigator || "Investigator not set"}</div>
          <div>Lumen {version} · Lumen Forensics</div>
        </div>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
