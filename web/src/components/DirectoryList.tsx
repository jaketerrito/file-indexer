import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useFileStatusPoller } from '../lib/useFileStatusPoller'
import { deleteDirectory, getDirectoryStats, listDirectory } from '../server/files'
import type { SortFieldInput, SortOrderInput } from '../server/impl'
import { DeleteFolderConfirmation } from './DeleteFolderConfirmation'
import { FileTable } from './FileTable'

const PAGE_SIZE = 50

interface DirectoryListProps {
  /** Directory being browsed; "" is the bucket root. */
  path: string
  sort: SortFieldInput
  order: SortOrderInput
  onNavigate: (path: string) => void
  /** Opens a file's standalone page (what the metadata modal used to show). */
  onOpenFile: (id: string) => void
}

/**
 * Lists a directory's immediate children in the shared FileTable:
 * subdirectories as rows first (always alphabetical — see ListDirectory's
 * two-phase design), then files directly in it. Deleting a folder is a
 * two-step confirm: clicking Delete fetches GetDirectoryStats to show
 * what's about to go before a second click actually runs DeleteDirectory,
 * since it is a non-atomic, unrecoverable bulk operation.
 */
export function DirectoryList({ path, sort, order, onNavigate, onOpenFile }: DirectoryListProps) {
  const queryClient = useQueryClient()

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
      queryClient.invalidateQueries({ queryKey: ['folder-tree'] })
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

  if (isPending) return <p>Loading…</p>
  if (isError) return <p role="alert">Failed to load directory: {String(error)}</p>

  const directories = data.pages.flatMap((p) => p.directories)
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
        <FileTable
          folders={directories.map((dir) => ({ path: dir, name: dirName(dir) }))}
          files={files}
          onOpenFile={onOpenFile}
          onNavigateFolder={onNavigate}
          onDeleteFolder={setFolderToDelete}
          displayKey={basename}
          // Deleting a folder's last file removes the folder itself
          // (directories are derived from key structure), so the sidebar
          // tree needs a refetch too.
          invalidateKeys={[['directory', path], ['folder-tree']]}
        />
      )}
      <div ref={sentinelRef} data-testid="scroll-sentinel" />
      {isFetchingNextPage ? <p>Loading more…</p> : null}

      {folderToDelete !== null ? (
        <DeleteFolderConfirmation
          folderPath={folderToDelete}
          statsPending={statsQuery.isPending}
          statsError={statsQuery.isError ? statsQuery.error : null}
          stats={statsQuery.data ?? null}
          deletePending={deleteDirMutation.isPending}
          deleteError={deleteDirMutation.isError ? deleteDirMutation.error : null}
          onConfirm={() => deleteDirMutation.mutate(folderToDelete)}
          onCancel={() => setFolderToDelete(null)}
        />
      ) : null}
    </div>
  )
}
