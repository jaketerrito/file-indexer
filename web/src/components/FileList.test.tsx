import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
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
    })),
    nextPageToken,
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

function renderFileList() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <FileList />
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
    expect(listFilesMock).toHaveBeenCalledWith({ data: { pageSize: 50, pageToken: '' } })
  })

  it('shows an empty state', async () => {
    listFilesMock.mockResolvedValue(page([], 0))
    renderFileList()
    expect(await screen.findByText('No files.')).toBeDefined()
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
    expect(listFilesMock).toHaveBeenLastCalledWith({
      data: { pageSize: 50, pageToken: 'cursor-1' },
    })
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
