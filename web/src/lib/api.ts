import "server-only"

import { notFound } from "next/navigation"
import { connection } from "next/server"

const API_URL = process.env.API_INTERNAL_URL ?? "http://localhost:8080"

export class ApiUnavailableError extends Error {}

/** Server-side GET against the Lumen API. Always fresh: evidence data is never cached. */
export async function api<T>(path: string): Promise<T> {
  await connection()
  let res: Response
  try {
    res = await fetch(`${API_URL}${path}`, { cache: "no-store" })
  } catch {
    throw new ApiUnavailableError(`The Lumen API is not reachable at ${API_URL}.`)
  }
  if (res.status === 404) notFound()
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error ?? `API ${path} failed with ${res.status}`)
  }
  return res.json() as Promise<T>
}
