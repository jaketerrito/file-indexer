import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import type { FileDto } from '../server/impl'
import { PhotoWall } from './PhotoWall'

vi.mock('../server/files', () => ({
  listFiles: vi.fn(),
  listContentTypes: vi.fn(),
  listDirectory: vi.fn(),
  getDirectoryStats: vi.fn(),
  deleteDirectory: vi.fn(),
  deleteFile: vi.fn(),
  getDownloadUrl: vi.fn(),
  getFileMetadata: vi.fn(),
  getFilePreviewStatuses: vi.fn(),
  getUploadUrl: vi.fn(),
  commitUpload: vi.fn(),
}))

import { listFiles } from '../server/files'

const listFilesMock = vi.mocked(listFiles)

function file(
  id: string,
  key: string,
  overrides: {
    takenAt?: string
    updatedAt?: string
    previewUrl?: string
    previewStatus?: number
  } = {},
): FileDto {
  return {
    id,
    key,
    contentType: 'image/jpeg',
    sizeBytes: 1,
    createdAt: null,
    updatedAt: overrides.updatedAt ?? null,
    takenAt: overrides.takenAt ?? null,
    previewUrl: overrides.previewUrl ?? null,
    previewWidth: null,
    previewHeight: null,
    previewStatus: overrides.previewStatus ?? PreviewStatus.NONE,
  }
}

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

function renderPhotoWall(onOpenFile: (id: string) => void = () => {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <PhotoWall onOpenFile={onOpenFile} />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
  listFilesMock.mockReset()
  listFilesMock.mockResolvedValue({ files: [], nextPageToken: '' })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
  intersectionCallback = undefined
})

describe('PhotoWall', () => {
  it('requests image files sorted by takenAt descending', async () => {
    listFilesMock.mockResolvedValue({ files: [file('1', 'photo.jpg')], nextPageToken: '' })

    renderPhotoWall()

    await waitFor(() => {
      expect(listFilesMock).toHaveBeenCalledWith({
        data: {
          pageSize: 50,
          pageToken: '',
          query: '',
          contentType: 'image/',
          sortField: 'takenAt',
          sortOrder: 'desc',
        },
      })
    })
  })

  it('renders day separators and preview images', async () => {
    listFilesMock.mockResolvedValue({
      files: [
        file('1', 'a.jpg', {
          takenAt: '2025-06-01T12:00:00.000Z',
          previewUrl: 'https://example.com/preview-1',
        }),
        file('2', 'b.jpg', {
          takenAt: '2025-06-01T10:00:00.000Z',
          previewUrl: 'https://example.com/preview-2',
        }),
        file('3', 'c.jpg', {
          takenAt: '2025-05-31T08:00:00.000Z',
          previewUrl: 'https://example.com/preview-3',
        }),
      ],
      nextPageToken: '',
    })

    renderPhotoWall()

    expect(await screen.findByText('Jun 1, 2025')).toBeDefined()
    expect(screen.getByText('May 31, 2025')).toBeDefined()
    expect(screen.getAllByRole('img')).toHaveLength(3)
  })

  it('calls onOpenFile when a thumbnail is clicked', async () => {
    const onOpenFile = vi.fn()
    listFilesMock.mockResolvedValue({
      files: [
        file('42', 'photo.jpg', {
          takenAt: '2025-06-01T12:00:00.000Z',
          previewUrl: 'https://example.com/preview-42',
        }),
      ],
      nextPageToken: '',
    })

    renderPhotoWall(onOpenFile)

    const button = await screen.findByRole('button', { name: 'photo.jpg' })
    fireEvent.click(button)

    await waitFor(() => {
      expect(onOpenFile).toHaveBeenCalledWith('42')
    })
  })

  it('renders placeholder states for non-ready previews', async () => {
    listFilesMock.mockResolvedValue({
      files: [
        file('1', 'pending.jpg', { previewStatus: PreviewStatus.PENDING }),
        file('2', 'processing.jpg', { previewStatus: PreviewStatus.PROCESSING }),
        file('3', 'failed.jpg', { previewStatus: PreviewStatus.FAILED }),
        file('4', 'none.jpg', { previewStatus: PreviewStatus.NONE }),
      ],
      nextPageToken: '',
    })

    renderPhotoWall()

    expect(await screen.findAllByText('Processing…')).toHaveLength(2)
    expect(screen.getByText('Preview failed')).toBeDefined()
    expect(screen.getByText('No preview')).toBeDefined()
  })

  it('fetches the next page when the scroll sentinel is visible', async () => {
    listFilesMock
      .mockResolvedValueOnce({
        files: [file('1', 'page1.jpg', { takenAt: '2025-06-01T12:00:00.000Z' })],
        nextPageToken: 'token-2',
      })
      .mockResolvedValueOnce({
        files: [file('2', 'page2.jpg', { takenAt: '2025-05-31T12:00:00.000Z' })],
        nextPageToken: '',
      })

    renderPhotoWall()

    await screen.findByRole('button', { name: 'page1.jpg' })
    triggerIntersection()

    await waitFor(() => {
      expect(listFilesMock).toHaveBeenCalledTimes(2)
    })
    expect(screen.getByRole('button', { name: 'page2.jpg' })).toBeDefined()
  })

  it('shows empty state when no photos are returned', async () => {
    listFilesMock.mockResolvedValue({ files: [], nextPageToken: '' })

    renderPhotoWall()

    expect(await screen.findByText('No photos.')).toBeDefined()
  })

  it('shows error state when listFiles fails', async () => {
    listFilesMock.mockRejectedValue(new Error('service unavailable'))

    renderPhotoWall()

    expect((await screen.findByRole('alert')).textContent).toContain('service unavailable')
  })
})
