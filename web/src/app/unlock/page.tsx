import type { Metadata } from "next"
import { redirect } from "next/navigation"

import { UnlockScreen } from "@/components/unlock-screen"
import { vaultStatus } from "@/lib/api"

export const metadata: Metadata = { title: "Locked" }

export default async function UnlockPage() {
  const status = await vaultStatus()
  if (status?.authenticated) redirect("/")
  return <UnlockScreen status={status} />
}
