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
      queue && queue.length > 0
        ? queue.shift()
        : { directories: [], files: [], nextPageToken: '' }
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
  it('renders current path breadcrumbs and the destination preview', async () => {
    mockDirectoryTree({
      'docs/': [
        {
          directories: ['docs/work/', 'docs/archive/'],
          files: [file('docs/report.pdf')],
          nextPageToken: '',
        },
      ],
    })
    renderDialog('docs/report.pdf')

    expect(await screen.findByRole('button', { name: 'Bucket root' })).toBeTruthy()
    expect(screen.getByText('docs', { exact: true })).toBeTruthy()
    expect(screen.getByText('Destination:')).toBeTruthy()
    expect(screen.getByText(/Destination:/).textContent).toContain('docs/report.pdf')
  })

  it('paginates past a page of files to fetch later subdirectories', async () => {
    mockDirectoryTree({
      'docs/': [
        {
          directories: [],
          files: [file('docs/report.pdf')],
          nextPageToken: 'page2',
        },
        {
          directories: ['docs/work/', 'docs/archive/'],
          files: [],
          nextPageToken: '',
        },
      ],
    })
    renderDialog('docs/report.pdf')

    expect(await screen.findByRole('button', { name: 'work' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'archive' })).toBeTruthy()
  })

  it('lists the immediate subdirectories of the current path', async () => {
    mockDirectoryTree({
      'docs/': [
        {
          directories: ['docs/work/', 'docs/archive/'],
          files: [file('docs/report.pdf')],
          nextPageToken: '',
        },
      ],
    })
    renderDialog('docs/report.pdf')

    expect(await screen.findByRole('button', { name: 'work' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'archive' })).toBeTruthy()
  })

  it('navigates into a subdirectory when clicked and reloads its children', async () => {
    mockDirectoryTree({
      '': [{ directories: ['docs/'], files: [file('report.pdf')], nextPageToken: '' }],
      'docs/': [
        {
          directories: ['docs/work/', 'docs/archive/'],
          files: [file('docs/report.pdf')],
          nextPageToken: '',
        },
      ],
      'docs/work/': [
        { directories: [], files: [file('docs/work/report.pdf')], nextPageToken: '' },
      ],
    })
    renderDialog('report.pdf')

    fireEvent.click(await screen.findByRole('button', { name: 'docs' }))
    await waitFor(() =>
      expect(screen.getByText(/Destination:/).textContent).toContain('docs/report.pdf'),
    )
    expect(await screen.findByRole('button', { name: 'work' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'archive' })).toBeTruthy()
  })

  it('navigates up via breadcrumb clicks', async () => {
    mockDirectoryTree({
      'docs/work/': [
        {
          directories: [],
          files: [file('docs/work/report.pdf')],
          nextPageToken: '',
        },
      ],
      'docs/': [
        {
          directories: ['docs/work/'],
          files: [file('docs/report.pdf')],
          nextPageToken: '',
        },
      ],
      '': [{ directories: ['docs/'], files: [file('report.pdf')], nextPageToken: '' }],
    })
    renderDialog('docs/work/report.pdf')

    fireEvent.click(await screen.findByRole('button', { name: 'Bucket root' }))
    await waitFor(() =>
      expect(screen.getByText(/Destination:/).textContent).toContain('report.pdf'),
    )
    expect(screen.getByRole('button', { name: 'docs' })).toBeTruthy()
  })

  it('shows an empty state when the current directory has no subfolders', async () => {
    mockDirectoryTree({
      'docs/': [{ directories: [], files: [file('docs/report.pdf')], nextPageToken: '' }],
    })
    renderDialog('docs/report.pdf')

    expect(await screen.findByText('No subfolders')).toBeTruthy()
  })

  it('adds a new folder and navigates into it', async () => {
    mockDirectoryTree({
      '': [{ directories: [], files: [file('report.pdf')], nextPageToken: '' }],
    })
    renderDialog('report.pdf')

    fireEvent.click(await screen.findByRole('button', { name: 'Add folder' }))
    fireEvent.change(screen.getByLabelText('New folder name'), {
      target: { value: 'archive' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))

    await waitFor(() =>
      expect(screen.getByText(/Destination:/).textContent).toContain('archive/report.pdf'),
    )
  })

  it('rejects invalid new folder names', async () => {
    mockDirectoryTree({
      '': [{ directories: [], files: [file('report.pdf')], nextPageToken: '' }],
    })
    renderDialog('report.pdf')

    fireEvent.click(await screen.findByRole('button', { name: 'Add folder' }))
    fireEvent.change(screen.getByLabelText('New folder name'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect((await screen.findByRole('alert')).textContent).toContain('cannot be empty')

    fireEvent.change(screen.getByLabelText('New folder name'), { target: { value: 'a/b' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect((await screen.findByRole('alert')).textContent).toContain('cannot contain "/"')

    fireEvent.change(screen.getByLabelText('New folder name'), { target: { value: '.' } })
    fireEvent.click(screen.getByRole('button', { name: 'Create' }))
    expect((await screen.findByRole('alert')).textContent).toContain('cannot be "." or ".."')
  })

  it('cancels the add-folder input', async () => {
    mockDirectoryTree({
      '': [{ directories: [], files: [file('report.pdf')], nextPageToken: '' }],
    })
    renderDialog('report.pdf')

    fireEvent.click(await screen.findByRole('button', { name: 'Add folder' }))
    fireEvent.change(screen.getByLabelText('New folder name'), { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel new folder' }))

    expect(screen.queryByLabelText('New folder name')).toBeNull()
    expect(screen.getByRole('button', { name: 'Add folder' })).toBeTruthy()
  })

  it('emits the currently viewed path on confirm', async () => {
    mockDirectoryTree({
      '': [{ directories: ['docs/'], files: [file('report.pdf')], nextPageToken: '' }],
      'docs/': [
        { directories: [], files: [file('docs/report.pdf')], nextPageToken: '' },
      ],
    })
    const { onConfirm } = renderDialog('report.pdf')

    fireEvent.click(await screen.findByRole('button', { name: 'docs' }))
    await waitFor(() =>
      expect(screen.getByText(/Destination:/).textContent).toContain('docs/report.pdf'),
    )

    fireEvent.click(screen.getByRole('button', { name: 'Move here' }))
    expect(onConfirm).toHaveBeenCalledWith('docs/')
  })

  it('disables confirm when the destination is unchanged', async () => {
    renderDialog('docs/report.pdf')
    expect(
      (screen.getByRole('button', { name: 'Move here' }) as HTMLButtonElement).disabled,
    ).toBe(true)
  })

  it('renders an error string and keeps the dialog open', async () => {
    renderDialog('docs/report.pdf', { error: new Error('s3 error') })

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('s3 error')
    expect(screen.getByRole('alertdialog')).toBeTruthy()
  })

  it('closes via backdrop click', async () => {
    const { onCancel } = renderDialog('docs/report.pdf')

    fireEvent.click(screen.getByRole('alertdialog'))
    expect(onCancel).toHaveBeenCalled()
  })

  it('closes via Escape key', async () => {
    const { onCancel } = renderDialog('docs/report.pdf')

    fireEvent.keyDown(screen.getByRole('alertdialog'), { key: 'Escape' })
    expect(onCancel).toHaveBeenCalled()
  })
})
