import type { Metadata } from "next"
import { Geist, Geist_Mono } from "next/font/google"

import { AppSidebar } from "@/components/app-sidebar"
import { SearchCommand } from "@/components/search-command"
import { ThemeProvider } from "@/components/theme-provider"
import { ThemeToggle } from "@/components/theme-toggle"
import { Separator } from "@/components/ui/separator"
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import type { CasePage, Settings } from "@/lib/types"
import "./globals.css"

const sans = Geist({ variable: "--font-sans", subsets: ["latin"] })
const mono = Geist_Mono({ variable: "--font-geist-mono", subsets: ["latin"] })

export const metadata: Metadata = {
  title: { default: "Lumen", template: "%s · Lumen" },
  description: "Lumen by Lumen Forensics: an OSINT Evidence Management System (EMS).",
}

async function loadShell() {
  try {
    const [settings, open] = await Promise.all([api<Settings>("/api/settings"), api<CasePage>("/api/cases?status=open&limit=6")])
    return { settings, open: open.items }
  } catch {
    return { settings: { investigator_name: "", organization: "", case_prefix: "LUM", version: "" }, open: [] }
  }
}

export default async function RootLayout({ children }: LayoutProps<"/">) {
  const { settings, open } = await loadShell()
  const investigator = [settings.investigator_name, settings.organization].filter(Boolean).join(" · ")
  return (
    <html lang="en" suppressHydrationWarning className={`${sans.variable} ${mono.variable} antialiased`}>
      <body>
        <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
          <TooltipProvider delayDuration={300}>
            <SidebarProvider>
              <AppSidebar openCases={open} investigator={investigator} version={settings.version} />
              <SidebarInset>
                <header className="sticky top-0 z-10 flex h-14 shrink-0 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur">
                  <SidebarTrigger className="-ml-1" />
                  <Separator orientation="vertical" className="mr-1 data-[orientation=vertical]:h-4" />
                  <div className="flex flex-1 items-center">
                    <SearchCommand />
                  </div>
                  <ThemeToggle />
                </header>
                <main className="flex flex-1 flex-col gap-6 p-4 md:p-6 lg:p-8">{children}</main>
              </SidebarInset>
            </SidebarProvider>
          </TooltipProvider>
          <Toaster richColors position="bottom-right" />
        </ThemeProvider>
      </body>
    </html>
  )
}
