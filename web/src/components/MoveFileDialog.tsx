import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { listDirectory } from '../server/files'

/**
 * Fetches every immediate child directory of `path`. Mirrors FolderTree's
 * query pattern so the dialog shares its cache keys and loading behaviour.
 */
async function fetchChildDirectories(path: string): Promise<string[]> {
  const directories: string[] = []
  let pageToken: string | undefined
  const maxPages = 100
  for (let i = 0; i < maxPages; i++) {
    const page = await listDirectory({
      data: { path, pageToken, sortField: 'key', sortOrder: 'asc' },
    })
    directories.push(...page.directories)
    if (page.nextPageToken === '' || page.nextPageToken == null) break
    pageToken = page.nextPageToken
  }
  return directories
}

interface MoveFileDialogProps {
  /** Full S3 key of the file being moved. */
  fileKey: string
  /** True while the move mutation is in flight. */
  moving: boolean
  /** Error from the last failed move attempt, or null. */
  error: unknown
  /** Called with the selected destination directory path (trailing slash). */
  onConfirm: (destinationPath: string) => void
  onCancel: () => void
}

/**
 * Destination picker for moving a file into another folder. Shows a small
 * folder browser: a breadcrumb path bar with an add-folder button, and a flat
 * list of the current directory's subdirectories. Folders are virtual
 * (derived from key structure), so "Add folder" only navigates to a path that
 * may not exist yet.
 */
export function MoveFileDialog({
  fileKey,
  moving,
  error,
  onConfirm,
  onCancel,
}: MoveFileDialogProps) {
  const fileName = fileKey.slice(fileKey.lastIndexOf('/') + 1)
  const currentFolder = fileKey.slice(0, fileKey.lastIndexOf('/') + 1)
  const [viewPath, setViewPath] = useState(currentFolder)
  const [showAddFolder, setShowAddFolder] = useState(false)
  const [newFolderName, setNewFolderName] = useState('')
  const [newFolderError, setNewFolderError] = useState<string | null>(null)

  const {
    data: subdirs,
    isPending,
    isError,
    error: listError,
  } = useQuery({
    queryKey: ['folder-tree', viewPath],
    queryFn: () => fetchChildDirectories(viewPath),
    staleTime: 30_000,
  })

  const destinationKey = viewPath + fileName
  const unchanged = destinationKey === fileKey

  function navigateTo(path: string) {
    setViewPath(path)
    setShowAddFolder(false)
    setNewFolderName('')
    setNewFolderError(null)
  }

  function handleNewFolderSubmit(e: React.FormEvent) {
    e.preventDefault()
    const name = newFolderName.trim()
    if (name === '') {
      setNewFolderError('Folder name cannot be empty')
      return
    }
    if (name === '.' || name === '..') {
      setNewFolderError('Folder name cannot be "." or ".."')
      return
    }
    if (name.includes('/')) {
      setNewFolderError('Folder name cannot contain "/"')
      return
    }
    const prefix = viewPath === '' ? '' : viewPath.endsWith('/') ? viewPath : `${viewPath}/`
    navigateTo(`${prefix}${name}/`)
  }

  function cancelAddFolder() {
    setShowAddFolder(false)
    setNewFolderName('')
    setNewFolderError(null)
  }

  const segments = viewPath.replace(/\/$/, '').split('/').filter(Boolean)

  return (
    <div
      role="alertdialog"
      aria-modal="true"
      aria-label="Move file"
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0, 0, 0, 0.5)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onCancel()
      }}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onCancel()
      }}
    >
      <div
        style={{
          background: 'white',
          padding: '1rem',
          minWidth: '24rem',
          maxWidth: '32rem',
          width: '100%',
        }}
      >
        <p>
          Move <strong>{fileName}</strong>
        </p>
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            gap: '0.5rem',
            marginBottom: showAddFolder ? '0.5rem' : undefined,
          }}
        >
          <nav aria-label="Current path" style={{ minWidth: 0 }}>
            <button
              type="button"
              onClick={() => navigateTo('')}
              aria-current={viewPath === '' ? 'true' : undefined}
              style={viewPath === '' ? { fontWeight: 'bold' } : undefined}
            >
              Bucket root
            </button>
            {segments.map((segment, index) => {
              const target = `${segments.slice(0, index + 1).join('/')}/`
              const isCurrent = index === segments.length - 1
              return (
                <span key={target}>
                  {' / '}
                  {isCurrent ? (
                    <span aria-current="location">{segment}</span>
                  ) : (
                    <button type="button" onClick={() => navigateTo(target)}>
                      {segment}
                    </button>
                  )}
                </span>
              )
            })}
          </nav>
          {!showAddFolder ? (
            <button
              type="button"
              onClick={() => setShowAddFolder(true)}
              aria-expanded="false"
              aria-controls="new-folder-form"
            >
              Add folder
            </button>
          ) : null}
        </div>
        {showAddFolder ? (
          <form
            id="new-folder-form"
            onSubmit={handleNewFolderSubmit}
            style={{ marginBottom: '0.5rem' }}
          >
            <input
              type="text"
              value={newFolderName}
              onChange={(e) => {
                setNewFolderName(e.target.value)
                setNewFolderError(null)
              }}
              placeholder="folder name"
              aria-label="New folder name"
              aria-invalid={newFolderError ? 'true' : undefined}
            />{' '}
            <button type="submit">Create</button>{' '}
            <button type="button" aria-label="Cancel new folder" onClick={cancelAddFolder}>
              Cancel
            </button>
            {newFolderError ? (
              <p role="alert" style={{ margin: '0.25rem 0 0' }}>
                {newFolderError}
              </p>
            ) : null}
          </form>
        ) : null}
        <div
          style={{
            maxHeight: '16rem',
            overflow: 'auto',
            border: '1px solid #ccc',
            padding: '0.5rem',
            marginBottom: '0.5rem',
          }}
        >
          {isPending ? <p>Loading…</p> : null}
          {isError ? <p role="alert">Failed to load folders: {String(listError)}</p> : null}
          {!isPending && !isError && subdirs?.length === 0 ? <p>No subfolders</p> : null}
          {!isPending && !isError && subdirs && subdirs.length > 0 ? (
            <ul aria-label="Subfolders" style={{ listStyle: 'none', margin: 0, padding: 0 }}>
              {subdirs.map((dir) => {
                const name = dir.replace(/\/$/, '').split('/').pop() as string
                return (
                  <li key={dir}>
                    <button type="button" onClick={() => navigateTo(dir)}>
                      {name}
                    </button>
                  </li>
                )
              })}
            </ul>
          ) : null}
        </div>
        <p>
          Destination: <code>{destinationKey}</code>
        </p>
        {error ? <p role="alert">Move failed: {String(error)}</p> : null}
        <button type="button" onClick={() => onConfirm(viewPath)} disabled={moving || unchanged}>
          {moving ? 'Moving…' : 'Move here'}
        </button>{' '}
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  )
}
