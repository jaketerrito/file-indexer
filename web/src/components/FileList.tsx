import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import type { SearchFilters } from '../lib/fileFilters'
import { useFileStatusPoller } from '../lib/useFileStatusPoller'
import { listContentTypes, listFiles } from '../server/files'
import { FileTable } from './FileTable'

const PAGE_SIZE = 50

/** Human label for a MIME category value: "image/" → "Image". */
function categoryLabel(category: string): string {
  const base = category.endsWith('/') ? category.slice(0, -1) : category
  return base.charAt(0).toUpperCase() + base.slice(1)
}

interface FileListProps {
  filters: SearchFilters
  onFiltersChange: (filters: SearchFilters) => void
  /** Opens a file's standalone page (what the metadata modal used to show). */
  onOpenFile: (id: string) => void
}

export function FileList({ filters, onFiltersChange, onOpenFile }: FileListProps) {
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
            query: filters.query,
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

  useFileStatusPoller(data?.pages, ['files', filters])

  const isFiltered = filters.query !== '' || filters.type !== ''
  const files = data?.pages.flatMap((page) => page.files) ?? []

  return (
    <div>
      <fieldset>
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
              onFiltersChange({ ...filters, sort: e.target.value as SearchFilters['sort'] })
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
          {files.length === 0 ? (
            <p>{isFiltered ? 'No files match your filters.' : 'No files.'}</p>
          ) : (
            <FileTable files={files} onOpenFile={onOpenFile} invalidateKeys={[['files']]} />
          )}
          <div ref={sentinelRef} data-testid="scroll-sentinel" />
          {isFetchingNextPage ? <p>Loading more…</p> : null}
        </>
      )}
    </div>
  )
}
