import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { getFilePreviewStatuses } from '../server/files'

interface HasFiles {
  files: Array<{ id: string; previewStatus: number; previewUrl: string | null }>
}

/**
 * Polls the backend for preview-status transitions of any files that are
 * currently PENDING or PROCESSING, and patches them directly into the
 * infinite-query cache. This avoids re-fetching entire pages just to watch
 * a few items transition.
 */
export function useFileStatusPoller(pages: HasFiles[] | undefined, queryKey: unknown[]) {
  const queryClient = useQueryClient()
  const queryKeyRef = useRef(queryKey)
  queryKeyRef.current = queryKey
  const pagesRef = useRef(pages)
  pagesRef.current = pages

  useEffect(() => {
    const interval = setInterval(async () => {
      const pendingIds: string[] = []
      const currentPages = pagesRef.current
      if (currentPages) {
        for (const page of currentPages) {
          for (const file of page.files) {
            if (
              file.previewStatus === PreviewStatus.PENDING ||
              file.previewStatus === PreviewStatus.PROCESSING
            ) {
              pendingIds.push(file.id)
            }
          }
        }
      }

      if (pendingIds.length === 0) return

      try {
        const res = await getFilePreviewStatuses({ data: { ids: pendingIds } })
        const byId = new Map<string, { previewStatus: number; previewUrl: string | null }>()
        for (const s of res.statuses) {
          byId.set(s.id, { previewStatus: s.previewStatus, previewUrl: s.previewUrl })
        }

        queryClient.setQueryData(queryKeyRef.current, (old: unknown) => {
          if (!old || typeof old !== 'object') return old
          const cached = old as { pages: HasFiles[] }
          return {
            ...cached,
            pages: cached.pages.map((page) => ({
              ...page,
              files: page.files.map((file) => {
                const update = byId.get(file.id)
                if (!update) return file
                return {
                  ...file,
                  previewStatus: update.previewStatus,
                  previewUrl: update.previewUrl ?? file.previewUrl,
                }
              }),
            })),
          }
        })
      } catch {
        // Polling is best-effort; retry next tick.
      }
    }, 2000)

    return () => clearInterval(interval)
  }, [queryClient])
}
