interface DeleteFileConfirmationProps {
  /** Display key of the file pending deletion. */
  fileKey: string
  /** True while the delete mutation is in flight. */
  pending: boolean
  /** Error from the last failed delete attempt, or null. */
  error: unknown
  onConfirm: () => void
  onCancel: () => void
}

/**
 * Two-step confirm for single-file deletion, mirroring the delete-folder
 * dialog in DirectoryList.tsx but without a stats fetch: deleting one file
 * needs no GetDirectoryStats call. Rendered as a centered overlay appended
 * after the list: an in-flow dialog renders below the fold and the Delete
 * button would appear to do nothing.
 */
export function DeleteFileConfirmation({
  fileKey,
  pending,
  error,
  onConfirm,
  onCancel,
}: DeleteFileConfirmationProps) {
  return (
    <div
      role="alertdialog"
      aria-modal="true"
      aria-label="Confirm delete file"
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
      <div style={{ background: 'white', padding: '1rem', minWidth: '24rem' }}>
        <p>
          Delete <strong>{fileKey}</strong>?
        </p>
        <p>This cannot be undone.</p>
        {error ? <p role="alert">Delete failed: {String(error)}</p> : null}
        <button type="button" onClick={onConfirm} disabled={pending}>
          {pending ? 'Deleting…' : 'Confirm delete'}
        </button>{' '}
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  )
}
