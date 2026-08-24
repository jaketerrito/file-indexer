import { timestampDate } from '@bufbuild/protobuf/wkt'
import type { Client } from '@connectrpc/connect'
import type { ExifMetadata, FileInfo, FilesService } from '../gen/service/v1/files_pb'
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
  /**
   * Presigned inline URL for this file's preview image, or null when the file
   * has no preview. Resolved server-side by listFilesImpl so the browser needs
   * only one request per page.
   */
  previewUrl: string | null
  /** Intrinsic dimensions of the preview, used to reserve layout space. */
  previewWidth: number | null
  previewHeight: number | null
}

/**
 * Plain-JSON projection of service.v1.ExifMetadata. Present only when the
 * file has been through the exif indexer and carries EXIF or XMP data;
 * fields absent in the source (sparse by nature) stay undefined rather than
 * being coerced to a zero value.
 */
export interface ExifMetadataDto {
  imageType?: string
  cameraMake?: string
  cameraModel?: string
  cameraSerial?: string
  lensMake?: string
  lensModel?: string
  takenAt?: string
  iso?: number
  fNumber?: number
  exposureTime?: number
  focalLength?: number
  focalLength35mm?: number
  exposureProgram?: number
  meteringMode?: number
  flash?: number
  orientation?: number
  imageWidth?: number
  imageHeight?: number
  gpsLatitude?: number
  gpsLongitude?: number
  gpsAltitude?: number
  gpsAt?: string
  software?: string
  artist?: string
  copyright?: string
  imageDescription?: string
  xmpTitle?: string
  xmpDescription?: string
  xmpCreator?: string
  xmpLabel?: string
  xmpRating?: number
  xmpKeywords: string[]
  xmpCreateDate?: string
  hasExif: boolean
  hasXmp: boolean
}

/**
 * Full metadata projection for the file metadata modal: everything in
 * FileDto plus updatedAt and, when present, EXIF/XMP data.
 */
export interface FileMetadataDto {
  id: string
  key: string
  contentType: string
  sizeBytes: number
  createdAt: string | null
  updatedAt: string | null
  exif: ExifMetadataDto | null
}

export function toExifMetadataDto(exif: ExifMetadata): ExifMetadataDto {
  return {
    imageType: exif.imageType,
    cameraMake: exif.cameraMake,
    cameraModel: exif.cameraModel,
    cameraSerial: exif.cameraSerial,
    lensMake: exif.lensMake,
    lensModel: exif.lensModel,
    takenAt: exif.takenAt ? timestampDate(exif.takenAt).toISOString() : undefined,
    iso: exif.iso,
    fNumber: exif.fNumber,
    exposureTime: exif.exposureTime,
    focalLength: exif.focalLength,
    focalLength35mm: exif.focalLength35mm,
    exposureProgram: exif.exposureProgram,
    meteringMode: exif.meteringMode,
    flash: exif.flash,
    orientation: exif.orientation,
    imageWidth: exif.imageWidth,
    imageHeight: exif.imageHeight,
    gpsLatitude: exif.gpsLatitude,
    gpsLongitude: exif.gpsLongitude,
    gpsAltitude: exif.gpsAltitude,
    gpsAt: exif.gpsAt ? timestampDate(exif.gpsAt).toISOString() : undefined,
    software: exif.software,
    artist: exif.artist,
    copyright: exif.copyright,
    imageDescription: exif.imageDescription,
    xmpTitle: exif.xmpTitle,
    xmpDescription: exif.xmpDescription,
    xmpCreator: exif.xmpCreator,
    xmpLabel: exif.xmpLabel,
    xmpRating: exif.xmpRating,
    xmpKeywords: exif.xmpKeywords,
    xmpCreateDate: exif.xmpCreateDate ? timestampDate(exif.xmpCreateDate).toISOString() : undefined,
    hasExif: exif.hasExif,
    hasXmp: exif.hasXmp,
  }
}

export function toFileMetadataDto(file: FileInfo): FileMetadataDto {
  return {
    id: file.id.toString(),
    key: file.key,
    contentType: file.contentType,
    sizeBytes: Number(file.sizeBytes),
    createdAt: file.createdAt ? timestampDate(file.createdAt).toISOString() : null,
    updatedAt: file.updatedAt ? timestampDate(file.updatedAt).toISOString() : null,
    exif: file.exif ? toExifMetadataDto(file.exif) : null,
  }
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
  const hasPreview = file.previewKey !== ''
  return {
    id: file.id.toString(),
    key: file.key,
    contentType: file.contentType,
    sizeBytes: Number(file.sizeBytes),
    createdAt: file.createdAt ? timestampDate(file.createdAt).toISOString() : null,
    // Filled in by listFilesImpl; toFileDto has no storage client.
    previewUrl: null,
    previewWidth: hasPreview ? file.previewWidth : null,
    previewHeight: hasPreview ? file.previewHeight : null,
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
  search: Client<typeof SearchService>,
  files: Client<typeof FilesService>,
  input: ListFilesInput,
): Promise<ListFilesResult> {
  const res = await search.listFiles({
    pageSize: input.pageSize ?? 0,
    pageToken: input.pageToken ?? '',
    prefix: input.prefix ?? '',
    contentType: input.contentType ?? '',
    // Explicit defaults match the server's (KEY ascending).
    sortField: SORT_FIELD_PB[input.sortField ?? 'key'],
    sortOrder: SORT_ORDER_PB[input.sortOrder ?? 'asc'],
  })

  const dtos = res.files.map(toFileDto)

  // Preview URLs are presigned here rather than fetched by the browser: the
  // signature is computed locally (no storage round trip) and both hops are
  // in-cluster, so a page of thumbnails costs a couple of milliseconds instead
  // of a second client round trip.
  const ids = res.files.filter((f) => f.previewKey !== '').map((f) => f.id)
  if (ids.length > 0) {
    try {
      const previews = await files.getPreviewURL({ ids })
      const byId = new Map(previews.previewUrls.map((p) => [p.id.toString(), p.url]))
      for (const dto of dtos) {
        dto.previewUrl = byId.get(dto.id) ?? null
      }
    } catch (err) {
      // Thumbnails are decoration. Degrade to a list without them rather than
      // failing the whole page.
      console.error('preview URL lookup failed', err)
    }
  }

  return { files: dtos, nextPageToken: res.nextPageToken }
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

export async function getFileMetadataImpl(
  client: Client<typeof FilesService>,
  id: string,
): Promise<FileMetadataDto> {
  const res = await client.getFileInfo({ id: BigInt(id) })
  if (!res.file) {
    throw new Error(`no file returned for id ${id}`)
  }
  return toFileMetadataDto(res.file)
}

export async function deleteFileImpl(
  client: Client<typeof FilesService>,
  id: string,
): Promise<void> {
  await client.deleteFile({ id: BigInt(id) })
}

export function validateKeyInput(input: unknown): { key: string } {
  const data = (input ?? {}) as Record<string, unknown>
  if (typeof data.key !== 'string' || data.key === '') {
    throw new Error('key must be a non-empty string')
  }
  return { key: data.key }
}

export async function getUploadUrlImpl(
  client: Client<typeof FilesService>,
  key: string,
): Promise<{ url: string }> {
  const res = await client.getUploadURL({ key })
  return { url: res.url }
}

/**
 * Confirms an upload PUT completed and registers the object with the index
 * (reference-based: only the key is sent, the API re-stats S3 itself).
 * Returns a stat-derived FileDto; preview/EXIF fields are unset until their
 * indexers run.
 */
export async function commitUploadImpl(
  client: Client<typeof FilesService>,
  key: string,
): Promise<FileDto> {
  const res = await client.commitUpload({ key })
  if (!res.file) {
    throw new Error(`no file returned committing upload for key ${key}`)
  }
  return toFileDto(res.file)
}

export interface ListDirectoryInput {
  /** Directory to list; "" is the bucket root. */
  path?: string
  pageSize?: number
  pageToken?: string
  sortField?: SortFieldInput
  sortOrder?: SortOrderInput
}

export interface ListDirectoryResult {
  /** Immediate child directories, full path from the bucket root, ending in "/". */
  directories: string[]
  files: FileDto[]
  nextPageToken: string
}

export function validateListDirectoryInput(input: unknown): ListDirectoryInput {
  const data = (input ?? {}) as Record<string, unknown>
  const out: ListDirectoryInput = {}
  if (data.path !== undefined) {
    if (typeof data.path !== 'string') {
      throw new Error('path must be a string')
    }
    out.path = data.path
  }
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

/**
 * Lists a directory's immediate children: subdirectories (derived purely
 * from key structure — S3 has no directory objects) followed by files
 * directly in it. Preview URLs are resolved the same way listFilesImpl does.
 */
export async function listDirectoryImpl(
  search: Client<typeof SearchService>,
  files: Client<typeof FilesService>,
  input: ListDirectoryInput,
): Promise<ListDirectoryResult> {
  const res = await search.listDirectory({
    path: input.path ?? '',
    pageSize: input.pageSize ?? 0,
    pageToken: input.pageToken ?? '',
    sortField: SORT_FIELD_PB[input.sortField ?? 'key'],
    sortOrder: SORT_ORDER_PB[input.sortOrder ?? 'asc'],
  })

  const dtos = res.files.map(toFileDto)

  const ids = res.files.filter((f) => f.previewKey !== '').map((f) => f.id)
  if (ids.length > 0) {
    try {
      const previews = await files.getPreviewURL({ ids })
      const byId = new Map(previews.previewUrls.map((p) => [p.id.toString(), p.url]))
      for (const dto of dtos) {
        dto.previewUrl = byId.get(dto.id) ?? null
      }
    } catch (err) {
      console.error('preview URL lookup failed', err)
    }
  }

  return { directories: res.directories, files: dtos, nextPageToken: res.nextPageToken }
}

export function validatePathInput(input: unknown): { path: string } {
  const data = (input ?? {}) as Record<string, unknown>
  if (typeof data.path !== 'string' || data.path === '') {
    throw new Error('path must be a non-empty string')
  }
  return { path: data.path }
}

export interface DirectoryStatsDto {
  fileCount: number
  totalBytes: number
}

export async function getDirectoryStatsImpl(
  client: Client<typeof FilesService>,
  path: string,
): Promise<DirectoryStatsDto> {
  const res = await client.getDirectoryStats({ path })
  return { fileCount: Number(res.fileCount), totalBytes: Number(res.totalBytes) }
}

export async function deleteDirectoryImpl(
  client: Client<typeof FilesService>,
  path: string,
): Promise<{ deletedCount: number }> {
  const res = await client.deleteDirectory({ path })
  return { deletedCount: Number(res.deletedCount) }
}
