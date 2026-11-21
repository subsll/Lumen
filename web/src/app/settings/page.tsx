import type { Metadata } from "next"
import { KeyRoundIcon, LockIcon, NetworkIcon, ShieldCheckIcon } from "lucide-react"

import { SettingsForm } from "@/components/actions"
import { PageHeader } from "@/components/page-header"
import { SecurityKeysCard } from "@/components/security-keys"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { api, vaultStatus } from "@/lib/api"
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
    title: "Hardware-key unlock",
    text: "With a security key registered, the master key exists only wrapped by your keys and recovery code. The vault starts locked, and unlocking, locking and key changes are recorded in the chain of custody.",
  },
  {
    icon: NetworkIcon,
    title: "Local only",
    text: "Lumen listens on localhost only. Without a security key it has no login, so do not expose it to a network.",
  },
]

export default async function SettingsPage() {
  const [settings, vault] = await Promise.all([api<Settings>("/api/settings"), vaultStatus()])
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
        <Card className="h-fit lg:row-span-2">
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
        {vault && <SecurityKeysCard status={vault} />}
      </div>
    </>
  )
}
