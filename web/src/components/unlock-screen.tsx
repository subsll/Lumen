"use client"

import { useState } from "react"
import { usePathname } from "next/navigation"
import { KeyRoundIcon, Loader2Icon, LockIcon, TriangleAlertIcon } from "lucide-react"

import { LumenMark } from "@/components/logo"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { send } from "@/lib/client"
import type { VaultStatus } from "@/lib/types"
import { assertSecurityKey, securityKeyProblem, useHydrated } from "@/lib/webauthn"

export function UnlockScreen({ status }: { status: VaultStatus | null }) {
  const pathname = usePathname()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [recovery, setRecovery] = useState(false)
  const hydrated = useHydrated()
  const problem = hydrated ? securityKeyProblem() : null

  const keys = (status?.keys ?? []).filter((k) => k.type === "webauthn")
  const here = hydrated ? location.hostname : ""
  const usable = hydrated ? keys.filter((k) => !k.rp_id || k.rp_id === here) : []
  const otherHost = keys.find((k) => k.rp_id && k.rp_id !== here)?.rp_id
  const minutes = Math.round((status?.idle_timeout_seconds ?? 900) / 60)

  function done() {
    // Full reload so every server component renders with the new session.
    window.location.assign(pathname === "/unlock" ? "/" : window.location.href)
  }

  async function unlockWithKey() {
    setBusy(true)
    setError(null)
    try {
      const proof = await assertSecurityKey(usable)
      await send("POST", "/api/vault/unlock", proof)
      done()
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  async function unlockWithCode(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const code = String(new FormData(e.currentTarget).get("code") ?? "")
    setBusy(true)
    setError(null)
    try {
      await send("POST", "/api/vault/unlock", { recovery_code: code })
      done()
    } catch (err) {
      setError((err as Error).message)
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-md">
        <CardHeader className="items-center text-center">
          <div className="relative mx-auto mb-2 w-fit">
            <LumenMark className="size-12" />
            <span className="absolute -right-2 -bottom-2 flex size-6 items-center justify-center rounded-full border bg-background">
              <LockIcon className="size-3.5" />
            </span>
          </div>
          <CardTitle className="text-xl">Lumen is locked</CardTitle>
          <CardDescription>
            Evidence is encrypted. {recovery ? "Enter your recovery code to unlock the vault." : "Insert your security key and touch it to unlock the vault."}
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {!status && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertDescription>The vault is not reachable. Check <code className="font-mono">docker compose logs vault</code>.</AlertDescription>
            </Alert>
          )}
          {problem && !recovery && (
            <Alert>
              <TriangleAlertIcon />
              <AlertDescription>{problem}</AlertDescription>
            </Alert>
          )}
          {!problem && !recovery && !usable.length && otherHost && (
            <Alert>
              <TriangleAlertIcon />
              <AlertDescription>
                Your security keys were registered on <span className="font-mono">{otherHost}</span>. Open Lumen at that address to use them.
              </AlertDescription>
            </Alert>
          )}
          {error && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}

          {recovery ? (
            <form onSubmit={unlockWithCode} className="flex flex-col gap-4">
              <Field>
                <FieldLabel htmlFor="code">Recovery code</FieldLabel>
                <Input id="code" name="code" required autoFocus autoComplete="off" spellCheck={false} placeholder="XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX" className="font-mono" />
              </Field>
              <Button type="submit" disabled={busy || !status}>
                {busy ? <Loader2Icon className="animate-spin" /> : <KeyRoundIcon />} Unlock
              </Button>
            </form>
          ) : (
            <Button size="lg" onClick={unlockWithKey} disabled={busy || !!problem || !usable.length}>
              {busy ? <Loader2Icon className="animate-spin" /> : <KeyRoundIcon />}
              {busy ? "Touch your security key…" : "Unlock with security key"}
            </Button>
          )}
        </CardContent>
        <CardFooter className="flex-col gap-2 text-center text-xs text-muted-foreground">
          <Button variant="link" size="sm" className="h-auto p-0" onClick={() => { setRecovery(!recovery); setError(null) }}>
            {recovery ? "Use a security key instead" : "Lost your key? Use the recovery code"}
          </Button>
          <p>Unlocking is recorded in the chain of custody. Lumen locks again after {minutes} minutes without activity.</p>
        </CardFooter>
      </Card>
    </div>
  )
}
