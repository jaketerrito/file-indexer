import { type QueryKey, useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { deleteFile, getDownloadUrl, getOpenUrl } from '../server/files'
import type { FileDto } from '../server/impl'
import { DeleteFileConfirmation } from './DeleteFileConfirmation'
import { formatBytes } from './FileMetadataTable'

/** A subdirectory row of the table (browse mode only). */
export interface FolderRow {
  /** Full path from the bucket root, ending in "/". */
  path: string
  /** Display name (basename without the trailing slash). */
  name: string
}

interface FileTableProps {
  files: FileDto[]
  /** Opens a file's standalone page. */
  onOpenFile: (id: string) => void
  /**
   * Display transform for a file's key, applied to the Key column and the
   * delete confirmation. Browse mode passes a basename helper; search mode
   * shows full keys (the default).
   */
  displayKey?: (key: string) => string
  /**
   * Query keys invalidated after a delete succeeds — each caller owns its
   * list query (and, in browse mode, the sidebar tree, since deleting a
   * folder's last file removes the folder).
   */
  invalidateKeys: QueryKey[]
  /**
   * Subdirectory rows, rendered before the files (always alphabetical,
   * always first — ListDirectory's two-phase design). Browse mode only.
   */
  folders?: FolderRow[]
  /** Navigates into a folder row. Required when folders is set. */
  onNavigateFolder?: (path: string) => void
  /** Selects a folder for the delete confirmation (owned by the caller). */
  onDeleteFolder?: (path: string) => void
}

/**
 * The one table of a listing's rows, shared by search mode (FileList) and
 * browse mode (DirectoryList): preview thumbnail (or index-status
 * placeholder), key, type, size, created, and the Open/Download/Metadata/Delete
 * actions. Browse mode additionally passes subdirectory rows (folders prop),
 * which sort before the files. File delete is a two-step confirm owned here
 * so both modes behave identically; only cache invalidation differs, via
 * invalidateKeys. Folder delete stays with the caller — it needs stats and a
 * different dialog.
 */
export function FileTable({
  files,
  onOpenFile,
  displayKey = (key) => key,
  invalidateKeys,
  folders,
  onNavigateFolder,
  onDeleteFolder,
}: FileTableProps) {
  const queryClient = useQueryClient()

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteFile({ data: { id } }),
    onSuccess: () => {
      for (const queryKey of invalidateKeys) {
        queryClient.invalidateQueries({ queryKey })
      }
      setFileToDelete(null)
    },
  })

  // File pending delete confirmation, or null when none. The Delete button
  // only selects; the dialog's Confirm delete actually runs the mutation.
  const [fileToDelete, setFileToDelete] = useState<{ id: string; key: string } | null>(null)

  async function handleDownload(id: string) {
    const { url } = await getDownloadUrl({ data: { id } })
    window.open(url, '_blank', 'noopener')
  }

  async function handleOpen(id: string) {
    const { url } = await getOpenUrl({ data: { id } })
    window.open(url, '_blank', 'noopener')
  }

  return (
    <>
      <table>
        <thead>
          <tr>
            <th>Preview</th>
            <th>Key</th>
            <th>Type</th>
            <th>Size</th>
            <th>Created</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {folders?.map((folder) => (
            <tr key={folder.path}>
              <td>📁</td>
              <td>
                <button type="button" onClick={() => onNavigateFolder?.(folder.path)}>
                  {folder.name}
                </button>
              </td>
              <td>Folder</td>
              <td />
              <td />
              <td>
                <button type="button" onClick={() => onDeleteFolder?.(folder.path)}>
                  Delete folder
                </button>
              </td>
            </tr>
          ))}
          {files.map((file) => (
            <tr key={file.id}>
              <td>
                {file.previewUrl ? (
                  <img
                    src={file.previewUrl}
                    alt=""
                    width={file.previewWidth ?? undefined}
                    height={file.previewHeight ?? undefined}
                    loading="lazy"
                    // Cap the rendered size: the width/height attributes
                    // reserve layout space at intrinsic preview size, which
                    // dwarfs a table row.
                    style={{ maxWidth: '4em', maxHeight: '4em', width: 'auto', height: 'auto' }}
                  />
                ) : file.previewStatus === PreviewStatus.PENDING ||
                  file.previewStatus === PreviewStatus.PROCESSING ? (
                  file.contentType.startsWith('image/') ? (
                    <span
                      style={{
                        display: 'inline-block',
                        width: '4em',
                        height: '4em',
                        border: '1px solid #ccc',
                        background: '#f5f5f5',
                      }}
                    >
                      Processing…
                    </span>
                  ) : (
                    <span>Indexing…</span>
                  )
                ) : file.previewStatus === PreviewStatus.FAILED ? (
                  <span>Preview failed</span>
                ) : null}
              </td>
              <td>{displayKey(file.key)}</td>
              <td>{file.contentType}</td>
              <td>{formatBytes(file.sizeBytes)}</td>
              <td>{file.createdAt}</td>
              <td>
                <button type="button" onClick={() => void handleOpen(file.id)}>
                  Open
                </button>{' '}
                <button type="button" onClick={() => void handleDownload(file.id)}>
                  Download
                </button>{' '}
                <button type="button" onClick={() => onOpenFile(file.id)}>
                  Metadata
                </button>{' '}
                <button
                  type="button"
                  onClick={() => setFileToDelete({ id: file.id, key: displayKey(file.key) })}
                  disabled={deleteMutation.isPending}
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {fileToDelete !== null ? (
        <DeleteFileConfirmation
          fileKey={fileToDelete.key}
          pending={deleteMutation.isPending}
          error={deleteMutation.isError ? deleteMutation.error : null}
          onConfirm={() => deleteMutation.mutate(fileToDelete.id)}
          onCancel={() => setFileToDelete(null)}
        />
      ) : null}
    </>
  )
}
