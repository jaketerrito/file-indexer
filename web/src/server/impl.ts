import { timestampDate } from '@bufbuild/protobuf/wkt'
import type { Client } from '@connectrpc/connect'
import type { FileInfo, FilesService } from '../gen/service/v1/files_pb'
import { type SearchService, SortField, SortOrder } from '../gen/service/v1/search_pb'

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

export const SORT_FIELDS = ['key', 'lastModified', 'size'] as const
export type SortFieldInput = (typeof SORT_FIELDS)[number]

export const SORT_ORDERS = ['asc', 'desc'] as const
export type SortOrderInput = (typeof SORT_ORDERS)[number]

const SORT_FIELD_PB: Record<SortFieldInput, SortField> = {
  key: SortField.KEY,
  lastModified: SortField.LAST_MODIFIED,
  size: SortField.SIZE,
}

const SORT_ORDER_PB: Record<SortOrderInput, SortOrder> = {
  asc: SortOrder.ASC,
  desc: SortOrder.DESC,
}

export interface ListFilesInput {
  pageSize?: number
  pageToken?: string
  /** Only return files whose key starts with this prefix. */
  prefix?: string
  /** Exact MIME type ("image/png") or category prefix ("image/"). */
  contentType?: string
  sortField?: SortFieldInput
  sortOrder?: SortOrderInput
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
  if (data.prefix !== undefined) {
    if (typeof data.prefix !== 'string') {
      throw new Error('prefix must be a string')
    }
    out.prefix = data.prefix
  }
  if (data.contentType !== undefined) {
    if (typeof data.contentType !== 'string') {
      throw new Error('contentType must be a string')
    }
    out.contentType = data.contentType
  }
  if (data.sortField !== undefined) {
    if (!SORT_FIELDS.includes(data.sortField as SortFieldInput)) {
      throw new Error(`sortField must be one of ${SORT_FIELDS.join(', ')}`)
    }
    out.sortField = data.sortField as SortFieldInput
  }
  if (data.sortOrder !== undefined) {
    if (!SORT_ORDERS.includes(data.sortOrder as SortOrderInput)) {
      throw new Error(`sortOrder must be one of ${SORT_ORDERS.join(', ')}`)
    }
    out.sortOrder = data.sortOrder as SortOrderInput
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
    prefix: input.prefix ?? '',
    contentType: input.contentType ?? '',
    // Explicit defaults match the server's (KEY ascending).
    sortField: SORT_FIELD_PB[input.sortField ?? 'key'],
    sortOrder: SORT_ORDER_PB[input.sortOrder ?? 'asc'],
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
