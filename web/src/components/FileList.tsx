import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { deleteFile, getDownloadUrl, listFiles } from '../server/files'

const PAGE_SIZE = 50

export function FileList() {
  const queryClient = useQueryClient()

  const { data, error, isPending, isError, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: ['files'],
      queryFn: ({ pageParam }) =>
        listFiles({ data: { pageSize: PAGE_SIZE, pageToken: pageParam } }),
      initialPageParam: '',
      getNextPageParam: (lastPage) => lastPage.nextPageToken || undefined,
    })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteFile({ data: { id } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['files'] }),
  })

  // Infinite scroll: fetch the next page whenever the sentinel below the list
  // becomes visible.
  const sentinelRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel) return
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && hasNextPage && !isFetchingNextPage) {
        void fetchNextPage()
      }
    })
    observer.observe(sentinel)
    return () => observer.disconnect()
  }, [fetchNextPage, hasNextPage, isFetchingNextPage])

  async function handleDownload(id: string) {
    const { url } = await getDownloadUrl({ data: { id } })
    window.open(url, '_blank', 'noopener')
  }

  if (isPending) return <p>Loading…</p>
  if (isError) return <p role="alert">Failed to load files: {String(error)}</p>

  const files = data.pages.flatMap((page) => page.files)

  return (
    <div>
      {files.length === 0 ? (
        <p>No files.</p>
      ) : (
        <ul>
          {files.map((file) => (
            <li key={file.id}>
              <span>{file.key}</span>{' '}
              <button type="button" onClick={() => void handleDownload(file.id)}>
                Download
              </button>{' '}
              <button
                type="button"
                onClick={() => deleteMutation.mutate(file.id)}
                disabled={deleteMutation.isPending}
              >
                Delete
              </button>
            </li>
          ))}
        </ul>
      )}
      <div ref={sentinelRef} data-testid="scroll-sentinel" />
      {isFetchingNextPage ? <p>Loading more…</p> : null}
    </div>
  )
}
