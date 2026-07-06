import { timestampDate } from '@bufbuild/protobuf/wkt'
import type { Client } from '@connectrpc/connect'
import type { FileInfo, FilesService } from '../gen/service/v1/files_pb'
import type { SearchService } from '../gen/service/v1/search_pb'

// Pure request/response logic for the server functions in files.ts, kept
// separate (with clients injected) so it can be unit tested without the
// TanStack Start runtime.

/**
 * Plain-JSON projection of service.v1.FileInfo. Protobuf messages carry
 * bigints and Timestamp messages, which don't survive the server-function
 * serialization boundary; ids stay strings to avoid Number precision loss.
 */
export interface FileDto {
  id: string
  key: string
  contentType: string
  sizeBytes: number
  createdAt: string | null
}

export interface ListFilesInput {
  pageSize?: number
  pageToken?: string
}

export interface ListFilesResult {
  files: FileDto[]
  nextPageToken: string
}

export function toFileDto(file: FileInfo): FileDto {
  return {
    id: file.id.toString(),
    key: file.key,
    contentType: file.contentType,
    sizeBytes: Number(file.sizeBytes),
    createdAt: file.createdAt ? timestampDate(file.createdAt).toISOString() : null,
  }
}

export function validateListFilesInput(input: unknown): ListFilesInput {
  const data = (input ?? {}) as Record<string, unknown>
  const out: ListFilesInput = {}
  if (data.pageSize !== undefined) {
    if (
      typeof data.pageSize !== 'number' ||
      !Number.isInteger(data.pageSize) ||
      data.pageSize < 1
    ) {
      throw new Error('pageSize must be a positive integer')
    }
    out.pageSize = data.pageSize
  }
  if (data.pageToken !== undefined) {
    if (typeof data.pageToken !== 'string') {
      throw new Error('pageToken must be a string')
    }
    out.pageToken = data.pageToken
  }
  return out
}

export function validateIdInput(input: unknown): { id: string } {
  const data = (input ?? {}) as Record<string, unknown>
  if (typeof data.id !== 'string' || !/^\d+$/.test(data.id)) {
    throw new Error('id must be a numeric string')
  }
  return { id: data.id }
}

export async function listFilesImpl(
  client: Client<typeof SearchService>,
  input: ListFilesInput,
): Promise<ListFilesResult> {
  const res = await client.listFiles({
    pageSize: input.pageSize ?? 0,
    pageToken: input.pageToken ?? '',
  })
  return {
    files: res.files.map(toFileDto),
    nextPageToken: res.nextPageToken,
  }
}

export async function getDownloadUrlImpl(
  client: Client<typeof FilesService>,
  id: string,
): Promise<{ url: string }> {
  const res = await client.getDownloadURL({ ids: [BigInt(id)] })
  const spec = res.downloadUrls.find((u) => u.id.toString() === id)
  if (!spec) {
    throw new Error(`no download URL returned for file ${id}`)
  }
  return { url: spec.url }
}

export async function deleteFileImpl(
  client: Client<typeof FilesService>,
  id: string,
): Promise<void> {
  await client.deleteFile({ id: BigInt(id) })
}
