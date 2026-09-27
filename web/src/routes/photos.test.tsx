import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewStatus } from '../gen/service/v1/files_pb'

import { routeTree } from '../routeTree.gen'
import type { FileDto } from '../server/impl'

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

import { listContentTypes, listFiles } from '../server/files'

const listFilesMock = vi.mocked(listFiles)
const listContentTypesMock = vi.mocked(listContentTypes)

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

function renderRoute(initialUrl = '/photos') {
  const history = createMemoryHistory({ initialEntries: [initialUrl] })
  const router = createRouter({ routeTree, history })
  render(<RouterProvider router={router} />)
  return router
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
  listFilesMock.mockResolvedValue({ files: [], nextPageToken: '' })
  listContentTypesMock.mockResolvedValue({ categories: [] })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('photos page', () => {
  it('renders filtered to image types sorted by takenAt descending', async () => {
    listFilesMock.mockResolvedValue({
      files: [file('1', 'photo.jpg', { previewUrl: 'https://example.com/preview-1' })],
      nextPageToken: '',
    })

    renderRoute('/photos')

    expect(await screen.findByRole('img')).toBeDefined()
    expect(listFilesMock).toHaveBeenCalledWith(
      expect.objectContaining({
        data: expect.objectContaining({
          query: '',
          contentType: 'image/',
          sortField: 'takenAt',
          sortOrder: 'desc',
        }),
      }),
    )
  })

  it('renders day separators from effective dates', async () => {
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

    renderRoute('/photos')

    expect(await screen.findByText('Jun 1, 2025')).toBeDefined()
    expect(screen.getByText('May 31, 2025')).toBeDefined()
    expect(screen.getAllByRole('img')).toHaveLength(3)
  })

  it('falls back to updatedAt for grouping when takenAt is absent', async () => {
    listFilesMock.mockResolvedValue({
      files: [file('1', 'a.jpg', { updatedAt: '2025-06-02T12:00:00.000Z' })],
      nextPageToken: '',
    })

    renderRoute('/photos')

    expect(await screen.findByText('Jun 2, 2025')).toBeDefined()
  })
})
