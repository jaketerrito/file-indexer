import { useState } from 'react'
import { FolderTree } from './FolderTree'

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
 * Destination picker for moving a file into another folder. Folders are
 * virtual (derived from key structure), so "New folder" only navigates to a
 * path that may not exist yet.
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
  const [selectedPath, setSelectedPath] = useState(currentFolder)
  const [newFolderName, setNewFolderName] = useState('')
  const [newFolderError, setNewFolderError] = useState<string | null>(null)

  const destinationKey = selectedPath + fileName
  const unchanged = destinationKey === fileKey

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
    const prefix =
      selectedPath === '' ? '' : selectedPath.endsWith('/') ? selectedPath : `${selectedPath}/`
    setSelectedPath(`${prefix}${name}/`)
    setNewFolderName('')
    setNewFolderError(null)
  }

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
      <div style={{ background: 'white', padding: '1rem', minWidth: '24rem', maxWidth: '32rem' }}>
        <p>
          Move <strong>{fileName}</strong>
        </p>
        <div
          style={{
            maxHeight: '16rem',
            overflow: 'auto',
            border: '1px solid #ccc',
            padding: '0.5rem',
            marginBottom: '0.5rem',
          }}
        >
          <FolderTree currentPath={selectedPath} onNavigate={setSelectedPath} />
        </div>
        <form onSubmit={handleNewFolderSubmit} style={{ marginBottom: '0.5rem' }}>
          <label>
            New folder{' '}
            <input
              type="text"
              value={newFolderName}
              onChange={(e) => {
                setNewFolderName(e.target.value)
                setNewFolderError(null)
              }}
              placeholder="folder name"
              aria-invalid={newFolderError ? 'true' : undefined}
            />
          </label>{' '}
          <button type="submit">Create</button>
          {newFolderError ? <p role="alert">{newFolderError}</p> : null}
        </form>
        <p>
          Destination: <code>{destinationKey}</code>
        </p>
        {error ? <p role="alert">Move failed: {String(error)}</p> : null}
        <button
          type="button"
          onClick={() => onConfirm(selectedPath)}
          disabled={moving || unchanged}
        >
          {moving ? 'Moving…' : 'Move here'}
        </button>{' '}
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  )
}
