import type { Metadata } from "next"
import { Geist, Geist_Mono } from "next/font/google"

import { AppSidebar } from "@/components/app-sidebar"
import { ThemeProvider } from "@/components/theme-provider"
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
                {/* No top bar: on desktop the sidebar collapses via its rail or ⌘B; on phones this opens it. */}
                <SidebarTrigger className="fixed top-3 left-3 z-20 border bg-background/90 shadow-sm backdrop-blur md:hidden" />
                <main className="flex flex-1 flex-col gap-6 p-4 pt-14 md:p-6 lg:p-8">{children}</main>
              </SidebarInset>
            </SidebarProvider>
          </TooltipProvider>
          <Toaster richColors position="bottom-right" />
        </ThemeProvider>
      </body>
    </html>
  )
}
