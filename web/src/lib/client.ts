"use client"

import { createSHA256 } from "hash-wasm"

const HEADERS = { "X-Lumen-Request": "1" }

/** JSON request from the browser. Writes carry X-Lumen-Request (CSRF guard). */
export async function send<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: { ...HEADERS, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error ?? `Request failed (${res.status})`)
  return data as T
}

export async function get<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, { signal })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error ?? `Request failed (${res.status})`)
  return data as T
}

const CHUNK = 8 * 1024 * 1024

/** Streams a file through SHA-256 in the browser, before it leaves the machine. */
export async function sha256File(file: File, onProgress?: (fraction: number) => void): Promise<string> {
  const hasher = await createSHA256()
  hasher.init()
  for (let offset = 0; offset < file.size; offset += CHUNK) {
    const buf = await file.slice(offset, offset + CHUNK).arrayBuffer()
    hasher.update(new Uint8Array(buf))
    onProgress?.(Math.min(1, (offset + CHUNK) / file.size))
  }
  onProgress?.(1)
  return hasher.digest("hex")
}

/** Multipart upload with progress. Metadata fields must precede the file. */
export function uploadWithProgress<T>(
  path: string,
  fields: Record<string, string>,
  file: File,
  onProgress?: (fraction: number) => void,
): Promise<T> {
  const form = new FormData()
  for (const [k, v] of Object.entries(fields)) form.append(k, v)
  form.append("file", file, file.name)
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open("POST", path)
    xhr.setRequestHeader("X-Lumen-Request", "1")
    xhr.upload.onprogress = (e) => e.lengthComputable && onProgress?.(e.loaded / e.total)
    xhr.onload = () => {
      let data: { error?: string } = {}
      try {
        data = JSON.parse(xhr.responseText)
      } catch {}
      if (xhr.status >= 200 && xhr.status < 300) resolve(data as T)
      else reject(new Error(data.error ?? `Upload failed (${xhr.status})`))
    }
    xhr.onerror = () => reject(new Error("Network error during upload"))
    xhr.send(form)
  })
}
