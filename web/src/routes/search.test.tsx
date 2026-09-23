import type { RegisteredRouter } from '@tanstack/react-router'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { normalizeSearchFilters, type SearchFilters } from '../lib/fileFilters'
import { routeTree } from '../routeTree.gen'
import type { FileDto } from '../server/impl'

// Same wholesale server-function mock as the index route tests; the route
// tree (and the header SearchBar) is shared. listContentTypes is mocked too
// because /search renders FileList, whose type dropdown degrades silently
// without it.
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

function renderRoute(initialUrl = '/search') {
  const history = createMemoryHistory({ initialEntries: [initialUrl] })
  const router = createRouter({ routeTree, history })
  render(<RouterProvider router={router} />)
  return router
}

// Names the location-search read used across every navigation assertion.
// location.search is the raw (stripped) URL search — defaults the route's
// validateSearch fills in never reach the URL — so run it through the same
// normalizer to assert on what the route actually sees. RegisteredRouter is
// the route-tree-registered router type (augmented in routeTree.gen.ts).
function currentFilters(router: RegisteredRouter): SearchFilters {
  return normalizeSearchFilters(router.state.location.search)
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

describe('search results page', () => {
  it('query param drives the results list', async () => {
    listFilesMock.mockResolvedValue({ files: [file('1', 'notes.txt')], nextPageToken: '' })

    renderRoute('/search?query=notes')

    expect(await screen.findByRole('heading', { name: 'Search results' })).toBeDefined()
    expect(await screen.findByText('notes.txt')).toBeDefined()
    expect(listFilesMock).toHaveBeenCalledWith(
      expect.objectContaining({ data: expect.objectContaining({ query: 'notes' }) }),
    )
  })

  it('filter changes replace the URL entry', async () => {
    const router = renderRoute('/search?query=notes')
    await screen.findByRole('heading', { name: 'Search results' })

    fireEvent.change(screen.getByLabelText(/Sort by/), { target: { value: 'size' } })

    // In-place tweaks replace (replace: true), so the query survives and no
    // history entry piles up.
    await waitFor(() => {
      expect(currentFilters(router).sort).toBe('size')
      expect(currentFilters(router).query).toBe('notes')
    })
  })
})
