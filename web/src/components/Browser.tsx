import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useRef } from 'react'
import { type FileFilters, isBrowsing, toBrowsePath, toSearchFromPath } from '../lib/fileFilters'
import { normalizeUploadPath } from '../lib/uploadPath'
import { visuallyHiddenStyle } from '../lib/visuallyHidden'
import { commitUpload, getUploadUrl } from '../server/files'
import { Breadcrumbs } from './Breadcrumbs'
import { DirectoryList } from './DirectoryList'
import { FileList } from './FileList'

interface BrowserProps {
  filters: FileFilters
  onFiltersChange: (filters: FileFilters) => void
}

/**
 * Top-level switch between the two ways of finding a file (see NOTES.md):
 * search (flat, recursive, filterable — FileList, unchanged from before
 * directory browsing existed) and browse (breadcrumb navigation over
 * directories derived purely from key structure — DirectoryList). Choosing a
 * content-type filter, or clicking "Search all files", drops out of browse
 * mode into search seeded with the current path as a recursive prefix:
 * folders are for navigation, filtering is search's job.
 *
 * Directories are virtual: there is no CreateDirectory call. "New folder"
 * just navigates to a path nothing lives under yet; DirectoryList shows it
 * as empty and ready to upload into, and it only becomes real (reachable by
 * ListChildDirectories) once a key actually lands there.
 */
export function Browser({ filters, onFiltersChange }: BrowserProps) {
  const queryClient = useQueryClient()
  const browsing = isBrowsing(filters)
  const path = filters.path ?? ''

  const uploadInputRef = useRef<HTMLInputElement>(null)
  const uploadMutation = useMutation({
    mutationFn: async (files: File[]) => {
      // Always the current folder — no freeform override here (unlike
      // FileList's search-mode upload, which has no path to default to).
      // Browsing to the right folder first is the destination picker.
      const dir = normalizeUploadPath(path)
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
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['directory'] }),
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
    return (
      <div>
        <p>
          <button type="button" onClick={() => handleNavigate('')}>
            Browse folders
          </button>
        </p>
        <FileList filters={filters} onFiltersChange={onFiltersChange} />
      </div>
    )
  }

  return (
    <div>
      <Breadcrumbs path={path} onNavigate={handleNavigate} />{' '}
      <button type="button" onClick={() => onFiltersChange(toSearchFromPath(filters, ''))}>
        Search all files
      </button>{' '}
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
      </fieldset>
      {uploadMutation.isError ? (
        <p role="alert">Upload failed: {String(uploadMutation.error)}</p>
      ) : null}
      <DirectoryList
        path={path}
        sort={filters.sort}
        order={filters.order}
        onNavigate={handleNavigate}
      />
    </div>
  )
}
