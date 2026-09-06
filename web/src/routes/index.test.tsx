import type { RegisteredRouter } from '@tanstack/react-router'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { FileFilters } from '../lib/fileFilters'
import { routeTree } from '../routeTree.gen'

vi.mock('../server/files', () => ({
  listFiles: vi.fn(),
  listDirectory: vi.fn(),
  getDirectoryStats: vi.fn(),
  deleteDirectory: vi.fn(),
  deleteFile: vi.fn(),
  getDownloadUrl: vi.fn(),
  getFileMetadata: vi.fn(),
  getUploadUrl: vi.fn(),
  commitUpload: vi.fn(),
}))

import { listDirectory, listFiles } from '../server/files'

const listFilesMock = vi.mocked(listFiles)
const listDirectoryMock = vi.mocked(listDirectory)

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

function renderRoute(initialUrl = '/') {
  const history = createMemoryHistory({ initialEntries: [initialUrl] })
  const router = createRouter({ routeTree, history })
  render(<RouterProvider router={router} />)
  return router
}

// Names the location-search read + cast used across every navigation
// assertion; RegisteredRouter is the route-tree-registered router type
// (augmented in routeTree.gen.ts).
function currentFilters(router: RegisteredRouter): FileFilters {
  return router.state.location.search as FileFilters
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
  listFilesMock.mockResolvedValue({ files: [], nextPageToken: '' })
  listDirectoryMock.mockResolvedValue({ directories: ['docs/'], files: [], nextPageToken: '' })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('folder navigation history', () => {
  it('folder navigation pushes; Back/Forward move between folders', async () => {
    const router = renderRoute()

    fireEvent.click(await screen.findByRole('button', { name: 'Browse folders' }))
    await waitFor(() => expect(currentFilters(router).path).toBe(''))

    fireEvent.click(await screen.findByRole('button', { name: 'docs' }))
    await waitFor(() => expect(currentFilters(router).path).toBe('docs/'))

    router.history.back()
    await waitFor(() => expect(currentFilters(router).path).toBe(''))

    router.history.forward()
    await waitFor(() => expect(currentFilters(router).path).toBe('docs/'))

    router.history.back()
    await waitFor(() => expect(currentFilters(router).path).toBe(''))
    router.history.back()
    await waitFor(() => expect(currentFilters(router).path).toBeUndefined())
  })

  it('sort change replaces — no history entry', async () => {
    const router = renderRoute()

    fireEvent.click(await screen.findByRole('button', { name: 'Browse folders' }))
    await waitFor(() => expect(currentFilters(router).path).toBe(''))
    fireEvent.click(await screen.findByRole('button', { name: 'docs' }))
    await waitFor(() => expect(currentFilters(router).path).toBe('docs/'))

    fireEvent.change(screen.getByLabelText('Sort by'), { target: { value: 'size' } })
    await waitFor(() => {
      expect(currentFilters(router).path).toBe('docs/')
      expect(currentFilters(router).sort).toBe('size')
    })

    // The pre-sort docs/ entry was replaced, so Back skips straight past it
    // to the root browse entry — a push would have landed back on docs/.
    router.history.back()
    await waitFor(() => expect(currentFilters(router).path).toBe(''))
  })

  it('leaving browse mode via "Search all files" pushes', async () => {
    const router = renderRoute('/?path=docs%2F')
    await screen.findByRole('button', { name: 'Search all files' })
    await waitFor(() => expect(currentFilters(router).path).toBe('docs/'))

    fireEvent.click(screen.getByRole('button', { name: 'Search all files' }))
    await waitFor(() => {
      expect(currentFilters(router).path).toBeUndefined()
      expect(currentFilters(router).prefix).toBe('docs/')
    })

    router.history.back()
    await waitFor(() => expect(currentFilters(router).path).toBe('docs/'))
  })
})
