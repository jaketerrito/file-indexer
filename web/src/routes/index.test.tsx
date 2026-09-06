import type { RegisteredRouter } from '@tanstack/react-router'
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { FileFilters } from '../lib/fileFilters'
import { routeTree } from '../routeTree.gen'
import type { ListDirectoryInput } from '../server/impl'

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
  // Path-aware: real ListDirectory children are strictly longer prefixes
  // than the queried path. Returning 'docs/' for every path would make
  // FolderTree's ancestor auto-expansion recurse forever (startsWith is
  // always true) and OOM the test worker. (The serverFn mock types its
  // options loosely — data is runtime-validated — hence the cast.)
  listDirectoryMock.mockImplementation((options) => {
    const { path } = (options as { data?: ListDirectoryInput } | undefined)?.data ?? {}
    return Promise.resolve(
      path === ''
        ? { directories: ['docs/'], files: [], nextPageToken: '' }
        : { directories: [], files: [], nextPageToken: '' },
    )
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

// The FolderTree sidebar and the directory listing both render a "docs"
// button; this finds the listing one (outside the tree's <nav>). Throws
// while only the tree button has rendered so waitFor retries.
function docsListButton(): HTMLElement {
  const button = screen
    .getAllByRole('button', { name: 'docs' })
    .find((b) => b.closest('nav') === null)
  if (!button) throw new Error('docs button in directory listing not found')
  return button
}

describe('folder navigation history', () => {
  it('folder navigation pushes; Back/Forward move between folders', async () => {
    const router = renderRoute()

    fireEvent.click(await screen.findByRole('button', { name: 'Home' }))
    await waitFor(() => expect(currentFilters(router).path).toBe(''))

    fireEvent.click(await waitFor(() => docsListButton()))
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

    fireEvent.click(await screen.findByRole('button', { name: 'Home' }))
    await waitFor(() => expect(currentFilters(router).path).toBe(''))
    fireEvent.click(await waitFor(() => docsListButton()))
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

  it('leaving browse mode via the app link pushes', async () => {
    const router = renderRoute('/?path=docs%2F')
    await screen.findByRole('button', { name: 'New folder' })
    await waitFor(() => expect(currentFilters(router).path).toBe('docs/'))

    // The header app link resets to the default filters — search mode with
    // no prefix seeding (toSearchFromPath is gone with the old button).
    fireEvent.click(screen.getByRole('link', { name: 'file-indexer' }))
    await waitFor(() => {
      expect(currentFilters(router).path).toBeUndefined()
      // stripSearchParams drops default values, so '' never reaches the URL.
      expect(currentFilters(router).prefix ?? '').toBe('')
    })

    router.history.back()
    await waitFor(() => expect(currentFilters(router).path).toBe('docs/'))
  })
})
