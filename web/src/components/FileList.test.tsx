import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { DEFAULT_FILTERS, type FileFilters } from '../lib/fileFilters'
import type { ListFilesResult } from '../server/impl'
import { FileList } from './FileList'

// The server functions module is mocked wholesale: on the client these are
// plain async functions, and unit tests must not touch the network.
vi.mock('../server/files', () => ({
  listFiles: vi.fn(),
  listContentTypes: vi.fn(),
  getDownloadUrl: vi.fn(),
  deleteFile: vi.fn(),
  getFilePreviewStatuses: vi.fn(),
}))

import { deleteFile, getDownloadUrl, listContentTypes, listFiles } from '../server/files'

const listFilesMock = vi.mocked(listFiles)
const listContentTypesMock = vi.mocked(listContentTypes)
const getDownloadUrlMock = vi.mocked(getDownloadUrl)
const deleteFileMock = vi.mocked(deleteFile)

function page(keys: string[], startId: number, nextPageToken = ''): ListFilesResult {
  return {
    files: keys.map((key, i) => ({
      id: String(startId + i),
      key,
      contentType: 'text/plain',
      sizeBytes: 1,
      createdAt: null,
      previewUrl: null,
      previewWidth: null,
      previewHeight: null,
      previewStatus: PreviewStatus.NONE,
    })),
    nextPageToken,
  }
}

/** Expected listFiles call payload for the given overrides. */
function listArgs(overrides: Record<string, unknown> = {}) {
  return {
    data: {
      pageSize: 50,
      pageToken: '',
      prefix: '',
      contentType: '',
      sortField: 'key',
      sortOrder: 'asc',
      ...overrides,
    },
  }
}

// jsdom has no IntersectionObserver; capture the callback so tests can
// simulate the sentinel scrolling into view.
let intersectionCallback: IntersectionObserverCallback | undefined

class FakeIntersectionObserver implements IntersectionObserver {
  readonly root = null
  readonly rootMargin = ''
  readonly scrollMargin = ''
  readonly thresholds = []
  constructor(callback: IntersectionObserverCallback) {
    intersectionCallback = callback
  }
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords(): IntersectionObserverEntry[] {
    return []
  }
}

function triggerIntersection() {
  intersectionCallback?.(
    [{ isIntersecting: true } as IntersectionObserverEntry],
    new FakeIntersectionObserver(() => {}),
  )
}

// Stateful harness standing in for the URL-backed filter state owned by the
// index route: onFiltersChange feeds back into the filters prop.
function Harness({
  initial = DEFAULT_FILTERS,
  onOpenFile,
}: {
  initial?: FileFilters
  onOpenFile: (id: string) => void
}) {
  const [filters, setFilters] = useState(initial)
  return <FileList filters={filters} onFiltersChange={setFilters} onOpenFile={onOpenFile} />
}

function renderFileList(initial?: FileFilters, onOpenFile: (id: string) => void = () => {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <Harness initial={initial} onOpenFile={onOpenFile} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
  // Default category list; tests override per scenario. The existing
  // type-filter test fires a change to 'image/', which must be a rendered
  // <option> for a controlled select to hold the value.
  listContentTypesMock.mockResolvedValue({ categories: ['image/', 'text/'] })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
  intersectionCallback = undefined
})

describe('FileList', () => {
  it('renders the file keys with download and delete buttons', async () => {
    listFilesMock.mockResolvedValue(page(['a.txt', 'b.txt'], 1))

    renderFileList()

    expect(await screen.findByText('a.txt')).toBeDefined()
    expect(screen.getByText('b.txt')).toBeDefined()
    expect(screen.getAllByRole('button', { name: 'Download' })).toHaveLength(2)
    expect(screen.getAllByRole('button', { name: 'Delete' })).toHaveLength(2)
    expect(listFilesMock).toHaveBeenCalledWith(listArgs())
  })

  it('shows an empty state', async () => {
    listFilesMock.mockResolvedValue(page([], 0))
    renderFileList()
    expect(await screen.findByText('No files.')).toBeDefined()
  })

  it('shows a filtered empty state when filters are active', async () => {
    listFilesMock.mockResolvedValue(page([], 0))
    renderFileList({ ...DEFAULT_FILTERS, prefix: 'zzz' })
    expect(await screen.findByText('No files match your filters.')).toBeDefined()
  })

  it('shows an error state when loading fails', async () => {
    listFilesMock.mockRejectedValue(new Error('boom'))
    renderFileList()
    expect(await screen.findByRole('alert')).toBeDefined()
  })

  it('fetches the next page with the cursor when the sentinel intersects', async () => {
    listFilesMock
      .mockResolvedValueOnce(page(['a.txt'], 1, 'cursor-1'))
      .mockResolvedValueOnce(page(['b.txt'], 2))

    renderFileList()
    await screen.findByText('a.txt')

    triggerIntersection()

    expect(await screen.findByText('b.txt')).toBeDefined()
    expect(listFilesMock).toHaveBeenLastCalledWith(listArgs({ pageToken: 'cursor-1' }))
    // Both pages stay rendered.
    expect(screen.getByText('a.txt')).toBeDefined()
  })

  it('does not fetch more pages once the cursor is exhausted', async () => {
    listFilesMock.mockResolvedValue(page(['a.txt'], 1, ''))

    renderFileList()
    await screen.findByText('a.txt')

    triggerIntersection()

    await waitFor(() => expect(listFilesMock).toHaveBeenCalledTimes(1))
  })

  it('debounces the prefix search into a fresh query', async () => {
    listFilesMock.mockResolvedValue(page(['docs/a.txt'], 1))

    renderFileList()
    await screen.findByText('docs/a.txt')

    fireEvent.change(screen.getByLabelText(/Search/), { target: { value: 'docs/' } })

    // Not refetched synchronously: the input is debounced.
    expect(listFilesMock).toHaveBeenCalledTimes(1)

    await waitFor(() =>
      expect(listFilesMock).toHaveBeenCalledWith(listArgs({ prefix: 'docs/', pageToken: '' })),
    )
  })

  it('refetches with the mapped content type when the type filter changes', async () => {
    listFilesMock.mockResolvedValue(page(['a.png'], 1))

    renderFileList()
    await screen.findByText('a.png')

    fireEvent.change(screen.getByLabelText(/Type/), { target: { value: 'image/' } })

    await waitFor(() =>
      expect(listFilesMock).toHaveBeenCalledWith(listArgs({ contentType: 'image/' })),
    )
  })

  it('populates the type dropdown from the backend category list', async () => {
    listFilesMock.mockResolvedValue(page(['a.png'], 1))
    listContentTypesMock.mockResolvedValue({ categories: ['image/', 'text/'] })

    renderFileList()
    await screen.findByText('a.png')

    const select = screen.getByLabelText(/Type/)
    await waitFor(() =>
      expect(
        within(select)
          .getAllByRole('option')
          .map((o) => o.textContent),
      ).toEqual(['All types', 'Image', 'Text']),
    )
  })

  it('keeps an active type filter selectable when its category is absent', async () => {
    listFilesMock.mockResolvedValue(page([], 0))
    listContentTypesMock.mockResolvedValue({ categories: ['text/'] })

    renderFileList({ ...DEFAULT_FILTERS, type: 'image/' })
    await screen.findByText('No files match your filters.')

    expect((screen.getByLabelText(/Type/) as HTMLSelectElement).value).toBe('image/')
  })

  it('refetches when sort field and order change', async () => {
    listFilesMock.mockResolvedValue(page(['a.txt'], 1))

    renderFileList()
    await screen.findByText('a.txt')

    fireEvent.change(screen.getByLabelText(/Sort by/), { target: { value: 'size' } })
    await waitFor(() => expect(listFilesMock).toHaveBeenCalledWith(listArgs({ sortField: 'size' })))

    fireEvent.click(screen.getByRole('button', { name: 'Ascending' }))
    await waitFor(() =>
      expect(listFilesMock).toHaveBeenCalledWith(
        listArgs({ sortField: 'size', sortOrder: 'desc' }),
      ),
    )
    expect(screen.getByRole('button', { name: 'Descending' })).toBeDefined()
  })

  it('opens the presigned URL when Download is clicked', async () => {
    listFilesMock.mockResolvedValue(page(['a.txt'], 7))
    getDownloadUrlMock.mockResolvedValue({ url: 'http://s3/presigned' })
    const openSpy = vi.fn()
    vi.stubGlobal('open', openSpy)

    renderFileList()
    const button = await screen.findByRole('button', { name: 'Download' })
    button.click()

    await waitFor(() => {
      expect(getDownloadUrlMock).toHaveBeenCalledWith({ data: { id: '7' } })
      expect(openSpy).toHaveBeenCalledWith('http://s3/presigned', '_blank', 'noopener')
    })
  })

  it('renders a preview image for files that have one', async () => {
    listFilesMock.mockResolvedValue({
      files: [
        {
          id: '1',
          key: 'photo.jpg',
          contentType: 'image/jpeg',
          sizeBytes: 1,
          createdAt: null,
          previewUrl: 'https://example.com/preview-1',
          previewWidth: 320,
          previewHeight: 160,
          previewStatus: PreviewStatus.READY,
        },
      ],
      nextPageToken: '',
    })

    renderFileList()
    await screen.findByText('photo.jpg')

    const img = screen.getByRole('presentation') as HTMLImageElement
    expect(img.src).toBe('https://example.com/preview-1')
    expect(img.width).toBe(320)
    expect(img.height).toBe(160)
  })

  it('renders no image for files without a preview', async () => {
    listFilesMock.mockResolvedValue(page(['a.txt'], 1))

    renderFileList()
    await screen.findByText('a.txt')

    expect(screen.queryByRole('presentation')).toBeNull()
  })

  it('shows a placeholder for image files whose preview is pending', async () => {
    listFilesMock.mockResolvedValue({
      files: [
        {
          id: '1',
          key: 'photo.jpg',
          contentType: 'image/jpeg',
          sizeBytes: 1,
          createdAt: null,
          previewUrl: null,
          previewWidth: null,
          previewHeight: null,
          previewStatus: PreviewStatus.PENDING,
        },
      ],
      nextPageToken: '',
    })

    renderFileList()
    await screen.findByText('photo.jpg')

    expect(screen.getByText('Processing…')).toBeDefined()
  })

  it('shows an indexing indicator for non-image files that are pending', async () => {
    listFilesMock.mockResolvedValue({
      files: [
        {
          id: '1',
          key: 'report.pdf',
          contentType: 'application/pdf',
          sizeBytes: 1,
          createdAt: null,
          previewUrl: null,
          previewWidth: null,
          previewHeight: null,
          previewStatus: PreviewStatus.PENDING,
        },
      ],
      nextPageToken: '',
    })

    renderFileList()
    await screen.findByText('report.pdf')

    expect(screen.getByText('Indexing…')).toBeDefined()
  })

  it('shows a failed indicator when preview indexing failed', async () => {
    listFilesMock.mockResolvedValue({
      files: [
        {
          id: '1',
          key: 'photo.jpg',
          contentType: 'image/jpeg',
          sizeBytes: 1,
          createdAt: null,
          previewUrl: null,
          previewWidth: null,
          previewHeight: null,
          previewStatus: PreviewStatus.FAILED,
        },
      ],
      nextPageToken: '',
    })

    renderFileList()
    await screen.findByText('photo.jpg')

    expect(screen.getByText('Preview failed')).toBeDefined()
  })

  it('deletes a file after confirmation and refetches the list', async () => {
    listFilesMock
      .mockResolvedValueOnce(page(['a.txt', 'b.txt'], 1))
      .mockResolvedValueOnce(page(['b.txt'], 2))
    deleteFileMock.mockResolvedValue(undefined)

    renderFileList()
    await screen.findByText('a.txt')

    fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0])

    // The row's Delete button only opens the confirmation dialog.
    expect(deleteFileMock).not.toHaveBeenCalled()

    fireEvent.click(await screen.findByRole('button', { name: 'Confirm delete' }))

    await waitFor(() => {
      expect(deleteFileMock).toHaveBeenCalledWith({ data: { id: '1' } })
      // Refetch after invalidation drops the deleted file.
      expect(listFilesMock).toHaveBeenCalledTimes(2)
    })
    await waitFor(() => expect(screen.queryByText('a.txt')).toBeNull())
    expect(screen.queryByRole('alertdialog')).toBeNull()
  })

  it('cancels the file delete confirmation without calling deleteFile', async () => {
    listFilesMock.mockResolvedValueOnce(page(['a.txt'], 1))

    renderFileList()
    await screen.findByText('a.txt')

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await screen.findByRole('alertdialog')

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('alertdialog')).toBeNull()
    expect(deleteFileMock).not.toHaveBeenCalled()
  })

  it('shows the delete error inside the confirmation dialog', async () => {
    listFilesMock.mockResolvedValueOnce(page(['a.txt'], 1))
    deleteFileMock.mockRejectedValue(new Error('boom'))

    renderFileList()
    await screen.findByText('a.txt')

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Confirm delete' }))

    await screen.findByRole('alert')
    // The dialog stays open on failure so the user can retry or cancel.
    expect(screen.getByRole('alertdialog')).toBeDefined()
  })

  it('routes the Metadata button to the file page via onOpenFile', async () => {
    listFilesMock.mockResolvedValue(page(['a.txt'], 1))
    const onOpenFile = vi.fn()

    renderFileList(undefined, onOpenFile)
    await screen.findByText('a.txt')

    fireEvent.click(screen.getByRole('button', { name: 'Metadata' }))

    expect(onOpenFile).toHaveBeenCalledWith('1')
  })

  it('has no upload UI — uploading is browse-mode-only (see Browser.tsx)', async () => {
    listFilesMock.mockResolvedValue(page(['a.txt'], 1))
    renderFileList()
    await screen.findByText('a.txt')

    expect(screen.queryByLabelText('Upload files')).toBeNull()
    expect(screen.queryByLabelText('Upload to')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Upload' })).toBeNull()
  })
})
