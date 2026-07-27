import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_FILTERS, type FileFilters } from '../lib/fileFilters'
import type { ListFilesResult } from '../server/impl'
import { FileList } from './FileList'

// The server functions module is mocked wholesale: on the client these are
// plain async functions, and unit tests must not touch the network.
vi.mock('../server/files', () => ({
  listFiles: vi.fn(),
  getDownloadUrl: vi.fn(),
  deleteFile: vi.fn(),
}))

import { deleteFile, getDownloadUrl, listFiles } from '../server/files'

const listFilesMock = vi.mocked(listFiles)
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
function Harness({ initial = DEFAULT_FILTERS }: { initial?: FileFilters }) {
  const [filters, setFilters] = useState(initial)
  return <FileList filters={filters} onFiltersChange={setFilters} />
}

function renderFileList(initial?: FileFilters) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <Harness initial={initial} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
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

  it('deletes a file and refetches the list', async () => {
    listFilesMock
      .mockResolvedValueOnce(page(['a.txt', 'b.txt'], 1))
      .mockResolvedValueOnce(page(['b.txt'], 2))
    deleteFileMock.mockResolvedValue(undefined)

    renderFileList()
    await screen.findByText('a.txt')

    screen.getAllByRole('button', { name: 'Delete' })[0]?.click()

    await waitFor(() => {
      expect(deleteFileMock).toHaveBeenCalledWith({ data: { id: '1' } })
      // Refetch after invalidation drops the deleted file.
      expect(listFilesMock).toHaveBeenCalledTimes(2)
    })
    await waitFor(() => expect(screen.queryByText('a.txt')).toBeNull())
  })
})
