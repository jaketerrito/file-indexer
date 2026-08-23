import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import type { FileFilters } from '../lib/fileFilters'
import { normalizeUploadPath } from '../lib/uploadPath'
import {
  commitUpload,
  deleteFile,
  getDownloadUrl,
  getFileMetadata,
  getUploadUrl,
  listFiles,
} from '../server/files'
import { FileMetadataModal } from './FileMetadataModal'

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

  // Destination for uploads, independent of the search prefix above — typing
  // a search term must never change where a file lands. Freeform text;
  // normalized (trailing slash added, doubled slashes collapsed) before
  // being prepended to each filename.
  const [uploadPath, setUploadPath] = useState('')

  // Uploads one file at a time (sequential, not parallel) so a single failure
  // in a multi-file selection doesn't leave concurrent requests in flight.
  // Each file: presigned PUT straight to S3, then CommitUpload registers the
  // reference (key only, no bytes) so the indexer seed picks it up.
  const uploadMutation = useMutation({
    mutationFn: async (files: File[]) => {
      const dir = normalizeUploadPath(uploadPath)
      for (const file of files) {
        const key = dir + file.name
        const { url } = await getUploadUrl({ data: { key } })
        const res = await fetch(url, {
          method: 'PUT',
          body: file,
          headers: { 'Content-Type': file.type || 'application/octet-stream' },
        })
        if (!res.ok) {
          throw new Error(`upload failed for ${file.name}: ${res.status}`)
        }
        await commitUpload({ data: { key } })
      }
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['files'] }),
  })

  const uploadInputRef = useRef<HTMLInputElement>(null)
  function handleUploadChange(e: React.ChangeEvent<HTMLInputElement>) {
    const files = e.target.files
    if (files && files.length > 0) {
      uploadMutation.mutate(Array.from(files))
    }
    // Reset so selecting the same file(s) again still fires onChange.
    e.target.value = ''
  }

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

  const [metadataId, setMetadataId] = useState<string | null>(null)

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
        </button>{' '}
        <label>
          Upload to{' '}
          <input
            type="text"
            placeholder="folder/subfolder/ (optional)"
            value={uploadPath}
            onChange={(e) => setUploadPath(e.target.value)}
          />
        </label>{' '}
        <input
          ref={uploadInputRef}
          type="file"
          multiple
          aria-label="Upload files"
          onChange={handleUploadChange}
          style={{ display: 'none' }}
        />
        <button
          type="button"
          onClick={() => uploadInputRef.current?.click()}
          disabled={uploadMutation.isPending}
        >
          {uploadMutation.isPending ? 'Uploading…' : 'Upload'}
        </button>
      </fieldset>
      {uploadMutation.isError ? (
        <p role="alert">Upload failed: {String(uploadMutation.error)}</p>
      ) : null}

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
                    ) : null}{' '}
                    <span>{file.key}</span>{' '}
                    <button type="button" onClick={() => void handleDownload(file.id)}>
                      Download
                    </button>{' '}
                    <button type="button" onClick={() => setMetadataId(file.id)}>
                      Metadata
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
      <FileMetadataModal
        fileId={metadataId}
        onClose={() => setMetadataId(null)}
        getFileMetadata={getFileMetadata}
      />
    </div>
  )
}
