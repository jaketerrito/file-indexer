// Client-side driver for the resumable large-file upload path (see
// CreateMultipartUpload's doc comment in files.proto). Files below
// MULTIPART_THRESHOLD_BYTES keep using the plain single-PUT
// getUploadUrl/commitUpload flow in Browser.tsx; this module only handles
// the chunked path for large files.
//
// Resumability works by persisting {uploadId, fingerprint} in localStorage
// keyed by the destination key: if the same file (matched by name/size/
// mtime) is re-uploaded to the same destination after a reload or a failed
// attempt, listUploadedParts tells us which parts already landed in S3 so
// we only upload what's missing. A part that fails after retries leaves the
// session in place — the caller can simply try again later; nothing here
// aborts the multipart upload automatically, since doing so would defeat
// resumability.

/** Part size: comfortably above S3's 5 MiB per-part minimum. */
export const PART_SIZE_BYTES = 16 * 1024 * 1024

/** Files at or above this size use the chunked multipart path. */
export const MULTIPART_THRESHOLD_BYTES = 2 * PART_SIZE_BYTES

/** How many times to retry a single part before giving up on the upload. */
const PART_RETRY_ATTEMPTS = 3

export interface FileLike {
  name: string
  size: number
  lastModified: number
  slice(start: number, end: number): Blob
}

export interface UploadedPartInfo {
  partNumber: number
  size: number
}

export interface MultipartUploadDeps {
  createMultipartUpload(input: { key: string; contentType: string }): Promise<{ uploadId: string }>
  getUploadPartUrl(input: {
    key: string
    uploadId: string
    partNumber: number
  }): Promise<{ url: string }>
  listUploadedParts(input: {
    key: string
    uploadId: string
  }): Promise<{ parts: UploadedPartInfo[] }>
  completeMultipartUpload(input: { key: string; uploadId: string }): Promise<void>
  abortMultipartUpload(input: { key: string; uploadId: string }): Promise<void>
  /** PUTs one part's bytes to a presigned URL; must honor `signal` so a
   * large in-flight transfer can actually be interrupted, not just skipped
   * between parts. */
  putPart(url: string, body: Blob, signal: AbortSignal): Promise<void>
}

export interface UploadOptions {
  onProgress?: (completedParts: number, totalParts: number) => void
  /** Aborts the upload, including a part transfer already in flight. */
  signal?: AbortSignal
}

export class UploadCancelledError extends Error {
  constructor(key: string) {
    super(`upload of ${key} was cancelled`)
    this.name = 'UploadCancelledError'
  }
}

/** Stand-in for callers that don't pass a cancellation signal. */
const NEVER_ABORTS = new AbortController().signal

interface PartPlan {
  partNumber: number
  start: number
  end: number
}

/** Splits a file into PART_SIZE_BYTES-sized byte ranges, 1-indexed. */
export function planParts(fileSize: number, partSize: number = PART_SIZE_BYTES): PartPlan[] {
  const parts: PartPlan[] = []
  let partNumber = 1
  for (let start = 0; start < fileSize; start += partSize) {
    parts.push({ partNumber, start, end: Math.min(start + partSize, fileSize) })
    partNumber++
  }
  return parts
}

/**
 * Identifies a file across reloads well enough to decide whether a
 * persisted upload session still applies to it. Not cryptographic — a
 * false positive (different file, same name/size/mtime) just means a
 * resumed upload skips parts it shouldn't, which S3's own multipart
 * checksums would not catch either; this repo accepts the same tradeoff
 * every resumable-upload client (tus, rclone, etc.) makes without hashing
 * the whole file up front.
 */
export function fileFingerprint(file: FileLike): string {
  return `${file.name}:${file.size}:${file.lastModified}`
}

interface StoredSession {
  uploadId: string
  fingerprint: string
}

function loadSession(key: string): StoredSession | null {
  if (typeof localStorage === 'undefined') return null
  const raw = localStorage.getItem(`multipart-upload:${key}`)
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw)
    if (typeof parsed.uploadId === 'string' && typeof parsed.fingerprint === 'string') {
      return parsed as StoredSession
    }
  } catch {
    // Corrupt entry; treat as absent.
  }
  return null
}

function saveSession(key: string, session: StoredSession): void {
  if (typeof localStorage === 'undefined') return
  localStorage.setItem(`multipart-upload:${key}`, JSON.stringify(session))
}

function clearSession(key: string): void {
  if (typeof localStorage === 'undefined') return
  localStorage.removeItem(`multipart-upload:${key}`)
}

/**
 * Resumes uploadId from a persisted session if it matches this file and S3
 * still has it, otherwise starts a fresh multipart upload. Returns the
 * upload id together with the parts S3 already has for it.
 */
async function resumeOrCreateUpload(
  file: FileLike,
  key: string,
  contentType: string,
  deps: MultipartUploadDeps,
): Promise<{ uploadId: string; landed: Map<number, number> }> {
  const fingerprint = fileFingerprint(file)
  const existing = loadSession(key)
  if (existing && existing.fingerprint === fingerprint) {
    try {
      const { parts } = await deps.listUploadedParts({ key, uploadId: existing.uploadId })
      const landed = new Map(parts.map((p) => [p.partNumber, p.size]))
      return { uploadId: existing.uploadId, landed }
    } catch {
      // Session no longer valid server-side (expired/aborted upload id) —
      // fall through and start a fresh one.
    }
  }

  const { uploadId } = await deps.createMultipartUpload({ key, contentType })
  saveSession(key, { uploadId, fingerprint })
  return { uploadId, landed: new Map() }
}

async function uploadPartWithRetry(
  file: FileLike,
  key: string,
  uploadId: string,
  part: PartPlan,
  deps: MultipartUploadDeps,
  signal: AbortSignal,
): Promise<void> {
  let lastErr: unknown
  for (let attempt = 1; attempt <= PART_RETRY_ATTEMPTS; attempt++) {
    try {
      const { url } = await deps.getUploadPartUrl({ key, uploadId, partNumber: part.partNumber })
      await deps.putPart(url, file.slice(part.start, part.end), signal)
      return
    } catch (err) {
      // An abort interrupts the transfer immediately rather than waiting
      // for a retry budget that cancellation has already made pointless.
      if (signal.aborted) throw err
      lastErr = err
    }
  }
  throw new Error(
    `part ${part.partNumber} of ${key} failed after ${PART_RETRY_ATTEMPTS} attempts: ${String(lastErr)}`,
  )
}

/**
 * Uploads a large file via S3 multipart, resuming a prior attempt for the
 * same file/destination when one is on record. Throws (leaving the session
 * in place for a later retry) if any part exhausts its retries or the final
 * CompleteMultipartUpload call fails; clears the session only on success.
 */
export async function uploadFileMultipart(
  file: FileLike,
  key: string,
  contentType: string,
  deps: MultipartUploadDeps,
  options?: UploadOptions,
): Promise<void> {
  const { uploadId, landed } = await resumeOrCreateUpload(file, key, contentType, deps)
  const plan = planParts(file.size)
  const signal = options?.signal ?? NEVER_ABORTS

  const cancel = async (): Promise<never> => {
    await deps.abortMultipartUpload({ key, uploadId })
    clearSession(key)
    throw new UploadCancelledError(key)
  }

  let completed = 0
  for (const part of plan) {
    if (signal.aborted) await cancel()
    if (landed.get(part.partNumber) !== part.end - part.start) {
      try {
        await uploadPartWithRetry(file, key, uploadId, part, deps, signal)
      } catch (err) {
        if (signal.aborted) await cancel()
        throw err
      }
    }
    completed++
    options?.onProgress?.(completed, plan.length)
  }

  await deps.completeMultipartUpload({ key, uploadId })
  clearSession(key)
}
