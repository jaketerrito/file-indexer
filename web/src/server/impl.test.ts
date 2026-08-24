import { create } from '@bufbuild/protobuf'
import { timestampFromDate } from '@bufbuild/protobuf/wkt'
import type { Client } from '@connectrpc/connect'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  CommitUploadResponseSchema,
  DeleteDirectoryResponseSchema,
  DeleteFileResponseSchema,
  DownloadURLSpecSchema,
  ExifMetadataSchema,
  FileInfoSchema,
  type FilesService,
  GetDirectoryStatsResponseSchema,
  GetDownloadURLResponseSchema,
  GetFileInfoResponseSchema,
  GetPreviewURLResponseSchema,
  GetUploadURLResponseSchema,
  PreviewURLSpecSchema,
} from '../gen/service/v1/files_pb'
import {
  ListDirectoryResponseSchema,
  ListFilesResponseSchema,
  type SearchService,
  SortField,
  SortOrder,
} from '../gen/service/v1/search_pb'
import {
  commitUploadImpl,
  deleteDirectoryImpl,
  deleteFileImpl,
  getDirectoryStatsImpl,
  getDownloadUrlImpl,
  getFileMetadataImpl,
  getUploadUrlImpl,
  listDirectoryImpl,
  listFilesImpl,
  toFileDto,
  toFileMetadataDto,
  validateIdInput,
  validateKeyInput,
  validateListDirectoryInput,
  validateListFilesInput,
  validatePathInput,
} from './impl'

const CREATED_AT = new Date('2026-01-02T03:04:05.000Z')

function fileInfo(
  overrides: {
    id?: bigint
    key?: string
    previewKey?: string
    previewWidth?: number
    previewHeight?: number
  } = {},
) {
  return create(FileInfoSchema, {
    id: overrides.id ?? 1n,
    key: overrides.key ?? 'docs/report.pdf',
    contentType: 'application/pdf',
    sizeBytes: 1024n,
    createdAt: timestampFromDate(CREATED_AT),
    previewKey: overrides.previewKey ?? '',
    previewWidth: overrides.previewWidth ?? 0,
    previewHeight: overrides.previewHeight ?? 0,
  })
}

/** A no-op files client stub for listFilesImpl calls that expect no previews. */
function noPreviewFilesClient(): Client<typeof FilesService> {
  return {
    getPreviewURL: vi.fn(),
  } as unknown as Client<typeof FilesService>
}

describe('toFileDto', () => {
  it('maps proto fields to plain JSON-safe values', () => {
    const dto = toFileDto(fileInfo({ id: 9007199254740993n }))
    expect(dto).toEqual({
      // Larger than Number.MAX_SAFE_INTEGER: must round-trip as a string.
      id: '9007199254740993',
      key: 'docs/report.pdf',
      contentType: 'application/pdf',
      sizeBytes: 1024,
      createdAt: '2026-01-02T03:04:05.000Z',
      previewUrl: null,
      previewWidth: null,
      previewHeight: null,
    })
  })

  it('maps a missing created_at to null', () => {
    const file = create(FileInfoSchema, { id: 1n, key: 'a' })
    expect(toFileDto(file).createdAt).toBeNull()
  })

  it('carries preview dimensions when the file has a preview', () => {
    const dto = toFileDto(
      fileInfo({ previewKey: '.index/previews/1', previewWidth: 320, previewHeight: 160 }),
    )
    expect(dto.previewWidth).toBe(320)
    expect(dto.previewHeight).toBe(160)
    // previewUrl is resolved later by listFilesImpl, not by toFileDto itself.
    expect(dto.previewUrl).toBeNull()
  })

  it('reports null preview dimensions when the file has no preview', () => {
    const dto = toFileDto(fileInfo())
    expect(dto.previewWidth).toBeNull()
    expect(dto.previewHeight).toBeNull()
  })
})

describe('validateListFilesInput', () => {
  it('accepts empty input', () => {
    expect(validateListFilesInput(undefined)).toEqual({})
    expect(validateListFilesInput({})).toEqual({})
  })

  it('accepts pageSize and pageToken', () => {
    expect(validateListFilesInput({ pageSize: 10, pageToken: 'abc' })).toEqual({
      pageSize: 10,
      pageToken: 'abc',
    })
  })

  it('accepts filter and sort fields', () => {
    expect(
      validateListFilesInput({
        prefix: 'docs/',
        contentType: 'image/',
        sortField: 'size',
        sortOrder: 'desc',
      }),
    ).toEqual({
      prefix: 'docs/',
      contentType: 'image/',
      sortField: 'size',
      sortOrder: 'desc',
    })
  })

  it.each([
    [{ prefix: 42 }, /prefix/],
    [{ contentType: 42 }, /contentType/],
    [{ sortField: 'bogus' }, /sortField/],
    [{ sortField: 1 }, /sortField/],
    [{ sortOrder: 'up' }, /sortOrder/],
  ] as const)('rejects invalid filter input %j', (input, want) => {
    expect(() => validateListFilesInput(input)).toThrow(want)
  })

  it.each([[{ pageSize: 0 }], [{ pageSize: -1 }], [{ pageSize: 1.5 }], [{ pageSize: '10' }]])(
    'rejects invalid pageSize %j',
    (input) => {
      expect(() => validateListFilesInput(input)).toThrow(/pageSize/)
    },
  )

  it('rejects a non-string pageToken', () => {
    expect(() => validateListFilesInput({ pageToken: 42 })).toThrow(/pageToken/)
  })
})

describe('validateIdInput', () => {
  it('accepts a numeric string id', () => {
    expect(validateIdInput({ id: '123' })).toEqual({ id: '123' })
  })

  it.each([[{}], [{ id: 123 }], [{ id: 'abc' }], [{ id: '' }], [{ id: '12x' }]])(
    'rejects %j',
    (input) => {
      expect(() => validateIdInput(input)).toThrow(/id/)
    },
  )
})

describe('listFilesImpl', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('passes pagination params through and maps the response', async () => {
    const listFiles = vi.fn().mockResolvedValue(
      create(ListFilesResponseSchema, {
        files: [fileInfo({ id: 1n }), fileInfo({ id: 2n, key: 'b.txt' })],
        nextPageToken: 'token-2',
      }),
    )
    const search = { listFiles } as unknown as Client<typeof SearchService>

    const result = await listFilesImpl(search, noPreviewFilesClient(), {
      pageSize: 25,
      pageToken: 'token-1',
    })

    expect(listFiles).toHaveBeenCalledWith({
      pageSize: 25,
      pageToken: 'token-1',
      prefix: '',
      contentType: '',
      sortField: SortField.KEY,
      sortOrder: SortOrder.ASC,
    })
    expect(result.files.map((f) => f.id)).toEqual(['1', '2'])
    expect(result.nextPageToken).toBe('token-2')
  })

  it('maps filter and sort params onto the request', async () => {
    const listFiles = vi
      .fn()
      .mockResolvedValue(create(ListFilesResponseSchema, { files: [], nextPageToken: '' }))
    const search = { listFiles } as unknown as Client<typeof SearchService>

    await listFilesImpl(search, noPreviewFilesClient(), {
      prefix: 'docs/',
      contentType: 'image/',
      sortField: 'size',
      sortOrder: 'desc',
    })

    expect(listFiles).toHaveBeenCalledWith({
      pageSize: 0,
      pageToken: '',
      prefix: 'docs/',
      contentType: 'image/',
      sortField: SortField.SIZE,
      sortOrder: SortOrder.DESC,
    })
  })

  it('defaults pageSize/pageToken and reports the end of pagination', async () => {
    const listFiles = vi
      .fn()
      .mockResolvedValue(create(ListFilesResponseSchema, { files: [], nextPageToken: '' }))
    const search = { listFiles } as unknown as Client<typeof SearchService>

    const result = await listFilesImpl(search, noPreviewFilesClient(), {})

    expect(listFiles).toHaveBeenCalledWith({
      pageSize: 0,
      pageToken: '',
      prefix: '',
      contentType: '',
      sortField: SortField.KEY,
      sortOrder: SortOrder.ASC,
    })
    expect(result).toEqual({ files: [], nextPageToken: '' })
  })

  it('propagates client errors', async () => {
    const search = {
      listFiles: vi.fn().mockRejectedValue(new Error('unavailable')),
    } as unknown as Client<typeof SearchService>
    await expect(listFilesImpl(search, noPreviewFilesClient(), {})).rejects.toThrow('unavailable')
  })

  it('merges preview URLs onto matching dtos, requesting only ids with a preview', async () => {
    const listFiles = vi.fn().mockResolvedValue(
      create(ListFilesResponseSchema, {
        files: [
          fileInfo({
            id: 1n,
            previewKey: '.index/previews/1',
            previewWidth: 320,
            previewHeight: 160,
          }),
          fileInfo({ id: 2n, key: 'no-preview.txt' }),
        ],
        nextPageToken: '',
      }),
    )
    const search = { listFiles } as unknown as Client<typeof SearchService>

    const getPreviewURL = vi.fn().mockResolvedValue(
      create(GetPreviewURLResponseSchema, {
        previewUrls: [
          create(PreviewURLSpecSchema, { id: 1n, url: 'https://example.com/preview-1' }),
        ],
      }),
    )
    const files = { getPreviewURL } as unknown as Client<typeof FilesService>

    const result = await listFilesImpl(search, files, {})

    expect(getPreviewURL).toHaveBeenCalledWith({ ids: [1n] })
    const byId = new Map(result.files.map((f) => [f.id, f]))
    expect(byId.get('1')?.previewUrl).toBe('https://example.com/preview-1')
    expect(byId.get('2')?.previewUrl).toBeNull()
  })

  it('does not call getPreviewURL when no file has a preview', async () => {
    const listFiles = vi.fn().mockResolvedValue(
      create(ListFilesResponseSchema, {
        files: [fileInfo({ id: 1n })],
        nextPageToken: '',
      }),
    )
    const search = { listFiles } as unknown as Client<typeof SearchService>
    const files = noPreviewFilesClient()

    await listFilesImpl(search, files, {})

    expect(files.getPreviewURL).not.toHaveBeenCalled()
  })

  it('degrades to previewUrl: null when the preview lookup fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})

    const listFiles = vi.fn().mockResolvedValue(
      create(ListFilesResponseSchema, {
        files: [fileInfo({ id: 1n, previewKey: '.index/previews/1' })],
        nextPageToken: '',
      }),
    )
    const search = { listFiles } as unknown as Client<typeof SearchService>
    const files = {
      getPreviewURL: vi.fn().mockRejectedValue(new Error('files service unavailable')),
    } as unknown as Client<typeof FilesService>

    const result = await listFilesImpl(search, files, {})

    expect(result.files[0].previewUrl).toBeNull()
  })
})

describe('getDownloadUrlImpl', () => {
  it('requests the id and returns its presigned URL', async () => {
    const getDownloadURL = vi.fn().mockResolvedValue(
      create(GetDownloadURLResponseSchema, {
        downloadUrls: [create(DownloadURLSpecSchema, { id: 42n, url: 'http://s3/file42' })],
      }),
    )
    const client = { getDownloadURL } as unknown as Client<typeof FilesService>

    await expect(getDownloadUrlImpl(client, '42')).resolves.toEqual({ url: 'http://s3/file42' })
    expect(getDownloadURL).toHaveBeenCalledWith({ ids: [42n] })
  })

  it('throws when the response has no URL for the id', async () => {
    const client = {
      getDownloadURL: vi
        .fn()
        .mockResolvedValue(create(GetDownloadURLResponseSchema, { downloadUrls: [] })),
    } as unknown as Client<typeof FilesService>

    await expect(getDownloadUrlImpl(client, '42')).rejects.toThrow('no download URL')
  })
})

describe('deleteFileImpl', () => {
  it('sends the id as int64', async () => {
    const deleteFile = vi.fn().mockResolvedValue(create(DeleteFileResponseSchema))
    const client = { deleteFile } as unknown as Client<typeof FilesService>

    await deleteFileImpl(client, '7')

    expect(deleteFile).toHaveBeenCalledWith({ id: 7n })
  })
})

const TAKEN_AT = new Date('2025-06-01T12:00:00.000Z')

describe('toFileMetadataDto', () => {
  it('maps stat/preview fields, with exif null when absent', () => {
    const dto = toFileMetadataDto(fileInfo({ id: 5n }))
    expect(dto).toEqual({
      id: '5',
      key: 'docs/report.pdf',
      contentType: 'application/pdf',
      sizeBytes: 1024,
      createdAt: '2026-01-02T03:04:05.000Z',
      updatedAt: null,
      exif: null,
    })
  })

  it('maps exif fields when present', () => {
    const file = create(FileInfoSchema, {
      id: 1n,
      key: 'photo.jpg',
      contentType: 'image/jpeg',
      exif: create(ExifMetadataSchema, {
        cameraMake: 'Canon',
        cameraModel: 'EOS R5',
        iso: 400,
        takenAt: timestampFromDate(TAKEN_AT),
        gpsLatitude: 51.5,
        gpsLongitude: -0.1,
        xmpKeywords: ['vacation', 'beach'],
        hasExif: true,
        hasXmp: false,
      }),
    })

    const dto = toFileMetadataDto(file)

    expect(dto.exif).toEqual(
      expect.objectContaining({
        cameraMake: 'Canon',
        cameraModel: 'EOS R5',
        iso: 400,
        takenAt: '2025-06-01T12:00:00.000Z',
        gpsLatitude: 51.5,
        gpsLongitude: -0.1,
        xmpKeywords: ['vacation', 'beach'],
        hasExif: true,
        hasXmp: false,
      }),
    )
  })
})

describe('validateKeyInput', () => {
  it('accepts a non-empty key', () => {
    expect(validateKeyInput({ key: 'docs/report.pdf' })).toEqual({ key: 'docs/report.pdf' })
  })

  it.each([[{}], [{ key: '' }], [{ key: 42 }]])('rejects %j', (input) => {
    expect(() => validateKeyInput(input)).toThrow(/key/)
  })
})

describe('getUploadUrlImpl', () => {
  it('requests the key and returns the presigned URL', async () => {
    const getUploadURL = vi
      .fn()
      .mockResolvedValue(create(GetUploadURLResponseSchema, { url: 'https://s3/put-url' }))
    const client = { getUploadURL } as unknown as Client<typeof FilesService>

    await expect(getUploadUrlImpl(client, 'docs/report.pdf')).resolves.toEqual({
      url: 'https://s3/put-url',
    })
    expect(getUploadURL).toHaveBeenCalledWith({ key: 'docs/report.pdf' })
  })
})

describe('commitUploadImpl', () => {
  it('sends the key and maps the returned file', async () => {
    const commitUpload = vi.fn().mockResolvedValue(
      create(CommitUploadResponseSchema, {
        file: fileInfo({ id: 9n, key: 'docs/report.pdf' }),
      }),
    )
    const client = { commitUpload } as unknown as Client<typeof FilesService>

    const result = await commitUploadImpl(client, 'docs/report.pdf')

    expect(commitUpload).toHaveBeenCalledWith({ key: 'docs/report.pdf' })
    expect(result.id).toBe('9')
    expect(result.key).toBe('docs/report.pdf')
  })

  it('throws when the response has no file', async () => {
    const client = {
      commitUpload: vi.fn().mockResolvedValue(create(CommitUploadResponseSchema, {})),
    } as unknown as Client<typeof FilesService>

    await expect(commitUploadImpl(client, 'docs/report.pdf')).rejects.toThrow('no file returned')
  })
})

describe('validateListDirectoryInput', () => {
  it('accepts empty input', () => {
    expect(validateListDirectoryInput(undefined)).toEqual({})
    expect(validateListDirectoryInput({})).toEqual({})
  })

  it('accepts path, pagination, and sort fields', () => {
    expect(
      validateListDirectoryInput({
        path: 'docs/',
        pageSize: 25,
        pageToken: 'abc',
        sortField: 'size',
        sortOrder: 'desc',
      }),
    ).toEqual({
      path: 'docs/',
      pageSize: 25,
      pageToken: 'abc',
      sortField: 'size',
      sortOrder: 'desc',
    })
  })

  it.each([
    [{ path: 42 }, /path/],
    [{ sortField: 'bogus' }, /sortField/],
    [{ sortOrder: 'up' }, /sortOrder/],
  ] as const)('rejects invalid input %j', (input, want) => {
    expect(() => validateListDirectoryInput(input)).toThrow(want)
  })

  it.each([[{ pageSize: 0 }], [{ pageSize: -1 }], [{ pageSize: 1.5 }]])(
    'rejects invalid pageSize %j',
    (input) => {
      expect(() => validateListDirectoryInput(input)).toThrow(/pageSize/)
    },
  )
})

describe('listDirectoryImpl', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('passes path/pagination/sort params through and maps the response', async () => {
    const listDirectory = vi.fn().mockResolvedValue(
      create(ListDirectoryResponseSchema, {
        directories: ['docs/sub/', 'docs/sub2/'],
        files: [fileInfo({ id: 1n, key: 'docs/a.txt' })],
        nextPageToken: 'token-2',
      }),
    )
    const search = { listDirectory } as unknown as Client<typeof SearchService>

    const result = await listDirectoryImpl(search, noPreviewFilesClient(), {
      path: 'docs/',
      pageSize: 25,
      pageToken: 'token-1',
      sortField: 'size',
      sortOrder: 'desc',
    })

    expect(listDirectory).toHaveBeenCalledWith({
      path: 'docs/',
      pageSize: 25,
      pageToken: 'token-1',
      sortField: SortField.SIZE,
      sortOrder: SortOrder.DESC,
    })
    expect(result.directories).toEqual(['docs/sub/', 'docs/sub2/'])
    expect(result.files.map((f) => f.id)).toEqual(['1'])
    expect(result.nextPageToken).toBe('token-2')
  })

  it('defaults path/pageSize/pageToken/sort', async () => {
    const listDirectory = vi.fn().mockResolvedValue(
      create(ListDirectoryResponseSchema, {
        directories: [],
        files: [],
        nextPageToken: '',
      }),
    )
    const search = { listDirectory } as unknown as Client<typeof SearchService>

    await listDirectoryImpl(search, noPreviewFilesClient(), {})

    expect(listDirectory).toHaveBeenCalledWith({
      path: '',
      pageSize: 0,
      pageToken: '',
      sortField: SortField.KEY,
      sortOrder: SortOrder.ASC,
    })
  })

  it('resolves preview URLs for files that have one', async () => {
    const listDirectory = vi.fn().mockResolvedValue(
      create(ListDirectoryResponseSchema, {
        directories: [],
        files: [fileInfo({ id: 1n, previewKey: '.index/previews/1' })],
        nextPageToken: '',
      }),
    )
    const search = { listDirectory } as unknown as Client<typeof SearchService>

    const getPreviewURL = vi.fn().mockResolvedValue(
      create(GetPreviewURLResponseSchema, {
        previewUrls: [
          create(PreviewURLSpecSchema, { id: 1n, url: 'https://example.com/preview-1' }),
        ],
      }),
    )
    const files = { getPreviewURL } as unknown as Client<typeof FilesService>

    const result = await listDirectoryImpl(search, files, {})

    expect(getPreviewURL).toHaveBeenCalledWith({ ids: [1n] })
    expect(result.files[0].previewUrl).toBe('https://example.com/preview-1')
  })

  it('propagates client errors', async () => {
    const search = {
      listDirectory: vi.fn().mockRejectedValue(new Error('unavailable')),
    } as unknown as Client<typeof SearchService>
    await expect(listDirectoryImpl(search, noPreviewFilesClient(), {})).rejects.toThrow(
      'unavailable',
    )
  })
})

describe('validatePathInput', () => {
  it('accepts a non-empty path', () => {
    expect(validatePathInput({ path: 'docs/' })).toEqual({ path: 'docs/' })
  })

  it.each([[{}], [{ path: '' }], [{ path: 42 }]])('rejects %j', (input) => {
    expect(() => validatePathInput(input)).toThrow(/path/)
  })
})

describe('getDirectoryStatsImpl', () => {
  it('requests the path and maps bigint counts to numbers', async () => {
    const getDirectoryStats = vi.fn().mockResolvedValue(
      create(GetDirectoryStatsResponseSchema, {
        fileCount: 3n,
        totalBytes: 1024n,
      }),
    )
    const client = { getDirectoryStats } as unknown as Client<typeof FilesService>

    const result = await getDirectoryStatsImpl(client, 'docs/')

    expect(getDirectoryStats).toHaveBeenCalledWith({ path: 'docs/' })
    expect(result).toEqual({ fileCount: 3, totalBytes: 1024 })
  })
})

describe('deleteDirectoryImpl', () => {
  it('requests the path and maps the deleted count', async () => {
    const deleteDirectory = vi
      .fn()
      .mockResolvedValue(create(DeleteDirectoryResponseSchema, { deletedCount: 5n }))
    const client = { deleteDirectory } as unknown as Client<typeof FilesService>

    const result = await deleteDirectoryImpl(client, 'docs/')

    expect(deleteDirectory).toHaveBeenCalledWith({ path: 'docs/' })
    expect(result).toEqual({ deletedCount: 5 })
  })
})

describe('getFileMetadataImpl', () => {
  it('requests the id and maps the response', async () => {
    const getFileInfo = vi.fn().mockResolvedValue(
      create(GetFileInfoResponseSchema, {
        file: fileInfo({ id: 42n }),
      }),
    )
    const client = { getFileInfo } as unknown as Client<typeof FilesService>

    const result = await getFileMetadataImpl(client, '42')

    expect(getFileInfo).toHaveBeenCalledWith({ id: 42n })
    expect(result.id).toBe('42')
    expect(result.exif).toBeNull()
  })

  it('throws when the response has no file', async () => {
    const client = {
      getFileInfo: vi.fn().mockResolvedValue(create(GetFileInfoResponseSchema, {})),
    } as unknown as Client<typeof FilesService>

    await expect(getFileMetadataImpl(client, '42')).rejects.toThrow('no file returned')
  })
})
