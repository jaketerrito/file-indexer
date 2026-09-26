import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import type { BrowseFilters } from '../lib/fileFilters'
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
  filters: BrowseFilters
  onFiltersChange: (filters: BrowseFilters) => void
  onOpenFile: (id: string) => void
}

/**
 * Browse body of "/": breadcrumb path on the left, New folder / Upload on
 * the right, then the DirectoryList. Directories are virtual: "New folder"
 * just navigates to an empty path; it becomes real once a key lands there.
 */
export function Browser({ filters, onFiltersChange, onOpenFile }: BrowserProps) {
  const queryClient = useQueryClient()
  const path = filters.path

  const uploadInputRef = useRef<HTMLInputElement>(null)
  const [uploadStatus, setUploadStatus] = useState<string | null>(null)
  const cancelControllerRef = useRef(new AbortController())
  const uploadMutation = useMutation({
    mutationFn: async (files: File[]) => {
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
    onFiltersChange({ ...filters, path: next })
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

  return (
    <div>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          gap: '1rem',
          marginBottom: '0.75rem',
        }}
      >
        <Breadcrumbs path={path} onNavigate={handleNavigate} />
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center' }}>
          <button type="button" onClick={handleNewFolder}>
            New folder
          </button>
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
              <span>{uploadStatus}</span>
              <button type="button" onClick={() => cancelControllerRef.current.abort()}>
                Cancel
              </button>
            </>
          ) : null}
        </div>
      </div>
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
