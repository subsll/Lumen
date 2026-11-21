"use client"

import { useState } from "react"
import { useRouter } from "next/navigation"
import { CheckIcon, CopyIcon, KeyRoundIcon, Loader2Icon, PlusIcon, RefreshCwIcon, ShieldCheckIcon, TrashIcon, TriangleAlertIcon } from "lucide-react"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { send } from "@/lib/client"
import { formatUTC } from "@/lib/format"
import type { VaultKey, VaultStatus } from "@/lib/types"
import { registerSecurityKey, securityKeyProblem, useHydrated } from "@/lib/webauthn"

function RegisterKeyDialog({ first, onRecoveryCode }: { first: boolean; onRecoveryCode: (code: string) => void }) {
  const router = useRouter()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const problem = useHydrated() ? securityKeyProblem() : null

  async function onSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault()
    const label = String(new FormData(e.currentTarget).get("label") ?? "").trim() || "Security key"
    setBusy(true)
    setError(null)
    try {
      const key = await registerSecurityKey(label)
      const res = await send<{ key: VaultKey; recovery_code?: string; rotation_error?: string }>("POST", "/api/vault/keys", key)
      setOpen(false)
      toast.success(`${res.key.label} registered`)
      if (res.rotation_error) toast.error(`Re-encryption is unfinished: ${res.rotation_error}. It continues on the next unlock.`, { duration: 15000 })
      if (res.recovery_code) onRecoveryCode(res.recovery_code)
      router.refresh()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => { setOpen(o); setError(null) }}>
      <DialogTrigger asChild>
        <Button variant={first ? "default" : "outline"} size="sm">
          <PlusIcon /> {first ? "Register security key" : "Add key"}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <form onSubmit={onSubmit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>Register a security key</DialogTitle>
            <DialogDescription>
              Insert your YubiKey or other FIDO2 key. You will be asked for its PIN and to touch it, possibly twice.
              {first &&
                " Lumen then replaces the master key in .env with a new one that only this key and a recovery code can unlock, and re-encrypts your stored evidence under it. From then on Lumen starts locked."}
            </DialogDescription>
          </DialogHeader>
          {problem && (
            <Alert>
              <TriangleAlertIcon />
              <AlertDescription>{problem}</AlertDescription>
            </Alert>
          )}
          <Field>
            <FieldLabel htmlFor="label">Key name</FieldLabel>
            <Input id="label" name="label" maxLength={64} autoFocus placeholder="e.g. YubiKey 5C (primary)" />
            <FieldDescription>Shown on the unlock screen and in the chain of custody.</FieldDescription>
          </Field>
          {error && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
          <DialogFooter>
            <DialogClose asChild>
              <Button type="button" variant="outline">
                Cancel
              </Button>
            </DialogClose>
            <Button type="submit" disabled={busy || !!problem}>
              {busy ? <Loader2Icon className="animate-spin" /> : <KeyRoundIcon />}
              {busy ? "Touch your key…" : "Register"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function RecoveryCodeDialog({ code, open, first, onClose }: { code: string; open: boolean; first: boolean; onClose: () => void }) {
  const [copied, setCopied] = useState(false)
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle>Save your recovery code</DialogTitle>
          <DialogDescription>
            This code unlocks the vault if every security key is lost. It is shown only once and cannot be recovered.
          </DialogDescription>
        </DialogHeader>
        <div className="flex items-center gap-2 rounded-md border bg-muted/50 p-3">
          <code className="flex-1 font-mono text-sm break-all select-all">{code}</code>
          <Button
            variant="ghost"
            size="icon"
            aria-label="Copy recovery code"
            onClick={async () => {
              await navigator.clipboard.writeText(code)
              setCopied(true)
            }}
          >
            {copied ? <CheckIcon /> : <CopyIcon />}
          </Button>
        </div>
        <ol className="list-decimal space-y-1.5 pl-5 text-sm text-muted-foreground">
          <li>Store the code offline, for example printed in a safe, separate from your security keys.</li>
          <li>Register a second security key as a backup.</li>
          {first && (
            <li>
              Delete the old <code className="font-mono">LUMEN_MASTER_KEY</code> line from <code className="font-mono">.env</code>. It was replaced and
              no longer decrypts anything, so any copies or backups of it are useless too.
            </li>
          )}
        </ol>
        <DialogFooter>
          <Button onClick={onClose}>I have stored it</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function SecurityKeysCard({ status }: { status: VaultStatus }) {
  const router = useRouter()
  // The code stays set after closing so it does not vanish during the close animation.
  const [code, setCode] = useState("")
  const [codeOpen, setCodeOpen] = useState(false)
  const [codeIsFirst, setCodeIsFirst] = useState(false)
  const [busy, setBusy] = useState<string | null>(null)
  const keys = status.keys.filter((k) => k.type === "webauthn")
  const recovery = status.keys.find((k) => k.type === "recovery")

  async function remove(k: VaultKey) {
    if (!confirm(`Remove ${k.label}? It will no longer unlock the vault.`)) return
    setBusy(k.id)
    try {
      await send("DELETE", `/api/vault/keys/${k.id}`)
      toast.success(`${k.label} removed`)
      router.refresh()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  async function replaceRecovery() {
    if (!confirm("Replace the recovery code? The current code will stop working.")) return
    setBusy("recovery")
    try {
      const res = await send<{ recovery_code: string }>("POST", "/api/vault/recovery")
      setCodeIsFirst(false)
      setCode(res.recovery_code)
      setCodeOpen(true)
      router.refresh()
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(null)
    }
  }

  return (
    <Card className="h-fit">
      <CardHeader>
        <CardTitle>Security keys</CardTitle>
        <CardDescription>
          {status.protected
            ? `The vault starts locked and only these keys can unlock it. It locks after ${Math.round(status.idle_timeout_seconds / 60)} minutes without activity.`
            : "Require a YubiKey or other FIDO2 security key to unlock the vault and view evidence."}
        </CardDescription>
        <CardAction>
          <RegisterKeyDialog
            first={!status.protected}
            onRecoveryCode={(c) => {
              setCodeIsFirst(true)
              setCode(c)
              setCodeOpen(true)
            }}
          />
        </CardAction>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {status.protected && status.rotation_pending && (
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>Re-encryption is unfinished</AlertTitle>
            <AlertDescription>
              Some stored files are still under the previous master key. Keep <code className="font-mono">LUMEN_MASTER_KEY</code> in{" "}
              <code className="font-mono">.env</code> until this message disappears; Lumen finishes the job each time it is unlocked.
            </AlertDescription>
          </Alert>
        )}
        {status.protected && !status.rotation_pending && status.env_key_present && (
          <Alert>
            <TriangleAlertIcon />
            <AlertTitle>An old master key is still in .env</AlertTitle>
            <AlertDescription>
              It was replaced when you registered your first key and no longer decrypts anything. Delete the{" "}
              <code className="font-mono">LUMEN_MASTER_KEY</code> line from <code className="font-mono">.env</code> to tidy up.
            </AlertDescription>
          </Alert>
        )}
        {status.protected && keys.length === 1 && (
          <Alert>
            <TriangleAlertIcon />
            <AlertDescription>Only one security key is registered. Add a backup key so losing one does not lock you out.</AlertDescription>
          </Alert>
        )}

        {!status.protected ? (
          <Alert variant="destructive">
            <TriangleAlertIcon />
            <AlertTitle>Not protected yet</AlertTitle>
            <AlertDescription>
              Right now the vault opens with <code className="font-mono">LUMEN_MASTER_KEY</code> from <code className="font-mono">.env</code>, so anyone
              with that file, or with access to Lumen, can view evidence. Register a security key to replace that master key: your security keys and
              a recovery code become the only way to unlock the vault.
            </AlertDescription>
          </Alert>
        ) : (
          <ul className="divide-y rounded-md border">
            {keys.map((k) => (
              <li key={k.id} className="flex items-center gap-3 p-3">
                <KeyRoundIcon className="size-4 shrink-0 text-primary" />
                <div className="min-w-0 flex-1">
                  <div className="truncate text-sm font-medium">{k.label}</div>
                  <div className="text-xs text-muted-foreground">
                    Registered {formatUTC(k.created_at)}
                    {k.rp_id && <> · {k.rp_id}</>}
                    {k.last_used_at && <> · last used {formatUTC(k.last_used_at)}</>}
                  </div>
                </div>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={`Remove ${k.label}`}
                  disabled={keys.length <= 1 || busy === k.id}
                  title={keys.length <= 1 ? "Register another key before removing the last one" : undefined}
                  onClick={() => remove(k)}
                >
                  {busy === k.id ? <Loader2Icon className="animate-spin" /> : <TrashIcon />}
                </Button>
              </li>
            ))}
            {recovery && (
              <li className="flex items-center gap-3 p-3">
                <ShieldCheckIcon className="size-4 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">Recovery code</div>
                  <div className="text-xs text-muted-foreground">
                    Created {formatUTC(recovery.created_at)}
                    {recovery.last_used_at && <> · last used {formatUTC(recovery.last_used_at)}</>}
                  </div>
                </div>
                <Button variant="ghost" size="sm" disabled={busy === "recovery"} onClick={replaceRecovery}>
                  {busy === "recovery" ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />} Replace
                </Button>
              </li>
            )}
          </ul>
        )}
      </CardContent>
      <RecoveryCodeDialog code={code} open={codeOpen} first={codeIsFirst} onClose={() => setCodeOpen(false)} />
    </Card>
  )
}
