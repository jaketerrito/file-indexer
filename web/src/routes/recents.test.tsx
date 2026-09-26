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

function file(id: string, key: string): FileDto {
  return {
    id,
    key,
    contentType: 'text/plain',
    sizeBytes: 1,
    createdAt: null,
    previewUrl: null,
    previewWidth: null,
    previewHeight: null,
    previewStatus: PreviewStatus.NONE,
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

function renderRoute(initialUrl = '/recents') {
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

describe('recents page', () => {
  it('renders with lastModified descending defaults', async () => {
    listFilesMock.mockResolvedValue({ files: [file('1', 'notes.txt')], nextPageToken: '' })

    renderRoute('/recents')

    expect(await screen.findByText('notes.txt')).toBeDefined()
    expect(listFilesMock).toHaveBeenCalledWith(
      expect.objectContaining({
        data: expect.objectContaining({
          query: '',
          contentType: '',
          sortField: 'lastModified',
          sortOrder: 'desc',
        }),
      }),
    )
  })

  it('hides the type filter', async () => {
    listFilesMock.mockResolvedValue({ files: [file('1', 'notes.txt')], nextPageToken: '' })

    renderRoute('/recents')
    await screen.findByText('notes.txt')

    expect(screen.queryByLabelText(/Type/)).toBeNull()
  })

  it('has no sort or type controls', async () => {
    listFilesMock.mockResolvedValue({ files: [file('1', 'notes.txt')], nextPageToken: '' })

    renderRoute('/recents')
    await screen.findByText('notes.txt')

    expect(screen.queryByLabelText(/Sort by/)).toBeNull()
    expect(screen.queryByLabelText(/Type/)).toBeNull()
  })
})
