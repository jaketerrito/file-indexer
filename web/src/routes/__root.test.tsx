import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { routeTree } from '../routeTree.gen'

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

import { listContentTypes, listDirectory, listFiles } from '../server/files'

const listFilesMock = vi.mocked(listFiles)
const listDirectoryMock = vi.mocked(listDirectory)
const listContentTypesMock = vi.mocked(listContentTypes)

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

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
  listFilesMock.mockResolvedValue({ files: [], nextPageToken: '' })
  listContentTypesMock.mockResolvedValue({ categories: [] })
  listDirectoryMock.mockResolvedValue({ directories: [], files: [], nextPageToken: '' })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('root layout nav', () => {
  it('has links to Files, Recents, and Photos', async () => {
    renderRoute('/')

    expect(await screen.findByRole('link', { name: 'Files' })).toBeDefined()
    expect(screen.getByRole('link', { name: 'Recents' })).toBeDefined()
    expect(screen.getByRole('link', { name: 'Photos' })).toBeDefined()
  })

  it('navigates to Recents when the Recents link is clicked', async () => {
    const router = renderRoute('/')
    await screen.findByRole('button', { name: 'New folder' })

    fireEvent.click(screen.getByRole('link', { name: 'Recents' }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/recents'))
    expect(screen.getByRole('link', { name: 'Recents' }).getAttribute('data-status')).toBe('active')
  })

  it('navigates to Photos when the Photos link is clicked', async () => {
    const router = renderRoute('/')
    await screen.findByRole('button', { name: 'New folder' })

    fireEvent.click(screen.getByRole('link', { name: 'Photos' }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/photos'))
    expect(screen.getByRole('link', { name: 'Photos' }).getAttribute('data-status')).toBe('active')
  })
})
