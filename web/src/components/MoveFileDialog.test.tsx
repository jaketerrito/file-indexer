import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { listDirectory } from '../server/files'
import type { FileDto, ListDirectoryInput, ListDirectoryResult } from '../server/impl'
import { MoveFileDialog } from './MoveFileDialog'

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

function mockDirectoryTree(pagesByPath: Record<string, Page[]>) {
  listDirectoryMock.mockImplementation((opts) => {
    const { data } = opts as unknown as { data: ListDirectoryInput }
    const queue = pagesByPath[data.path ?? '']
    const page =
      queue && queue.length > 0 ? queue.shift() : { directories: [], files: [], nextPageToken: '' }
    return Promise.resolve(page as ListDirectoryResult)
  })
}

function renderDialog(
  fileKey: string,
  {
    moving = false,
    error = null,
    onConfirm = vi.fn(),
    onCancel = vi.fn(),
  }: {
    moving?: boolean
    error?: unknown
    onConfirm?: (path: string) => void
    onCancel?: () => void
  } = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={queryClient}>
      <MoveFileDialog
        fileKey={fileKey}
        moving={moving}
        error={error}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />
    </QueryClientProvider>,
  )
  return { onConfirm, onCancel }
}

beforeEach(() => {
  mockDirectoryTree({})
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('MoveFileDialog', () => {
  it('renders the folder tree and default destination preview', async () => {
    mockDirectoryTree({
      '': [{ directories: ['docs/', 'photos/'], files: [file('a.txt')], nextPageToken: '' }],
    })
    renderDialog('docs/report.pdf')

    expect(await screen.findByRole('button', { name: 'docs' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'photos' })).toBeTruthy()
    expect(screen.getByText(/Destination:/).textContent).toContain('docs/report.pdf')
  })

  it('updates the destination preview when a folder is clicked', async () => {
    mockDirectoryTree({
      '': [{ directories: ['photos/'], files: [file('a.txt')], nextPageToken: '' }],
    })
    renderDialog('docs/report.pdf')

    fireEvent.click(await screen.findByRole('button', { name: 'photos' }))
    await waitFor(() =>
      expect(screen.getByText(/Destination:/).textContent).toContain('photos/report.pdf'),
    )
  })

  it('creates a new folder and updates the selection', async () => {
    mockDirectoryTree({
      '': [{ directories: ['docs/'], files: [file('a.txt')], nextPageToken: '' }],
    })
    renderDialog('report.pdf')

    fireEvent.change(screen.getByPlaceholderText('folder name'), { target: { value: 'archive' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() =>
      expect(screen.getByText(/Destination:/).textContent).toContain('archive/report.pdf'),
    )
  })

  it('rejects invalid new folder names', async () => {
    renderDialog('report.pdf')

    fireEvent.change(screen.getByPlaceholderText('folder name'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect((await screen.findByRole('alert')).textContent).toContain('cannot be empty')

    fireEvent.change(screen.getByPlaceholderText('folder name'), { target: { value: 'a/b' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect((await screen.findByRole('alert')).textContent).toContain('cannot contain "/"')
  })

  it('emits the selected path on confirm', async () => {
    mockDirectoryTree({
      '': [{ directories: ['photos/'], files: [file('a.txt')], nextPageToken: '' }],
    })
    const { onConfirm } = renderDialog('docs/report.pdf')

    fireEvent.click(await screen.findByRole('button', { name: 'photos' }))
    await waitFor(() =>
      expect(screen.getByText(/Destination:/).textContent).toContain('photos/report.pdf'),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Move here' }))
    expect(onConfirm).toHaveBeenCalledWith('photos/')
  })

  it('disables confirm when the destination is unchanged', async () => {
    renderDialog('docs/report.pdf')
    expect((screen.getByRole('button', { name: 'Move here' }) as HTMLButtonElement).disabled).toBe(
      true,
    )
  })

  it('renders an error string and keeps the dialog open', async () => {
    renderDialog('docs/report.pdf', { error: new Error('s3 error') })

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('s3 error')
    expect(screen.getByRole('alertdialog')).toBeTruthy()
  })

  it('closes via backdrop click', async () => {
    const { onCancel } = renderDialog('docs/report.pdf')

    // The backdrop is the fixed-position container.
    fireEvent.click(screen.getByRole('alertdialog'))
    expect(onCancel).toHaveBeenCalled()
  })

  it('closes via Escape key', async () => {
    const { onCancel } = renderDialog('docs/report.pdf')

    fireEvent.keyDown(screen.getByRole('alertdialog'), { key: 'Escape' })
    expect(onCancel).toHaveBeenCalled()
  })
})
