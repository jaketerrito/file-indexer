import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { useFileStatusPoller } from '../lib/useFileStatusPoller'
import {
  deleteDirectory,
  deleteFile,
  getDirectoryStats,
  getDownloadUrl,
  getFileMetadata,
  listDirectory,
} from '../server/files'
import type { SortFieldInput, SortOrderInput } from '../server/impl'
import { FileMetadataModal, formatBytes } from './FileMetadataModal'

const PAGE_SIZE = 50

interface DirectoryListProps {
  /** Directory being browsed; "" is the bucket root. */
  path: string
  sort: SortFieldInput
  order: SortOrderInput
  onNavigate: (path: string) => void
}

/**
 * Lists a directory's immediate children: subdirectories (always
 * alphabetical, always first — see ListDirectory's two-phase design) then
 * files directly in it. Deleting a folder is a two-step confirm: clicking
 * Delete fetches GetDirectoryStats to show what's about to go before a
 * second click actually runs DeleteDirectory, since it is a non-atomic,
 * unrecoverable bulk operation.
 */
export function DirectoryList({ path, sort, order, onNavigate }: DirectoryListProps) {
  const queryClient = useQueryClient()
  const [metadataId, setMetadataId] = useState<string | null>(null)

  const { data, error, isPending, isError, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: ['directory', path, sort, order],
      queryFn: ({ pageParam }) =>
        listDirectory({
          data: {
            path,
            pageSize: PAGE_SIZE,
            pageToken: pageParam,
            sortField: sort,
            sortOrder: order,
          },
        }),
      initialPageParam: '',
      getNextPageParam: (lastPage) => lastPage.nextPageToken || undefined,
    })

  const deleteFileMutation = useMutation({
    mutationFn: (id: string) => deleteFile({ data: { id } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['directory', path] }),
  })

  // Folder pending delete confirmation, or null when none. Stats are fetched
  // once a folder is selected so the confirm dialog can show what's about to
  // be removed (DeleteDirectory has no dry-run flag by design; this is the
  // client-side substitute).
  const [folderToDelete, setFolderToDelete] = useState<string | null>(null)

  useFileStatusPoller(data?.pages, ['directory', path, sort, order])
  const statsQuery = useQuery({
    queryKey: ['directory-stats', folderToDelete],
    queryFn: () => getDirectoryStats({ data: { path: folderToDelete as string } }),
    enabled: folderToDelete !== null,
  })
  const deleteDirMutation = useMutation({
    mutationFn: (p: string) => deleteDirectory({ data: { path: p } }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['directory'] })
      setFolderToDelete(null)
    },
  })

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
  if (isError) return <p role="alert">Failed to load directory: {String(error)}</p>

  const directories = data.pages[0]?.directories ?? []
  const files = data.pages.flatMap((p) => p.files)
  // Direct children only (ListDirectory's direct_only filter guarantees
  // this), so the basename is everything after the current path. Directory
  // names additionally drop their trailing "/" for display — the folder
  // icon already carries that meaning; onNavigate still gets the full
  // trailing-slash prefix (dir), unaffected by this.
  const basename = (key: string) => key.slice(path.length)
  const dirName = (dir: string) => basename(dir).replace(/\/$/, '')

  return (
    <div>
      {directories.length === 0 && files.length === 0 ? (
        <p>No files yet — upload here, or navigate into a subfolder.</p>
      ) : (
        <ul>
          {directories.map((dir) => (
            <li key={dir}>
              📁{' '}
              <button type="button" onClick={() => onNavigate(dir)}>
                {dirName(dir)}
              </button>{' '}
              <button type="button" onClick={() => setFolderToDelete(dir)}>
                Delete folder
              </button>
            </li>
          ))}
          {files.map((file) => (
            <li key={file.id}>
              {file.previewUrl ? (
                <img
                  src={file.previewUrl}
                  alt=""
                  width={file.previewWidth ?? undefined}
                  height={file.previewHeight ?? undefined}
                  loading="lazy"
                />
              ) : file.previewStatus === PreviewStatus.PENDING ||
                file.previewStatus === PreviewStatus.PROCESSING ? (
                file.contentType.startsWith('image/') ? (
                  <span
                    style={{
                      display: 'inline-block',
                      width: '4em',
                      height: '4em',
                      border: '1px solid #ccc',
                      background: '#f5f5f5',
                    }}
                  >
                    Processing…
                  </span>
                ) : (
                  <span>Indexing…</span>
                )
              ) : file.previewStatus === PreviewStatus.FAILED ? (
                <span>Preview failed</span>
              ) : null}{' '}
              <span>{basename(file.key)}</span>{' '}
              <button type="button" onClick={() => void handleDownload(file.id)}>
                Download
              </button>{' '}
              <button type="button" onClick={() => setMetadataId(file.id)}>
                Metadata
              </button>{' '}
              <button
                type="button"
                onClick={() => deleteFileMutation.mutate(file.id)}
                disabled={deleteFileMutation.isPending}
              >
                Delete
              </button>
            </li>
          ))}
        </ul>
      )}
      <div ref={sentinelRef} data-testid="scroll-sentinel" />
      {isFetchingNextPage ? <p>Loading more…</p> : null}

      {folderToDelete !== null ? (
        <div role="alertdialog" aria-label="Confirm delete folder">
          <p>
            Delete <strong>{folderToDelete}</strong>?
          </p>
          {statsQuery.isPending ? (
            <p>Checking contents…</p>
          ) : statsQuery.isError ? (
            <p role="alert">Failed to load folder contents: {String(statsQuery.error)}</p>
          ) : (
            <p>
              This deletes {statsQuery.data.fileCount} file
              {statsQuery.data.fileCount === 1 ? '' : 's'} (
              {formatBytes(statsQuery.data.totalBytes)}). This cannot be undone, and is not atomic:
              on a partial failure some files may already be gone while others remain.
            </p>
          )}
          {deleteDirMutation.isError ? (
            <p role="alert">Delete failed: {String(deleteDirMutation.error)}</p>
          ) : null}
          <button
            type="button"
            onClick={() => deleteDirMutation.mutate(folderToDelete)}
            disabled={statsQuery.isPending || deleteDirMutation.isPending}
          >
            {deleteDirMutation.isPending ? 'Deleting…' : 'Confirm delete'}
          </button>{' '}
          <button type="button" onClick={() => setFolderToDelete(null)}>
            Cancel
          </button>
        </div>
      ) : null}
      <FileMetadataModal
        fileId={metadataId}
        onClose={() => setMetadataId(null)}
        getFileMetadata={getFileMetadata}
      />
    </div>
  )
}
