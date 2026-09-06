import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import type { FileFilters } from '../lib/fileFilters'
import { useFileStatusPoller } from '../lib/useFileStatusPoller'
import { deleteFile, getDownloadUrl, listContentTypes, listFiles } from '../server/files'
import { DeleteFileConfirmation } from './DeleteFileConfirmation'

const PAGE_SIZE = 50
const PREFIX_DEBOUNCE_MS = 300

/** Human label for a MIME category value: "image/" → "Image". */
function categoryLabel(category: string): string {
  const base = category.endsWith('/') ? category.slice(0, -1) : category
  return base.charAt(0).toUpperCase() + base.slice(1)
}

interface FileListProps {
  filters: FileFilters
  onFiltersChange: (filters: FileFilters) => void
  /** Opens a file's standalone page (what the metadata modal used to show). */
  onOpenFile: (id: string) => void
}

export function FileList({ filters, onFiltersChange, onOpenFile }: FileListProps) {
  const queryClient = useQueryClient()

  const { data, error, isPending, isError, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      // Filters are part of the key: changing them restarts pagination, which
      // the API requires (page tokens are bound to the params that issued them).
      queryKey: ['files', filters],
      queryFn: ({ pageParam }) =>
        listFiles({
          data: {
            pageSize: PAGE_SIZE,
            pageToken: pageParam,
            prefix: filters.prefix,
            contentType: filters.type,
            sortField: filters.sort,
            sortOrder: filters.order,
          },
        }),
      initialPageParam: '',
      getNextPageParam: (lastPage) => lastPage.nextPageToken || undefined,
    })

  // Categories that actually exist in the index, for the type dropdown.
  // Loading or failure degrades to just "All types": the list below has its
  // own error surface and the filter stays usable without options.
  const { data: contentTypes } = useQuery({
    queryKey: ['contentTypes'],
    queryFn: () => listContentTypes(),
  })

  const categories = contentTypes?.categories ?? []
  // Keep an active filter selectable even when the backend doesn't report
  // its category: a stale shared URL, or an exact MIME type like
  // "application/pdf", which the filter supports but categories never list.
  const optionValues =
    filters.type !== '' && !categories.includes(filters.type)
      ? [filters.type, ...categories]
      : categories

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteFile({ data: { id } }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['files'] })
      setFileToDelete(null)
    },
  })

  // The prefix input is local state, debounced into the URL-backed filters so
  // we don't fire a request (and a history replace) per keystroke.
  const [prefixInput, setPrefixInput] = useState(filters.prefix)
  useEffect(() => {
    setPrefixInput(filters.prefix)
  }, [filters.prefix])
  useEffect(() => {
    if (prefixInput === filters.prefix) return
    const timer = setTimeout(
      () => onFiltersChange({ ...filters, prefix: prefixInput }),
      PREFIX_DEBOUNCE_MS,
    )
    return () => clearTimeout(timer)
  }, [prefixInput, filters, onFiltersChange])

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

  // File pending delete confirmation, or null when none. The Delete button
  // only selects; the dialog's Confirm delete actually runs the mutation.
  const [fileToDelete, setFileToDelete] = useState<{ id: string; key: string } | null>(null)

  useFileStatusPoller(data?.pages, ['files', filters])

  const isFiltered = filters.prefix !== '' || filters.type !== ''

  return (
    <div>
      <fieldset>
        <label>
          Search{' '}
          <input
            type="search"
            placeholder="Filter by key prefix"
            value={prefixInput}
            onChange={(e) => setPrefixInput(e.target.value)}
          />
        </label>{' '}
        <label>
          Type{' '}
          <select
            value={filters.type}
            onChange={(e) => onFiltersChange({ ...filters, type: e.target.value })}
          >
            <option value="">All types</option>
            {optionValues.map((value) => (
              <option key={value} value={value}>
                {categoryLabel(value)}
              </option>
            ))}
          </select>
        </label>{' '}
        <label>
          Sort by{' '}
          <select
            value={filters.sort}
            onChange={(e) =>
              onFiltersChange({ ...filters, sort: e.target.value as FileFilters['sort'] })
            }
          >
            <option value="key">Key</option>
            <option value="lastModified">Modified</option>
            <option value="size">Size</option>
          </select>
        </label>{' '}
        <button
          type="button"
          onClick={() =>
            onFiltersChange({ ...filters, order: filters.order === 'asc' ? 'desc' : 'asc' })
          }
        >
          {filters.order === 'asc' ? 'Ascending' : 'Descending'}
        </button>
      </fieldset>

      {isPending ? (
        <p>Loading…</p>
      ) : isError ? (
        <p role="alert">Failed to load files: {String(error)}</p>
      ) : (
        <>
          {data.pages.flatMap((page) => page.files).length === 0 ? (
            <p>{isFiltered ? 'No files match your filters.' : 'No files.'}</p>
          ) : (
            <ul>
              {data.pages
                .flatMap((page) => page.files)
                .map((file) => (
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
                    <span>{file.key}</span>{' '}
                    <button type="button" onClick={() => void handleDownload(file.id)}>
                      Download
                    </button>{' '}
                    <button type="button" onClick={() => onOpenFile(file.id)}>
                      Metadata
                    </button>{' '}
                    <button
                      type="button"
                      onClick={() => setFileToDelete({ id: file.id, key: file.key })}
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
        </>
      )}
      {fileToDelete !== null ? (
        <DeleteFileConfirmation
          fileKey={fileToDelete.key}
          pending={deleteMutation.isPending}
          error={deleteMutation.isError ? deleteMutation.error : null}
          onConfirm={() => deleteMutation.mutate(fileToDelete.id)}
          onCancel={() => setFileToDelete(null)}
        />
      ) : null}
    </div>
  )
}
