import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { FilePage, Route } from './file.$id'

vi.mock('../server/files', () => ({
  getFileMetadata: vi.fn(),
  getOpenUrl: vi.fn(),
}))

vi.mock('@tanstack/react-router', async () => {
  const actual = await vi.importActual('@tanstack/react-router')
  return {
    ...actual,
    useNavigate: () => vi.fn(),
  }
})

import { getFileMetadata, getOpenUrl } from '../server/files'

const getFileMetadataMock = vi.mocked(getFileMetadata)
const getOpenUrlMock = vi.mocked(getOpenUrl)

function mockMetadata(
  contentType: string,
  id = '1',
  exif: { hasExif: boolean; hasXmp: boolean; cameraModel?: string } | null = null,
) {
  getFileMetadataMock.mockResolvedValue({
    id,
    key: `files/${id}.${contentType.split('/')[1] ?? 'bin'}`,
    contentType,
    sizeBytes: 1,
    createdAt: null,
    updatedAt: '2026-04-01T12:34:56Z',
    exif: exif
      ? {
          hasExif: exif.hasExif,
          hasXmp: exif.hasXmp,
          xmpKeywords: [],
          cameraModel: exif.cameraModel,
        }
      : null,
  })
}

function renderFilePage(
  contentType: string,
  id = '1',
  exif: { hasExif: boolean; hasXmp: boolean; cameraModel?: string } | null = null,
) {
  vi.spyOn(Route, 'useParams').mockReturnValue({ id })
  mockMetadata(contentType, id, exif)

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <FilePage />
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  getOpenUrlMock.mockResolvedValue({ url: 'https://s3.example/open' })
})

afterEach(() => {
  vi.clearAllMocks()
})

describe('FilePage preview', () => {
  it('image content type renders an img with the open URL', async () => {
    renderFilePage('image/png')

    const img = (await screen.findByText(
      (_, element) => element?.tagName === 'IMG',
    )) as HTMLImageElement
    expect(img.src).toBe('https://s3.example/open')
  })

  it('video content type renders a video with the open URL', async () => {
    const { container } = renderFilePage('video/mp4')

    await waitFor(() => expect(container.querySelector('video')).toBeTruthy())
    const video = container.querySelector('video') as HTMLVideoElement
    expect(video.src).toBe('https://s3.example/open')
  })

  it('text content type renders an iframe with the open URL', async () => {
    renderFilePage('text/plain')

    const iframe = (await screen.findByTitle('File preview')) as HTMLIFrameElement
    expect(iframe.src).toBe('https://s3.example/open')
  })

  it('PDF content type renders an iframe with the open URL', async () => {
    renderFilePage('application/pdf')

    const iframe = (await screen.findByTitle('File preview')) as HTMLIFrameElement
    expect(iframe.src).toBe('https://s3.example/open')
  })

  it('shows an error message when fetching the open URL fails', async () => {
    getOpenUrlMock.mockRejectedValue(new Error('s3 unavailable'))
    renderFilePage('image/png')

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('s3 unavailable')
  })
})

describe('FilePage metadata', () => {
  it('renders updated date next to the file name by default', async () => {
    renderFilePage('image/png')

    expect(await screen.findByText('Updated Apr 1, 2026')).toBeTruthy()
  })

  it('opens a modal with the full metadata table when the Metadata button is clicked', async () => {
    renderFilePage('image/jpeg', '1', { hasExif: true, hasXmp: false, cameraModel: 'TestCam' })

    await screen.findByText('1.jpeg')
    fireEvent.click(screen.getByRole('button', { name: 'Metadata' }))

    expect(await screen.findByRole('dialog')).toBeTruthy()
    expect(screen.getByText('Camera (EXIF)')).toBeTruthy()
    expect(screen.getByText('TestCam')).toBeTruthy()
    expect(screen.getByText('Content type')).toBeTruthy()
  })
})
