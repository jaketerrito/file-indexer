import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import type { ListDirectoryResult } from '../server/impl'
import { DirectoryList } from './DirectoryList'

vi.mock('../server/files', () => ({
  listDirectory: vi.fn(),
  getDirectoryStats: vi.fn(),
  deleteDirectory: vi.fn(),
  deleteFile: vi.fn(),
  getDownloadUrl: vi.fn(),
  getFileMetadata: vi.fn(),
}))

import {
  deleteDirectory,
  deleteFile,
  getDirectoryStats,
  getDownloadUrl,
  getFileMetadata,
  listDirectory,
} from '../server/files'

const listDirectoryMock = vi.mocked(listDirectory)
const getDirectoryStatsMock = vi.mocked(getDirectoryStats)
const deleteDirectoryMock = vi.mocked(deleteDirectory)
const deleteFileMock = vi.mocked(deleteFile)
const getDownloadUrlMock = vi.mocked(getDownloadUrl)
const getFileMetadataMock = vi.mocked(getFileMetadata)

function page(
  directories: string[],
  files: { id: string; key: string }[],
  nextPageToken = '',
): ListDirectoryResult {
  return {
    directories,
    files: files.map((f) => ({
      id: f.id,
      key: f.key,
      contentType: 'text/plain',
      sizeBytes: 1,
      createdAt: null,
      previewUrl: null,
      previewWidth: null,
      previewHeight: null,
      previewStatus: PreviewStatus.NONE,
    })),
    nextPageToken,
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

function renderDirectoryList(path = 'docs/') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const onNavigate = vi.fn()
  render(
    <QueryClientProvider client={queryClient}>
      <DirectoryList path={path} sort="key" order="asc" onNavigate={onNavigate} />
    </QueryClientProvider>,
  )
  return { onNavigate }
}

beforeEach(() => {
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
  intersectionCallback = undefined
})

describe('DirectoryList', () => {
  it('renders directories and files with only their basename', async () => {
    listDirectoryMock.mockResolvedValue(
      page(['docs/sub/', 'docs/sub2/'], [{ id: '1', key: 'docs/a.txt' }]),
    )

    renderDirectoryList()

    expect(await screen.findByRole('button', { name: 'sub' })).toBeDefined()
    expect(screen.getByRole('button', { name: 'sub2' })).toBeDefined()
    expect(screen.getByText('a.txt')).toBeDefined()
    // Full keys must not leak into the row text.
    expect(screen.queryByText('docs/a.txt')).toBeNull()
    expect(listDirectoryMock).toHaveBeenCalledWith({
      data: { path: 'docs/', pageSize: 50, pageToken: '', sortField: 'key', sortOrder: 'asc' },
    })
  })

  it('shows an empty state for a folder with nothing in it', async () => {
    listDirectoryMock.mockResolvedValue(page([], []))
    renderDirectoryList()
    expect(await screen.findByText(/No files yet/)).toBeDefined()
  })

  it('shows an error state when loading fails', async () => {
    listDirectoryMock.mockRejectedValue(new Error('boom'))
    renderDirectoryList()
    expect(await screen.findByRole('alert')).toBeDefined()
  })

  it('navigates into a subdirectory when clicked', async () => {
    listDirectoryMock.mockResolvedValue(page(['docs/sub/'], []))
    const { onNavigate } = renderDirectoryList()

    fireEvent.click(await screen.findByRole('button', { name: 'sub' }))
    expect(onNavigate).toHaveBeenCalledWith('docs/sub/')
  })

  it('fetches the next page when the sentinel intersects', async () => {
    listDirectoryMock
      .mockResolvedValueOnce(page([], [{ id: '1', key: 'docs/a.txt' }], 'cursor-1'))
      .mockResolvedValueOnce(page([], [{ id: '2', key: 'docs/b.txt' }]))

    renderDirectoryList()
    await screen.findByText('a.txt')

    triggerIntersection()

    expect(await screen.findByText('b.txt')).toBeDefined()
    expect(listDirectoryMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ data: expect.objectContaining({ pageToken: 'cursor-1' }) }),
    )
  })

  it('deletes a file after confirmation and refetches the directory', async () => {
    listDirectoryMock
      .mockResolvedValueOnce(page([], [{ id: '1', key: 'docs/a.txt' }]))
      .mockResolvedValueOnce(page([], []))
    deleteFileMock.mockResolvedValue(undefined)

    renderDirectoryList()
    await screen.findByText('a.txt')

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))

    // The row's Delete button only opens the confirmation dialog.
    expect(deleteFileMock).not.toHaveBeenCalled()

    fireEvent.click(await screen.findByRole('button', { name: 'Confirm delete' }))

    await waitFor(() => {
      expect(deleteFileMock).toHaveBeenCalledWith({ data: { id: '1' } })
      expect(listDirectoryMock).toHaveBeenCalledTimes(2)
    })
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
  })

  it('cancels the file delete confirmation without calling deleteFile', async () => {
    listDirectoryMock.mockResolvedValueOnce(page([], [{ id: '1', key: 'docs/a.txt' }]))

    renderDirectoryList()
    await screen.findByText('a.txt')

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await screen.findByRole('alertdialog')

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('alertdialog')).toBeNull()
    expect(deleteFileMock).not.toHaveBeenCalled()
  })

  it('opens the presigned URL when Download is clicked', async () => {
    listDirectoryMock.mockResolvedValue(page([], [{ id: '7', key: 'docs/a.txt' }]))
    getDownloadUrlMock.mockResolvedValue({ url: 'http://s3/presigned' })
    const openSpy = vi.fn()
    vi.stubGlobal('open', openSpy)

    renderDirectoryList()
    fireEvent.click(await screen.findByRole('button', { name: 'Download' }))

    await waitFor(() => {
      expect(getDownloadUrlMock).toHaveBeenCalledWith({ data: { id: '7' } })
      expect(openSpy).toHaveBeenCalledWith('http://s3/presigned', '_blank', 'noopener')
    })
  })

  it('opens the metadata modal', async () => {
    listDirectoryMock.mockResolvedValue(page([], [{ id: '1', key: 'docs/a.txt' }]))
    getFileMetadataMock.mockResolvedValue({
      id: '1',
      key: 'docs/a.txt',
      contentType: 'text/plain',
      sizeBytes: 2048,
      createdAt: null,
      updatedAt: null,
      exif: null,
    })

    renderDirectoryList()
    await screen.findByText('a.txt')
    fireEvent.click(screen.getByRole('button', { name: 'Metadata' }))

    expect(await screen.findByRole('dialog')).toBeDefined()
  })

  it('shows a placeholder for pending image previews', async () => {
    listDirectoryMock.mockResolvedValue({
      directories: [],
      files: [
        {
          id: '1',
          key: 'docs/photo.jpg',
          contentType: 'image/jpeg',
          sizeBytes: 1,
          createdAt: null,
          previewUrl: null,
          previewWidth: null,
          previewHeight: null,
          previewStatus: PreviewStatus.PENDING,
        },
      ],
      nextPageToken: '',
    })

    renderDirectoryList()
    await screen.findByText('photo.jpg')

    expect(screen.getByText('Processing…')).toBeDefined()
  })

  it('shows an indexing indicator for pending non-images', async () => {
    listDirectoryMock.mockResolvedValue({
      directories: [],
      files: [
        {
          id: '1',
          key: 'docs/report.pdf',
          contentType: 'application/pdf',
          sizeBytes: 1,
          createdAt: null,
          previewUrl: null,
          previewWidth: null,
          previewHeight: null,
          previewStatus: PreviewStatus.PENDING,
        },
      ],
      nextPageToken: '',
    })

    renderDirectoryList()
    await screen.findByText('report.pdf')

    expect(screen.getByText('Indexing…')).toBeDefined()
  })

  it('shows a failed indicator for failed previews', async () => {
    listDirectoryMock.mockResolvedValue({
      directories: [],
      files: [
        {
          id: '1',
          key: 'docs/photo.jpg',
          contentType: 'image/jpeg',
          sizeBytes: 1,
          createdAt: null,
          previewUrl: null,
          previewWidth: null,
          previewHeight: null,
          previewStatus: PreviewStatus.FAILED,
        },
      ],
      nextPageToken: '',
    })

    renderDirectoryList()
    await screen.findByText('photo.jpg')

    expect(screen.getByText('Preview failed')).toBeDefined()
  })

  it('shows folder contents in a confirm dialog before deleting', async () => {
    listDirectoryMock.mockResolvedValueOnce(page(['docs/sub/'], []))
    getDirectoryStatsMock.mockResolvedValue({ fileCount: 3, totalBytes: 2048 })

    renderDirectoryList()
    fireEvent.click(await screen.findByRole('button', { name: 'Delete folder' }))

    expect(await screen.findByRole('alertdialog')).toBeDefined()
    expect(getDirectoryStatsMock).toHaveBeenCalledWith({ data: { path: 'docs/sub/' } })
    expect(await screen.findByText(/3 files/)).toBeDefined()
    expect(screen.getByText(/2\.0 KB/)).toBeDefined()
  })

  it('deletes the folder on confirm and refetches', async () => {
    listDirectoryMock
      .mockResolvedValueOnce(page(['docs/sub/'], []))
      .mockResolvedValueOnce(page([], []))
    getDirectoryStatsMock.mockResolvedValue({ fileCount: 1, totalBytes: 10 })
    deleteDirectoryMock.mockResolvedValue({ deletedCount: 1 })

    renderDirectoryList()
    fireEvent.click(await screen.findByRole('button', { name: 'Delete folder' }))
    // Wait for stats to resolve: Confirm delete is disabled until then.
    await screen.findByText(/1 file/)

    fireEvent.click(screen.getByRole('button', { name: 'Confirm delete' }))

    await waitFor(() => {
      expect(deleteDirectoryMock).toHaveBeenCalledWith({ data: { path: 'docs/sub/' } })
    })
    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull())
  })

  it('cancels the folder delete confirmation without calling deleteDirectory', async () => {
    listDirectoryMock.mockResolvedValueOnce(page(['docs/sub/'], []))
    getDirectoryStatsMock.mockResolvedValue({ fileCount: 1, totalBytes: 10 })

    renderDirectoryList()
    fireEvent.click(await screen.findByRole('button', { name: 'Delete folder' }))
    await screen.findByRole('alertdialog')

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('alertdialog')).toBeNull()
    expect(deleteDirectoryMock).not.toHaveBeenCalled()
  })

  it('shows an error in the confirm dialog when delete fails', async () => {
    listDirectoryMock.mockResolvedValueOnce(page(['docs/sub/'], []))
    getDirectoryStatsMock.mockResolvedValue({ fileCount: 1, totalBytes: 10 })
    deleteDirectoryMock.mockRejectedValue(new Error('s3 error'))

    renderDirectoryList()
    fireEvent.click(await screen.findByRole('button', { name: 'Delete folder' }))
    await screen.findByText(/1 file/)
    fireEvent.click(screen.getByRole('button', { name: 'Confirm delete' }))

    expect(await screen.findByRole('alert')).toBeDefined()
  })
})
