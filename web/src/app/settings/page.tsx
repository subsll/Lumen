import type { Metadata } from "next"
import { KeyRoundIcon, LockIcon, NetworkIcon, ShieldCheckIcon } from "lucide-react"

import { SettingsForm } from "@/components/actions"
import { PageHeader } from "@/components/page-header"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api"
import type { Settings } from "@/lib/types"

export const metadata: Metadata = { title: "Settings" }

const SECURITY = [
  {
    icon: LockIcon,
    title: "Encrypted vault",
    text: "Each file is encrypted with its own AES-256-GCM key, wrapped by your master key. The vault runs in its own container on an internal network with no internet access.",
  },
  {
    icon: ShieldCheckIcon,
    title: "Proven hashing",
    text: "SHA-256 is computed in your browser and again in the vault; registration fails if they differ. Stored files are re-hashed automatically on a schedule.",
  },
  {
    icon: KeyRoundIcon,
    title: "Back up your master key",
    text: "LUMEN_MASTER_KEY in your .env file decrypts the vault. Without it the evidence is unrecoverable. Keep a copy offline, separate from your backups.",
  },
  {
    icon: NetworkIcon,
    title: "Local only",
    text: "Lumen listens on localhost only and has no login. Do not expose it to a network without adding authentication in front.",
  },
]

export default async function SettingsPage() {
  const settings = await api<Settings>("/api/settings")
  return (
    <>
      <PageHeader title="Settings" description="Your identity on the chain of custody and how cases are numbered." />
      <div className="grid gap-6 lg:grid-cols-[1fr_380px]">
        <Card className="h-fit">
          <CardHeader>
            <CardTitle>Investigator</CardTitle>
            <CardDescription>Changes to these settings are themselves recorded in the chain of custody.</CardDescription>
          </CardHeader>
          <CardContent>
            <SettingsForm initial={settings} />
          </CardContent>
        </Card>
        <Card className="h-fit">
          <CardHeader>
            <CardTitle>Security model</CardTitle>
            <CardDescription>Lumen {settings.version} · MIT licensed · by Lumen Forensics</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {SECURITY.map((s) => (
              <div key={s.title} className="flex gap-3">
                <s.icon className="mt-0.5 size-4 shrink-0 text-primary" />
                <div>
                  <div className="text-sm font-medium">{s.title}</div>
                  <p className="text-sm text-muted-foreground">{s.text}</p>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      </div>
    </>
  )
}
