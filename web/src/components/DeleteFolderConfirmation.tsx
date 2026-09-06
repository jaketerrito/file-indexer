import type { DirectoryStatsDto } from '../server/impl'
import { formatBytes } from './FileMetadataTable'

interface DeleteFolderConfirmationProps {
  /** Full path of the folder pending deletion. */
  folderPath: string
  /** True while the folder-contents stats fetch is in flight. */
  statsPending: boolean
  /** Error from a failed stats fetch, or null. */
  statsError: unknown
  /** Folder contents summary, or null until the stats fetch resolves. */
  stats: DirectoryStatsDto | null
  /** True while the delete mutation is in flight. */
  deletePending: boolean
  /** Error from the last failed delete attempt, or null. */
  deleteError: unknown
  onConfirm: () => void
  onCancel: () => void
}

/**
 * Two-step confirm for folder deletion. Shows the folder's contents summary
 * (fetched by the caller once a folder is selected) so the user sees what's
 * about to go — DeleteDirectory has no dry-run flag by design; this is the
 * client-side substitute. Rendered as a centered overlay (see
 * DeleteFileConfirmation for the pattern): an in-flow dialog renders below
 * the fold and the Delete folder button would appear to do nothing.
 */
export function DeleteFolderConfirmation({
  folderPath,
  statsPending,
  statsError,
  stats,
  deletePending,
  deleteError,
  onConfirm,
  onCancel,
}: DeleteFolderConfirmationProps) {
  return (
    <div
      role="alertdialog"
      aria-modal="true"
      aria-label="Confirm delete folder"
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
          Delete <strong>{folderPath}</strong>?
        </p>
        {statsPending ? (
          <p>Checking contents…</p>
        ) : statsError ? (
          <p role="alert">Failed to load folder contents: {String(statsError)}</p>
        ) : stats !== null ? (
          <p>
            This deletes {stats.fileCount} file
            {stats.fileCount === 1 ? '' : 's'} ({formatBytes(stats.totalBytes)}). This cannot be
            undone, and is not atomic: on a partial failure some files may already be gone while
            others remain.
          </p>
        ) : null}
        {deleteError ? <p role="alert">Delete failed: {String(deleteError)}</p> : null}
        <button type="button" onClick={onConfirm} disabled={statsPending || deletePending}>
          {deletePending ? 'Deleting…' : 'Confirm delete'}
        </button>{' '}
        <button type="button" onClick={onCancel}>
          Cancel
        </button>
      </div>
    </div>
  )
}
