import "server-only"

import { cookies } from "next/headers"
import { notFound, redirect } from "next/navigation"
import { connection } from "next/server"

import type { VaultStatus } from "@/lib/types"

const API_URL = process.env.API_INTERNAL_URL ?? "http://localhost:8080"
const SESSION_COOKIE = "lumen_session"

export class ApiUnavailableError extends Error {}

/** Forwards the browser's Lumen session to the API. */
async function sessionHeaders(): Promise<HeadersInit> {
  const session = (await cookies()).get(SESSION_COOKIE)
  return session ? { Cookie: `${SESSION_COOKIE}=${session.value}` } : {}
}

async function request(path: string): Promise<Response> {
  await connection()
  try {
    return await fetch(`${API_URL}${path}`, { cache: "no-store", headers: await sessionHeaders() })
  } catch {
    throw new ApiUnavailableError(`The Lumen API is not reachable at ${API_URL}.`)
  }
}

/** Server-side GET against the Lumen API. Always fresh: evidence data is never cached. */
export async function api<T>(path: string): Promise<T> {
  const res = await request(path)
  if (res.status === 404) notFound()
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    if (res.status === 401 && body.locked) redirect("/unlock")
    throw new Error(body.error ?? `API ${path} failed with ${res.status}`)
  }
  return res.json() as Promise<T>
}

/** Vault lock status, or null if the API or vault cannot be reached. */
export async function vaultStatus(): Promise<VaultStatus | null> {
  try {
    const res = await request("/api/vault")
    return res.ok ? ((await res.json()) as VaultStatus) : null
  } catch {
    return null
  }
}
