import { create } from '@bufbuild/protobuf'
import { timestampFromDate } from '@bufbuild/protobuf/wkt'
import type { Client } from '@connectrpc/connect'
import { describe, expect, it, vi } from 'vitest'
import {
  DeleteFileResponseSchema,
  DownloadURLSpecSchema,
  FileInfoSchema,
  type FilesService,
  GetDownloadURLResponseSchema,
} from '../gen/service/v1/files_pb'
import {
  ListFilesResponseSchema,
  type SearchService,
  SortField,
  SortOrder,
} from '../gen/service/v1/search_pb'
import {
  deleteFileImpl,
  getDownloadUrlImpl,
  listFilesImpl,
  toFileDto,
  validateIdInput,
  validateListFilesInput,
} from './impl'

const CREATED_AT = new Date('2026-01-02T03:04:05.000Z')

function fileInfo(overrides: { id?: bigint; key?: string } = {}) {
  return create(FileInfoSchema, {
    id: overrides.id ?? 1n,
    key: overrides.key ?? 'docs/report.pdf',
    contentType: 'application/pdf',
    sizeBytes: 1024n,
    createdAt: timestampFromDate(CREATED_AT),
  })
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
    })
  })

  it('maps a missing created_at to null', () => {
    const file = create(FileInfoSchema, { id: 1n, key: 'a' })
    expect(toFileDto(file).createdAt).toBeNull()
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

  it.each([
    [{ pageSize: 0 }],
    [{ pageSize: -1 }],
    [{ pageSize: 1.5 }],
    [{ pageSize: '10' }],
  ])('rejects invalid pageSize %j', (input) => {
    expect(() => validateListFilesInput(input)).toThrow(/pageSize/)
  })

  it('rejects a non-string pageToken', () => {
    expect(() => validateListFilesInput({ pageToken: 42 })).toThrow(/pageToken/)
  })
})

describe('validateIdInput', () => {
  it('accepts a numeric string id', () => {
    expect(validateIdInput({ id: '123' })).toEqual({ id: '123' })
  })

  it.each([
    [{}],
    [{ id: 123 }],
    [{ id: 'abc' }],
    [{ id: '' }],
    [{ id: '12x' }],
  ])('rejects %j', (input) => {
    expect(() => validateIdInput(input)).toThrow(/id/)
  })
})

describe('listFilesImpl', () => {
  it('passes pagination params through and maps the response', async () => {
    const listFiles = vi.fn().mockResolvedValue(
      create(ListFilesResponseSchema, {
        files: [fileInfo({ id: 1n }), fileInfo({ id: 2n, key: 'b.txt' })],
        nextPageToken: 'token-2',
      }),
    )
    const client = { listFiles } as unknown as Client<typeof SearchService>

    const result = await listFilesImpl(client, { pageSize: 25, pageToken: 'token-1' })

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
    const client = { listFiles } as unknown as Client<typeof SearchService>

    await listFilesImpl(client, {
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
    const client = { listFiles } as unknown as Client<typeof SearchService>

    const result = await listFilesImpl(client, {})

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
    const client = {
      listFiles: vi.fn().mockRejectedValue(new Error('unavailable')),
    } as unknown as Client<typeof SearchService>
    await expect(listFilesImpl(client, {})).rejects.toThrow('unavailable')
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
