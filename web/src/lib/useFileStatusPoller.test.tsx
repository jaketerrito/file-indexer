import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { getFilePreviewStatuses } from '../server/files'
import { useFileStatusPoller } from './useFileStatusPoller'

vi.mock('../server/files', () => ({
  getFilePreviewStatuses: vi.fn(),
}))

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
}

describe('useFileStatusPoller', () => {
  const mock = vi.mocked(getFilePreviewStatuses)

  beforeEach(() => {
    mock.mockClear()
  })

  it('polls pending IDs and patches the cache', async () => {
    vi.useFakeTimers()
    mock.mockResolvedValue([
      {
        id: '1',
        previewStatus: PreviewStatus.READY,
        previewUrl: 'http://example.com/preview.jpg',
      },
    ])

    const queryClient = new QueryClient()
    queryClient.setQueryData(['files'], {
      pages: [
        {
          files: [
            {
              id: '1',
              previewStatus: PreviewStatus.PENDING,
              previewUrl: null,
            },
          ],
        },
      ],
    })

    const { unmount } = renderHook(
      () =>
        useFileStatusPoller(
          queryClient.getQueryData<{
            pages: Array<{
              files: Array<{ id: string; previewStatus: number; previewUrl: string | null }>
            }>
          }>(['files'])?.pages,
          ['files'],
        ),
      {
        wrapper: ({ children }) => (
          <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
        ),
      },
    )

    await vi.advanceTimersByTimeAsync(2500)
    expect(mock).toHaveBeenCalledTimes(1)
    expect(mock).toHaveBeenCalledWith({ data: { ids: ['1'] } })

    const data = queryClient.getQueryData(['files']) as {
      pages: Array<{ files: Array<{ previewStatus: number; previewUrl: string | null }> }>
    }
    expect(data.pages[0].files[0].previewStatus).toBe(PreviewStatus.READY)
    expect(data.pages[0].files[0].previewUrl).toBe('http://example.com/preview.jpg')

    unmount()
    vi.useRealTimers()
  })

  it('does nothing when there are no pending files', async () => {
    vi.useFakeTimers()

    const { unmount } = renderHook(
      () =>
        useFileStatusPoller(
          [{ files: [{ id: '1', previewStatus: PreviewStatus.READY, previewUrl: 'url' }] }],
          ['files'],
        ),
      { wrapper },
    )

    await vi.advanceTimersByTimeAsync(2500)
    expect(mock).not.toHaveBeenCalled()
    unmount()
    vi.useRealTimers()
  })
})
