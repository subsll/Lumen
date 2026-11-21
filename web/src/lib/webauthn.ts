"use client"

// Security keys (YubiKey and other FIDO2 keys) via the WebAuthn PRF extension.
//
// PRF (CTAP2 "hmac-secret") lets the key compute HMAC(secret-inside-the-key, salt).
// Lumen sends that 32-byte output to the vault, which uses it to unwrap the
// master key. The secret never leaves the security key, so only that physical
// key can unlock the vault. User verification (the key's PIN) is required:
// the key derives a different output without it, so a stolen key is not enough.

import { useSyncExternalStore } from "react"

import type { VaultKey } from "@/lib/types"

function bytes(buf: ArrayBuffer | ArrayBufferView): Uint8Array {
  return buf instanceof ArrayBuffer ? new Uint8Array(buf) : new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength)
}

export function b64url(buf: ArrayBuffer | ArrayBufferView): string {
  let s = ""
  for (const b of bytes(buf)) s += String.fromCharCode(b)
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
}

export function fromB64url(s: string): Uint8Array<ArrayBuffer> {
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/"))
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

function random(n: number): Uint8Array<ArrayBuffer> {
  return crypto.getRandomValues(new Uint8Array(n))
}

type PrfResult = { enabled?: boolean; results?: { first?: ArrayBuffer | ArrayBufferView } }
const prfOf = (c: PublicKeyCredential) => (c.getClientExtensionResults() as { prf?: PrfResult }).prf

/** Why security keys cannot be used on this page, or null if they can. */
export function securityKeyProblem(): string | null {
  if (typeof window === "undefined") return null
  if (!window.PublicKeyCredential || !navigator.credentials) return "This browser does not support security keys."
  const host = location.hostname
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(host) || host.includes(":")) {
    return `Security keys cannot be used on an IP address. Open Lumen at http://localhost${location.port ? `:${location.port}` : ""} instead.`
  }
  if (!window.isSecureContext) return "Security keys need HTTPS, or Lumen opened on localhost."
  return null
}

function friendly(e: unknown): Error {
  if (e instanceof DOMException) {
    if (e.name === "NotAllowedError") return new Error("The request was cancelled or timed out. Try again and touch your key when it blinks.")
    if (e.name === "InvalidStateError") return new Error("This security key is already registered.")
    if (e.name === "SecurityError") return new Error(securityKeyProblem() ?? e.message)
  }
  return e instanceof Error ? e : new Error(String(e))
}

const NO_PRF =
  "This security key or browser does not support the PRF extension (hmac-secret). Use a YubiKey 5 series or another FIDO2 key with hmac-secret, in a current version of Chrome, Edge, Firefox or Safari."

/** Asks a registered key for its PRF output. Resolves with the key that answered. */
export async function assertSecurityKey(keys: Pick<VaultKey, "credential_id" | "salt">[]) {
  const usable = keys.filter((k) => k.credential_id && k.salt)
  if (!usable.length) throw new Error("No security key is registered for this address.")
  let cred: PublicKeyCredential | null
  try {
    cred = (await navigator.credentials.get({
      publicKey: {
        challenge: random(32),
        rpId: location.hostname,
        timeout: 120_000,
        userVerification: "required",
        allowCredentials: usable.map((k) => ({ type: "public-key", id: fromB64url(k.credential_id!), transports: ["usb", "nfc"] })),
        hints: ["security-key"],
        extensions: { prf: { evalByCredential: Object.fromEntries(usable.map((k) => [k.credential_id!, { first: fromB64url(k.salt!) }])) } },
      } as PublicKeyCredentialRequestOptions,
    })) as PublicKeyCredential | null
  } catch (e) {
    throw friendly(e)
  }
  if (!cred) throw new Error("No security key responded.")
  const out = prfOf(cred)?.results?.first
  if (!out) throw new Error(NO_PRF)
  return { credential_id: b64url(cred.rawId), secret: b64url(out) }
}

/** Registers a new security key and returns what the vault needs to wrap the master key for it. */
export async function registerSecurityKey(label: string) {
  const salt = random(32)
  let cred: PublicKeyCredential | null
  try {
    cred = (await navigator.credentials.create({
      publicKey: {
        rp: { name: "Lumen", id: location.hostname },
        user: { id: random(16), name: label, displayName: label },
        challenge: random(32),
        pubKeyCredParams: [
          { type: "public-key", alg: -7 }, // ES256
          { type: "public-key", alg: -8 }, // EdDSA
          { type: "public-key", alg: -257 }, // RS256
        ],
        authenticatorSelection: { authenticatorAttachment: "cross-platform", residentKey: "discouraged", userVerification: "required" },
        attestation: "none",
        timeout: 120_000,
        hints: ["security-key"],
        extensions: { prf: { eval: { first: salt } } },
      } as PublicKeyCredentialCreationOptions,
    })) as PublicKeyCredential | null
  } catch (e) {
    throw friendly(e)
  }
  if (!cred) throw new Error("No security key responded.")
  const credential_id = b64url(cred.rawId)
  const prf = prfOf(cred)
  if (prf?.enabled === false) throw new Error(NO_PRF)

  // Some browsers only evaluate PRF when signing in, so ask the key once more.
  let secret = prf?.results?.first ? b64url(prf.results.first) : null
  if (!secret) secret = (await assertSecurityKey([{ credential_id, salt: b64url(salt) }])).secret
  return { label, credential_id, rp_id: location.hostname, salt: b64url(salt), secret }
}

const noop = () => () => {}

/** True after hydration; avoids server/client mismatches for browser-only checks. */
export function useHydrated(): boolean {
  return useSyncExternalStore(noop, () => true, () => false)
}
