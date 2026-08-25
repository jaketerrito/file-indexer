import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DEFAULT_FILTERS, type FileFilters, normalizeFilters } from '../lib/fileFilters'
import { Browser } from './Browser'

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

import { commitUpload, getUploadUrl, listDirectory, listFiles } from '../server/files'

const listFilesMock = vi.mocked(listFiles)
const listDirectoryMock = vi.mocked(listDirectory)
const getUploadUrlMock = vi.mocked(getUploadUrl)
const commitUploadMock = vi.mocked(commitUpload)

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

function Harness({ initial }: { initial: FileFilters }) {
  const [filters, setFilters] = useState(initial)
  return (
    <>
      <div data-testid="filters">{JSON.stringify(filters)}</div>
      <Browser filters={filters} onFiltersChange={setFilters} />
    </>
  )
}

function renderBrowser(initial: FileFilters = DEFAULT_FILTERS) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <Harness initial={initial} />
    </QueryClientProvider>,
  )
}

function currentFilters(): FileFilters {
  return JSON.parse(screen.getByTestId('filters').textContent as string)
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
  listFilesMock.mockResolvedValue({ files: [], nextPageToken: '' })
  listDirectoryMock.mockResolvedValue({ directories: [], files: [], nextPageToken: '' })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('Browser', () => {
  it('defaults to search mode with a Browse folders button', async () => {
    renderBrowser()

    expect(await screen.findByText('No files.')).toBeDefined()
    expect(screen.getByRole('button', { name: 'Browse folders' })).toBeDefined()
  })

  it('switches to browse mode at the root when Browse folders is clicked', async () => {
    renderBrowser()
    fireEvent.click(await screen.findByRole('button', { name: 'Browse folders' }))

    await waitFor(() => expect(currentFilters().path).toBe(''))
    expect(await screen.findByRole('navigation', { name: 'Breadcrumb' })).toBeDefined()
  })

  it('renders breadcrumbs and directory contents in browse mode', async () => {
    listDirectoryMock.mockResolvedValue({
      directories: ['docs/sub/'],
      files: [],
      nextPageToken: '',
    })
    renderBrowser(normalizeFilters({ path: 'docs/' }))

    expect(await screen.findByRole('button', { name: 'sub' })).toBeDefined()
    // "docs" is the current (last) breadcrumb segment, not a link.
    expect(screen.getByText('docs').getAttribute('aria-current')).toBe('page')
    expect(listDirectoryMock).toHaveBeenCalledWith(
      expect.objectContaining({ data: expect.objectContaining({ path: 'docs/' }) }),
    )
  })

  it('navigating a breadcrumb updates the path filter', async () => {
    listDirectoryMock.mockResolvedValue({
      directories: [],
      files: [],
      nextPageToken: '',
    })
    renderBrowser(normalizeFilters({ path: 'docs/sub/' }))

    fireEvent.click(await screen.findByRole('button', { name: 'Home' }))
    await waitFor(() => expect(currentFilters().path).toBe(''))
  })

  it('switches to search mode, seeded with the current path as prefix', async () => {
    renderBrowser(normalizeFilters({ path: 'docs/' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Search all files' }))

    await waitFor(() => {
      const f = currentFilters()
      expect(f.path).toBeUndefined()
      expect(f.prefix).toBe('docs/')
    })
  })

  it('creates a new folder by navigating to it, without a backend call', async () => {
    vi.stubGlobal(
      'prompt',
      vi.fn(() => 'photos'),
    )
    renderBrowser(normalizeFilters({ path: 'docs/' }))
    await screen.findByRole('button', { name: 'New folder' })

    fireEvent.click(screen.getByRole('button', { name: 'New folder' }))

    await waitFor(() => expect(currentFilters().path).toBe('docs/photos/'))
  })

  it('rejects a folder name containing "/"', async () => {
    const alertSpy = vi.fn()
    vi.stubGlobal(
      'prompt',
      vi.fn(() => 'a/b'),
    )
    vi.stubGlobal('alert', alertSpy)
    renderBrowser(normalizeFilters({ path: 'docs/' }))
    await screen.findByRole('button', { name: 'New folder' })

    fireEvent.click(screen.getByRole('button', { name: 'New folder' }))

    expect(alertSpy).toHaveBeenCalled()
    expect(currentFilters().path).toBe('docs/')
  })

  it('uploads to the current folder by default while browsing', async () => {
    getUploadUrlMock.mockResolvedValue({ url: 'https://s3/put-url' })
    commitUploadMock.mockResolvedValue({
      id: '1',
      key: 'docs/new.txt',
      contentType: '',
      sizeBytes: 3,
      createdAt: null,
      previewUrl: null,
      previewWidth: null,
      previewHeight: null,
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))

    renderBrowser(normalizeFilters({ path: 'docs/' }))
    await screen.findByRole('button', { name: 'Upload' })

    const file = new File(['abc'], 'new.txt', { type: 'text/plain' })
    fireEvent.change(screen.getByLabelText('Upload files'), { target: { files: [file] } })

    await waitFor(() =>
      expect(getUploadUrlMock).toHaveBeenCalledWith({ data: { key: 'docs/new.txt' } }),
    )
  })

  it('lets an upload override the default folder destination', async () => {
    getUploadUrlMock.mockResolvedValue({ url: 'https://s3/put-url' })
    commitUploadMock.mockResolvedValue({
      id: '1',
      key: 'elsewhere/new.txt',
      contentType: '',
      sizeBytes: 3,
      createdAt: null,
      previewUrl: null,
      previewWidth: null,
      previewHeight: null,
    })
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true }))

    renderBrowser(normalizeFilters({ path: 'docs/' }))
    await screen.findByRole('button', { name: 'Upload' })

    fireEvent.change(screen.getByLabelText('Upload to'), { target: { value: 'elsewhere' } })
    const file = new File(['abc'], 'new.txt', { type: 'text/plain' })
    fireEvent.change(screen.getByLabelText('Upload files'), { target: { files: [file] } })

    await waitFor(() =>
      expect(getUploadUrlMock).toHaveBeenCalledWith({ data: { key: 'elsewhere/new.txt' } }),
    )
  })

  it('resets the upload override after navigating to a different folder', async () => {
    renderBrowser(normalizeFilters({ path: 'docs/' }))
    const uploadInput = (await screen.findByLabelText('Upload to')) as HTMLInputElement
    fireEvent.change(uploadInput, { target: { value: 'elsewhere' } })
    expect(uploadInput.value).toBe('elsewhere')

    listDirectoryMock.mockResolvedValue({
      directories: ['docs/sub/'],
      files: [],
      nextPageToken: '',
    })
    fireEvent.click(await screen.findByRole('button', { name: 'Home' }))

    await waitFor(() => expect(currentFilters().path).toBe(''))
    const uploadInputAfter = (await screen.findByLabelText('Upload to')) as HTMLInputElement
    expect(uploadInputAfter.value).toBe('')
  })

  it('refetches sort/order for the directory listing', async () => {
    renderBrowser(normalizeFilters({ path: 'docs/' }))
    await screen.findByRole('button', { name: 'New folder' })

    fireEvent.change(screen.getByLabelText('Sort by'), { target: { value: 'size' } })
    await waitFor(() =>
      expect(listDirectoryMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ data: expect.objectContaining({ sortField: 'size' }) }),
      ),
    )
  })
})
