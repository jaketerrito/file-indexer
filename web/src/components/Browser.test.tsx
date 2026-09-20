import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { type BrowseFilters, DEFAULT_BROWSE_FILTERS } from '../lib/fileFilters'
import { Browser } from './Browser'

vi.mock('../server/files', () => ({
  listDirectory: vi.fn(),
  getDirectoryStats: vi.fn(),
  deleteDirectory: vi.fn(),
  deleteFile: vi.fn(),
  getDownloadUrl: vi.fn(),
  getUploadUrl: vi.fn(),
  commitUpload: vi.fn(),
  createMultipartUpload: vi.fn(),
  getUploadPartUrl: vi.fn(),
  listUploadedParts: vi.fn(),
  completeMultipartUpload: vi.fn(),
  abortMultipartUpload: vi.fn(),
}))

import {
  abortMultipartUpload,
  commitUpload,
  completeMultipartUpload,
  createMultipartUpload,
  getUploadPartUrl,
  getUploadUrl,
  listDirectory,
} from '../server/files'

const listDirectoryMock = vi.mocked(listDirectory)
const getUploadUrlMock = vi.mocked(getUploadUrl)
const commitUploadMock = vi.mocked(commitUpload)
const createMultipartUploadMock = vi.mocked(createMultipartUpload)
const getUploadPartUrlMock = vi.mocked(getUploadPartUrl)
const completeMultipartUploadMock = vi.mocked(completeMultipartUpload)
const abortMultipartUploadMock = vi.mocked(abortMultipartUpload)

class FakeIntersectionObserver implements IntersectionObserver {
  readonly root = null
  readonly rootMargin = ''
  readonly scrollMargin = ''
  readonly thresholds = []
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords(): IntersectionObserverEntry[] {
    return []
  }
}

function Harness({ initial }: { initial: BrowseFilters }) {
  const [filters, setFilters] = useState(initial)
  return (
    <>
      <div data-testid="filters">{JSON.stringify(filters)}</div>
      <Browser filters={filters} onFiltersChange={setFilters} onOpenFile={() => {}} />
    </>
  )
}

function renderBrowser(initial: BrowseFilters = DEFAULT_BROWSE_FILTERS) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <Harness initial={initial} />
    </QueryClientProvider>,
  )
}

function currentFilters(): BrowseFilters {
  return JSON.parse(screen.getByTestId('filters').textContent as string)
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
  listDirectoryMock.mockResolvedValue({ directories: [], files: [], nextPageToken: '' })
  localStorage.clear()
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('Browser', () => {
  it('renders breadcrumbs and directory contents in browse mode', async () => {
    listDirectoryMock.mockResolvedValue({
      directories: ['docs/sub/'],
      files: [],
      nextPageToken: '',
    })
    renderBrowser({ ...DEFAULT_BROWSE_FILTERS, path: 'docs/' })

    expect(await screen.findByRole('button', { name: 'sub' })).toBeDefined()
    // "docs" is the current (last) breadcrumb segment, not a link.
    expect(screen.getByText('docs').getAttribute('aria-current')).toBe('page')
    expect(listDirectoryMock).toHaveBeenCalledWith(
      expect.objectContaining({ data: expect.objectContaining({ path: 'docs/' }) }),
    )
  })

  it('navigating a breadcrumb updates the path filter', async () => {
    listDirectoryMock.mockResolvedValue({
      directories: [],
      files: [],
      nextPageToken: '',
    })
    renderBrowser({ ...DEFAULT_BROWSE_FILTERS, path: 'docs/sub/' })

    fireEvent.click(await screen.findByRole('button', { name: 'Home' }))
    await waitFor(() => expect(currentFilters().path).toBe(''))
  })

  it('creates a new folder by navigating to it, without a backend call', async () => {
    vi.stubGlobal(
      'prompt',
      vi.fn(() => 'photos'),
    )
    renderBrowser({ ...DEFAULT_BROWSE_FILTERS, path: 'docs/' })
    await screen.findByRole('button', { name: 'New folder' })

    fireEvent.click(screen.getByRole('button', { name: 'New folder' }))

    await waitFor(() => expect(currentFilters().path).toBe('docs/photos/'))
  })

  it('rejects a folder name containing "/"', async () => {
    const alertSpy = vi.fn()
    vi.stubGlobal(
      'prompt',
      vi.fn(() => 'a/b'),
    )
    vi.stubGlobal('alert', alertSpy)
    renderBrowser({ ...DEFAULT_BROWSE_FILTERS, path: 'docs/' })
    await screen.findByRole('button', { name: 'New folder' })

    fireEvent.click(screen.getByRole('button', { name: 'New folder' }))

    expect(alertSpy).toHaveBeenCalled()
    expect(currentFilters().path).toBe('docs/')
  })

  it('uploads to the current folder by default while browsing', async () => {
    getUploadUrlMock.mockResolvedValue({ url: 'https://s3/put-url' })
    commitUploadMock.mockResolvedValue({
      id: '1',
      key: 'docs/new.txt',
      contentType: '',
      sizeBytes: 3,
      createdAt: null,
      previewUrl: null,
      previewWidth: null,
      previewHeight: null,
      previewStatus: PreviewStatus.PENDING,
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))

    renderBrowser({ ...DEFAULT_BROWSE_FILTERS, path: 'docs/' })
    await screen.findByRole('button', { name: 'Upload' })

    const file = new File(['abc'], 'new.txt', { type: 'text/plain' })
    fireEvent.change(screen.getByLabelText('Upload files'), { target: { files: [file] } })

    await waitFor(() =>
      expect(getUploadUrlMock).toHaveBeenCalledWith({ data: { key: 'docs/new.txt' } }),
    )
  })

  it('uploads into the new folder after navigating there, with no destination override field', async () => {
    getUploadUrlMock.mockResolvedValue({ url: 'https://s3/put-url' })
    commitUploadMock.mockResolvedValue({
      id: '1',
      key: 'docs/sub/new.txt',
      contentType: '',
      sizeBytes: 3,
      createdAt: null,
      previewUrl: null,
      previewWidth: null,
      previewHeight: null,
      previewStatus: PreviewStatus.PENDING,
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))

    listDirectoryMock.mockResolvedValue({
      directories: ['docs/sub/'],
      files: [],
      nextPageToken: '',
    })
    renderBrowser({ ...DEFAULT_BROWSE_FILTERS, path: 'docs/' })
    fireEvent.click(await screen.findByRole('button', { name: 'sub' }))
    await waitFor(() => expect(currentFilters().path).toBe('docs/sub/'))

    // No freeform "Upload to" override — navigating is the only way to
    // change the destination, and it's already been done above.
    expect(screen.queryByLabelText('Upload to')).toBeNull()

    const file = new File(['abc'], 'new.txt', { type: 'text/plain' })
    fireEvent.change(screen.getByLabelText('Upload files'), { target: { files: [file] } })

    await waitFor(() =>
      expect(getUploadUrlMock).toHaveBeenCalledWith({ data: { key: 'docs/sub/new.txt' } }),
    )
  })

  it(
    'keeps the upload input clickable in every browser (not display:none, which WebKit ' +
      'silently refuses to open via a programmatic click)',
    async () => {
      renderBrowser({ ...DEFAULT_BROWSE_FILTERS, path: 'docs/' })
      const input = (await screen.findByLabelText('Upload files')) as HTMLInputElement

      expect(input.style.display).not.toBe('none')
      expect(input.style.position).toBe('absolute')
    },
  )

  it('refetches sort/order for the directory listing', async () => {
    renderBrowser({ ...DEFAULT_BROWSE_FILTERS, path: 'docs/' })
    await screen.findByRole('button', { name: 'New folder' })

    fireEvent.change(screen.getByLabelText('Sort by'), { target: { value: 'size' } })
    await waitFor(() =>
      expect(listDirectoryMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ data: expect.objectContaining({ sortField: 'size' }) }),
      ),
    )
  })

  it('uploads large files via the resumable multipart path instead of a single PUT', async () => {
    createMultipartUploadMock.mockResolvedValue({ uploadId: 'upload-1' })
    getUploadPartUrlMock.mockResolvedValue({ url: 'https://s3/part-url' })
    completeMultipartUploadMock.mockResolvedValue({
      id: '1',
      key: 'docs/big.bin',
      contentType: '',
      sizeBytes: 40 << 20,
      createdAt: null,
      previewUrl: null,
      previewWidth: null,
      previewHeight: null,
      previewStatus: PreviewStatus.PENDING,
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))

    renderBrowser({ ...DEFAULT_BROWSE_FILTERS, path: 'docs/' })
    await screen.findByRole('button', { name: 'Upload' })

    const bigFile = new File([new ArrayBuffer(40 << 20)], 'big.bin', {
      type: 'application/octet-stream',
    })
    fireEvent.change(screen.getByLabelText('Upload files'), { target: { files: [bigFile] } })

    await waitFor(() =>
      expect(createMultipartUploadMock).toHaveBeenCalledWith({
        data: { key: 'docs/big.bin', contentType: 'application/octet-stream' },
      }),
    )
    await waitFor(() =>
      expect(completeMultipartUploadMock).toHaveBeenCalledWith({
        data: { key: 'docs/big.bin', uploadId: 'upload-1' },
      }),
    )
    expect(getUploadUrlMock).not.toHaveBeenCalled()
  })

  it('cancels an in-progress multipart upload and aborts it server-side', async () => {
    createMultipartUploadMock.mockResolvedValue({ uploadId: 'upload-1' })
    getUploadPartUrlMock.mockResolvedValue({ url: 'https://s3/part-url' })
    // Simulates a real fetch: the PUT never resolves on its own, but rejects
    // as soon as its AbortSignal fires — exactly what the Cancel button
    // needs to interrupt a part transfer already in flight.
    vi.stubGlobal(
      'fetch',
      vi.fn((_url: string, init?: RequestInit) => {
        return new Promise((_resolve, reject) => {
          init?.signal?.addEventListener('abort', () => reject(new Error('aborted')))
        })
      }),
    )

    renderBrowser({ ...DEFAULT_BROWSE_FILTERS, path: 'docs/' })
    await screen.findByRole('button', { name: 'Upload' })

    const bigFile = new File([new ArrayBuffer(40 << 20)], 'big.bin', {
      type: 'application/octet-stream',
    })
    fireEvent.change(screen.getByLabelText('Upload files'), { target: { files: [bigFile] } })

    fireEvent.click(await screen.findByRole('button', { name: 'Cancel' }))

    await waitFor(() => expect(screen.getByRole('alert').textContent).toMatch(/cancelled/))
    expect(abortMultipartUploadMock).toHaveBeenCalledWith({
      data: { key: 'docs/big.bin', uploadId: 'upload-1' },
    })
  })
})
