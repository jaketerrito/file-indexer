import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { listFiles } from '../server/files'
import type { FileDto } from '../server/impl'
import { SearchBar } from './SearchBar'

vi.mock('../server/files', () => ({
  listFiles: vi.fn(),
}))

const listFilesMock = vi.mocked(listFiles)

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

function renderSearchBar(onSelect = vi.fn(), onSearch = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <SearchBar onSelect={onSelect} onSearch={onSearch} />
    </QueryClientProvider>,
  )
  return { onSelect, onSearch }
}

beforeEach(() => {
  listFilesMock.mockResolvedValue({ files: [], nextPageToken: '' })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('SearchBar', () => {
  it('queries by text after debounce and shows matching keys', async () => {
    listFilesMock.mockResolvedValue({
      files: [file('1', 'docs/notes.txt'), file('2', 'docs/todo.txt')],
      nextPageToken: '',
    })
    renderSearchBar()

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search files by path' }), {
      target: { value: 'docs' },
    })

    expect(await screen.findByRole('button', { name: 'docs/notes.txt' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'docs/todo.txt' })).toBeTruthy()
    expect(listFilesMock).toHaveBeenCalledWith({
      data: { query: 'docs', pageSize: 10, sortField: 'key', sortOrder: 'asc' },
    })
  })

  it('hands the selected file to onSelect and clears the box', async () => {
    const selected = file('1', 'docs/notes.txt')
    listFilesMock.mockResolvedValue({ files: [selected], nextPageToken: '' })
    const { onSelect } = renderSearchBar()

    fireEvent.change(screen.getByRole('searchbox', { name: 'Search files by path' }), {
      target: { value: 'docs' },
    })
    fireEvent.mouseDown(await screen.findByRole('button', { name: 'docs/notes.txt' }))

    expect(onSelect).toHaveBeenCalledWith(selected)
    const input = screen.getByRole('searchbox', { name: 'Search files by path' })
    expect((input as HTMLInputElement).value).toBe('')
    expect(screen.queryByRole('list')).toBeNull()
  })

  it('shows a no-matches row', async () => {
    renderSearchBar()
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search files by path' }), {
      target: { value: 'zzz' },
    })
    expect(await screen.findByText('No matches')).toBeTruthy()
  })

  it('does not search when the input is empty', async () => {
    renderSearchBar()
    const input = screen.getByRole('searchbox', { name: 'Search files by path' })
    fireEvent.change(input, { target: { value: 'a' } })
    fireEvent.change(input, { target: { value: '' } })
    await waitFor(() => expect(listFilesMock).not.toHaveBeenCalled())
    expect(screen.queryByRole('list')).toBeNull()
  })

  it('calls onSearch with the trimmed query on Enter and closes the dropdown', async () => {
    listFilesMock.mockResolvedValue({ files: [file('1', 'notes.txt')], nextPageToken: '' })
    const { onSearch } = renderSearchBar()

    const input = screen.getByRole('searchbox', { name: 'Search files by path' })
    fireEvent.change(input, { target: { value: '  notes  ' } })
    // Wait for the debounced dropdown to open with a result.
    await screen.findByRole('button', { name: 'notes.txt' })

    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onSearch).toHaveBeenCalledWith('notes')
    expect(screen.queryByRole('list')).toBeNull()
    // The input keeps its text so the box still shows what was searched.
    expect((input as HTMLInputElement).value).toBe('  notes  ')
  })

  it('does nothing on Enter with an empty query', () => {
    const { onSearch } = renderSearchBar()
    const input = screen.getByRole('searchbox', { name: 'Search files by path' })

    fireEvent.keyDown(input, { key: 'Enter' })

    expect(onSearch).not.toHaveBeenCalled()
  })
})
