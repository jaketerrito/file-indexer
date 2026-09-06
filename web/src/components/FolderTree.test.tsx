import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { listDirectory } from '../server/files'
import type { FileDto, ListDirectoryInput, ListDirectoryResult } from '../server/impl'
import { FolderTree } from './FolderTree'

vi.mock('../server/files', () => ({
  listDirectory: vi.fn(),
}))

const listDirectoryMock = vi.mocked(listDirectory)

function file(key: string): FileDto {
  return {
    id: key,
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

type Page = Pick<ListDirectoryResult, 'directories' | 'files' | 'nextPageToken'>

/**
 * Stubs listDirectory per directory path; each queued page is returned in
 * order so paging behavior (fetch until a page contains files) is exercised.
 * Unlisted paths resolve to an empty listing.
 */
function mockDirectoryTree(pagesByPath: Record<string, Page[]>) {
  listDirectoryMock.mockImplementation((opts) => {
    // The serverFn's public call type is a loosely-typed options union; the
    // tests always invoke it with validated data.
    const { data } = opts as unknown as { data: ListDirectoryInput }
    const queue = pagesByPath[data.path ?? '']
    const page =
      queue && queue.length > 0 ? queue.shift() : { directories: [], files: [], nextPageToken: '' }
    return Promise.resolve(page as ListDirectoryResult)
  })
}

function renderTree(currentPath: string | undefined, onNavigate = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <FolderTree currentPath={currentPath} onNavigate={onNavigate} />
    </QueryClientProvider>,
  )
  return onNavigate
}

beforeEach(() => {
  mockDirectoryTree({})
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('FolderTree', () => {
  it('lists top-level folders and navigates on name click', async () => {
    mockDirectoryTree({
      '': [{ directories: ['docs/', 'photos/'], files: [file('a.txt')], nextPageToken: '' }],
    })
    const onNavigate = renderTree(undefined)

    const docs = await screen.findByRole('button', { name: 'docs' })
    expect(screen.getByRole('button', { name: 'photos' })).toBeTruthy()
    fireEvent.click(docs)
    expect(onNavigate).toHaveBeenCalledWith('docs/')
    // Children load lazily: only the root listing was fetched.
    for (const call of listDirectoryMock.mock.calls) {
      // Same options-union cast as mockDirectoryTree; every call carries data.
      const opts = call[0] as unknown as { data: ListDirectoryInput }
      expect(opts.data.path).toBe('')
    }
  })

  it('navigates to the root via Home', async () => {
    const onNavigate = renderTree(undefined)
    fireEvent.click(screen.getByRole('button', { name: 'Home' }))
    expect(onNavigate).toHaveBeenCalledWith('')
  })

  it('expands and collapses a folder, paging until files appear', async () => {
    mockDirectoryTree({
      '': [{ directories: ['photos/'], files: [file('a.txt')], nextPageToken: '' }],
      'photos/': [
        { directories: ['photos/2024/'], files: [], nextPageToken: 't2' },
        { directories: ['photos/2025/'], files: [file('photos/x.txt')], nextPageToken: '' },
      ],
    })
    renderTree(undefined)

    fireEvent.click(await screen.findByRole('button', { name: 'Expand folder photos' }))
    expect(await screen.findByRole('button', { name: '2024' })).toBeTruthy()
    expect(screen.getByRole('button', { name: '2025' })).toBeTruthy()
    // First page was all directories, so the fetch continued with the token.
    await waitFor(() =>
      expect(listDirectoryMock).toHaveBeenCalledWith({
        data: { path: 'photos/', pageToken: 't2', sortField: 'key', sortOrder: 'asc' },
      }),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Collapse folder photos' }))
    expect(screen.queryByRole('button', { name: '2024' })).toBeNull()
  })

  it('auto-expands ancestors of the current path and marks it current', async () => {
    mockDirectoryTree({
      '': [{ directories: ['photos/'], files: [file('a.txt')], nextPageToken: '' }],
      'photos/': [
        { directories: ['photos/2024/'], files: [file('photos/x.txt')], nextPageToken: '' },
      ],
    })
    renderTree('photos/2024/')

    const current = await screen.findByRole('button', { name: '2024' })
    expect(current.getAttribute('aria-current')).toBe('page')
    expect(
      screen.getByRole('button', { name: 'Collapse folder photos' }).getAttribute('aria-expanded'),
    ).toBe('true')
    expect(screen.getByRole('button', { name: 'Home' }).getAttribute('aria-current')).toBeNull()
  })

  it('reports load failures', async () => {
    listDirectoryMock.mockRejectedValue(new Error('boom'))
    renderTree(undefined)
    expect((await screen.findByRole('alert')).textContent).toContain('Failed to load folders')
  })
})
