import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import type { FileFilters } from '../lib/fileFilters'
import { deleteFile, getDownloadUrl, listFiles } from '../server/files'

const PAGE_SIZE = 50
const PREFIX_DEBOUNCE_MS = 300

// TODO: populate these options from the backend (e.g. a SearchService RPC
// returning the distinct content-type categories that actually exist) instead
// of this hardcoded list.
const CONTENT_TYPE_OPTIONS = [
  { value: '', label: 'All types' },
  { value: 'image/', label: 'Images' },
  { value: 'video/', label: 'Video' },
  { value: 'audio/', label: 'Audio' },
  { value: 'text/', label: 'Text' },
  { value: 'application/', label: 'Application' },
]

interface FileListProps {
  filters: FileFilters
  onFiltersChange: (filters: FileFilters) => void
}

export function FileList({ filters, onFiltersChange }: FileListProps) {
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

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteFile({ data: { id } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['files'] }),
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
            {CONTENT_TYPE_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
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
        </>
      )}
    </div>
  )
}
