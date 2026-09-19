import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { type FileFilters, isBrowsing, toBrowsePath } from '../lib/fileFilters'
import {
  MULTIPART_THRESHOLD_BYTES,
  type MultipartUploadDeps,
  uploadFileMultipart,
} from '../lib/multipartUpload'
import { normalizeUploadPath } from '../lib/uploadPath'
import { visuallyHiddenStyle } from '../lib/visuallyHidden'
import {
  abortMultipartUpload,
  commitUpload,
  completeMultipartUpload,
  createMultipartUpload,
  getUploadPartUrl,
  getUploadUrl,
  listUploadedParts,
} from '../server/files'
import { Breadcrumbs } from './Breadcrumbs'
import { DirectoryList } from './DirectoryList'
import { FileList } from './FileList'

// Bridges the plain-object multipartUpload module to the TanStack Start
// server functions, which wrap every input in { data }. Module-level: none
// of this depends on component props or state.
const multipartDeps: MultipartUploadDeps = {
  createMultipartUpload: (input) => createMultipartUpload({ data: input }),
  getUploadPartUrl: (input) => getUploadPartUrl({ data: input }),
  listUploadedParts: (input) => listUploadedParts({ data: input }),
  completeMultipartUpload: async (input) => {
    await completeMultipartUpload({ data: input })
  },
  abortMultipartUpload: async (input) => {
    await abortMultipartUpload({ data: input })
  },
  putPart: async (url, body, signal) => {
    const res = await fetch(url, { method: 'PUT', body, signal })
    if (!res.ok) {
      throw new Error(`part upload failed: ${res.status}`)
    }
  },
}

interface BrowserProps {
  filters: FileFilters
  onFiltersChange: (filters: FileFilters) => void
  /** Opens a file's standalone page; threaded through to both lists. */
  onOpenFile: (id: string) => void
}

/**
 * Top-level switch between the two ways of finding a file:
 * search (flat, recursive, filterable — FileList, unchanged from before
 * directory browsing existed) and browse (breadcrumb navigation over
 * directories derived purely from key structure — DirectoryList). Browse
 * mode is entered from the sidebar FolderTree; search mode is the default,
 * re-entered via the top bar's app link or global search: folders are for
 * navigation, filtering is search's job.
 *
 * Directories are virtual: there is no CreateDirectory call. "New folder"
 * just navigates to a path nothing lives under yet; DirectoryList shows it
 * as empty and ready to upload into, and it only becomes real (reachable by
 * ListChildDirectories) once a key actually lands there.
 */
export function Browser({ filters, onFiltersChange, onOpenFile }: BrowserProps) {
  const queryClient = useQueryClient()
  const browsing = isBrowsing(filters)
  const path = filters.path ?? ''

  const uploadInputRef = useRef<HTMLInputElement>(null)
  const [uploadStatus, setUploadStatus] = useState<string | null>(null)
  // Mutable, not state: read inside the async mutation loop between parts,
  // not something a re-render needs to observe.
  const cancelControllerRef = useRef(new AbortController())
  const uploadMutation = useMutation({
    mutationFn: async (files: File[]) => {
      // Always the current folder — no freeform override here (unlike
      // FileList's search-mode upload, which has no path to default to).
      // Browsing to the right folder first is the destination picker.
      const dir = normalizeUploadPath(path)
      cancelControllerRef.current = new AbortController()
      try {
        for (const file of files) {
          const key = dir + file.name
          if (file.size >= MULTIPART_THRESHOLD_BYTES) {
            setUploadStatus(`Uploading ${file.name}…`)
            await uploadFileMultipart(
              file,
              key,
              file.type || 'application/octet-stream',
              multipartDeps,
              {
                signal: cancelControllerRef.current.signal,
                onProgress: (completed, total) =>
                  setUploadStatus(`Uploading ${file.name}: part ${completed}/${total}`),
              },
            )
          } else {
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
        }
      } finally {
        setUploadStatus(null)
      }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['directory'] })
      // Uploads create folders the sidebar tree hasn't fetched yet.
      queryClient.invalidateQueries({ queryKey: ['folder-tree'] })
    },
  })

  function handleUploadChange(e: React.ChangeEvent<HTMLInputElement>) {
    const files = e.target.files
    if (files && files.length > 0) {
      uploadMutation.mutate(Array.from(files))
    }
    e.target.value = ''
  }

  function handleNavigate(next: string) {
    onFiltersChange(toBrowsePath(filters, next))
  }

  function handleNewFolder() {
    const name = window.prompt('New folder name')
    if (!name) return
    if (name.includes('/')) {
      window.alert('Folder name cannot contain "/"')
      return
    }
    handleNavigate(`${path}${name}/`)
  }

  if (!browsing) {
    return <FileList filters={filters} onFiltersChange={onFiltersChange} onOpenFile={onOpenFile} />
  }

  return (
    <div>
      <Breadcrumbs path={path} onNavigate={handleNavigate} />{' '}
      <button type="button" onClick={handleNewFolder}>
        New folder
      </button>
      <fieldset>
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
        <input
          ref={uploadInputRef}
          type="file"
          multiple
          aria-label="Upload files"
          onChange={handleUploadChange}
          style={visuallyHiddenStyle}
        />
        <button
          type="button"
          onClick={() => uploadInputRef.current?.click()}
          disabled={uploadMutation.isPending}
        >
          {uploadMutation.isPending ? 'Uploading…' : 'Upload'}
        </button>
        {uploadStatus ? (
          <>
            {' '}
            <span>{uploadStatus}</span>{' '}
            <button type="button" onClick={() => cancelControllerRef.current.abort()}>
              Cancel
            </button>
          </>
        ) : null}
      </fieldset>
      {uploadMutation.isError ? (
        <p role="alert">Upload failed: {String(uploadMutation.error)}</p>
      ) : null}
      <DirectoryList
        path={path}
        sort={filters.sort}
        order={filters.order}
        onNavigate={handleNavigate}
        onOpenFile={onOpenFile}
      />
    </div>
  )
}
