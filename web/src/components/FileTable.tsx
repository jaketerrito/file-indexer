import { Code, ConnectError } from '@connectrpc/connect'
import { type QueryKey, useMutation, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { PreviewStatus } from '../gen/service/v1/files_pb'
import { getFileIcon } from '../lib/fileIcon'
import { formatDate } from '../lib/formatDate'
import { deleteFile, getDownloadUrl, moveFile } from '../server/files'
import type { FileDto } from '../server/impl'
import { DeleteFileConfirmation } from './DeleteFileConfirmation'
import { formatBytes } from './FileMetadataTable'
import { MoveFileDialog } from './MoveFileDialog'

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
 * The one table of a listing's rows, shared by search mode (FileList),
 * browse mode (DirectoryList), Recents, and Photos: preview thumbnail or
 * content-type icon, key, size, created, and a triple-dot menu for actions.
 * Only one action menu is open at a time; it closes on outside click or
 * Escape. Clicking a file row opens its page. Browse mode additionally
 * passes subdirectory rows, which sort before the files and have their own
 * triple-dot menu.
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

  const [moveError, setMoveError] = useState<string | null>(null)
  const moveMutation = useMutation({
    mutationFn: ({ id, destinationKey }: { id: string; destinationKey: string }) =>
      moveFile({ data: { id, destinationKey } }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['files'] })
      queryClient.invalidateQueries({ queryKey: ['directory'] })
      queryClient.invalidateQueries({ queryKey: ['folder-tree'] })
      setFileToMove(null)
      setMoveError(null)
    },
    onError: (err) => {
      const connectErr = ConnectError.from(err)
      // The server function transport strips the connect code (surfaces as
      // Unknown with the raw "[already_exists]" message), so match both.
      if (connectErr.code === Code.AlreadyExists || /already_exists/i.test(connectErr.message)) {
        setMoveError('A file with this name already exists there')
      } else {
        setMoveError(connectErr.message)
      }
    },
  })

  // File pending delete/move confirmation, or null when none. The row
  // buttons only select; the dialog's confirm button actually runs the
  // mutation.
  const [fileToDelete, setFileToDelete] = useState<{ id: string; key: string } | null>(null)
  const [fileToMove, setFileToMove] = useState<{ id: string; key: string } | null>(null)

  // ID of the file whose action menu is open.
  const [openFileMenuId, setOpenFileMenuId] = useState<string | null>(null)
  // Path of the folder whose action menu is open.
  const [openFolderMenuPath, setOpenFolderMenuPath] = useState<string | null>(null)
  const menuRef = useRef<HTMLDivElement>(null)

  const closeMenus = useCallback(() => {
    setOpenFileMenuId(null)
    setOpenFolderMenuPath(null)
  }, [])

  const anyMenuOpen = openFileMenuId !== null || openFolderMenuPath !== null
  useEffect(() => {
    if (!anyMenuOpen) return
    function handleClickOutside(event: MouseEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        closeMenus()
      }
    }
    function handleEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        closeMenus()
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    document.addEventListener('keydown', handleEscape)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
      document.removeEventListener('keydown', handleEscape)
    }
  }, [anyMenuOpen, closeMenus])

  async function handleDownload(id: string) {
    const { url } = await getDownloadUrl({ data: { id } })
    window.open(url, '_blank', 'noopener')
  }

  function toggleFileMenu(id: string, event: React.MouseEvent) {
    event.stopPropagation()
    setOpenFileMenuId((current) => (current === id ? null : id))
    setOpenFolderMenuPath(null)
  }

  function toggleFolderMenu(path: string, event: React.MouseEvent) {
    event.stopPropagation()
    setOpenFolderMenuPath((current) => (current === path ? null : path))
    setOpenFileMenuId(null)
  }

  const menuStyle: React.CSSProperties = {
    position: 'absolute',
    right: 0,
    top: '100%',
    background: 'white',
    border: '1px solid #ddd',
    boxShadow: '0 2px 6px rgba(0,0,0,0.15)',
    zIndex: 5,
    minWidth: '8rem',
  }

  const menuItemStyle: React.CSSProperties = {
    display: 'block',
    width: '100%',
    textAlign: 'left',
  }

  return (
    <>
      <table>
        <thead>
          <tr>
            <th>Preview</th>
            <th>Key</th>
            <th>Size</th>
            <th>Created</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {folders?.map((folder) => {
            const isMenuOpen = openFolderMenuPath === folder.path
            return (
              <tr key={folder.path}>
                <td>📁</td>
                <td>
                  <button type="button" onClick={() => onNavigateFolder?.(folder.path)}>
                    {folder.name}
                  </button>
                </td>
                <td />
                <td />
                <td>
                  <div ref={isMenuOpen ? menuRef : null} style={{ position: 'relative' }}>
                    <button
                      type="button"
                      aria-haspopup="menu"
                      aria-expanded={isMenuOpen}
                      aria-label="Folder actions"
                      onClick={(e) => toggleFolderMenu(folder.path, e)}
                    >
                      ⋯
                    </button>
                    {isMenuOpen ? (
                      <div role="menu" style={menuStyle}>
                        <button
                          type="button"
                          role="menuitem"
                          onClick={(e) => {
                            e.stopPropagation()
                            closeMenus()
                            onDeleteFolder?.(folder.path)
                          }}
                          style={menuItemStyle}
                        >
                          Delete folder
                        </button>
                      </div>
                    ) : null}
                  </div>
                </td>
              </tr>
            )
          })}
          {files.map((file) => {
            const isImage = file.contentType.startsWith('image/')
            const isMenuOpen = openFileMenuId === file.id
            return (
              <tr
                key={file.id}
                onClick={() => onOpenFile(file.id)}
                style={{ cursor: 'pointer' }}
                data-testid={`file-row-${file.id}`}
              >
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
                    isImage ? (
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
                  ) : isImage ? (
                    <span
                      style={{
                        display: 'inline-block',
                        width: '4em',
                        height: '4em',
                        border: '1px solid #ccc',
                        background: '#f5f5f5',
                      }}
                    >
                      No preview
                    </span>
                  ) : (
                    (() => {
                      const icon = getFileIcon(file.contentType)
                      return (
                        <span role="img" aria-label={icon.label}>
                          {icon.emoji}
                        </span>
                      )
                    })()
                  )}
                </td>
                <td>{displayKey(file.key)}</td>
                <td>{formatBytes(file.sizeBytes)}</td>
                <td>{formatDate(file.createdAt)}</td>
                <td>
                  <div ref={isMenuOpen ? menuRef : null} style={{ position: 'relative' }}>
                    <button
                      type="button"
                      aria-haspopup="menu"
                      aria-expanded={isMenuOpen}
                      aria-label="File actions"
                      onClick={(e) => toggleFileMenu(file.id, e)}
                    >
                      ⋯
                    </button>
                    {isMenuOpen ? (
                      <div role="menu" style={menuStyle}>
                        <button
                          type="button"
                          role="menuitem"
                          onClick={(e) => {
                            e.stopPropagation()
                            closeMenus()
                            void handleDownload(file.id)
                          }}
                          style={menuItemStyle}
                        >
                          Download
                        </button>
                        <button
                          type="button"
                          role="menuitem"
                          onClick={(e) => {
                            e.stopPropagation()
                            closeMenus()
                            setFileToMove({ id: file.id, key: file.key })
                          }}
                          style={menuItemStyle}
                        >
                          Move
                        </button>
                        <button
                          type="button"
                          role="menuitem"
                          onClick={(e) => {
                            e.stopPropagation()
                            closeMenus()
                            setFileToDelete({ id: file.id, key: displayKey(file.key) })
                          }}
                          style={menuItemStyle}
                        >
                          Delete
                        </button>
                      </div>
                    ) : null}
                  </div>
                </td>
              </tr>
            )
          })}
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
      {fileToMove !== null ? (
        <MoveFileDialog
          fileKey={fileToMove.key}
          moving={moveMutation.isPending}
          error={moveError}
          onConfirm={(destinationPath) => {
            const destinationKey =
              destinationPath + fileToMove.key.slice(fileToMove.key.lastIndexOf('/') + 1)
            moveMutation.mutate({ id: fileToMove.id, destinationKey })
          }}
          onCancel={() => {
            setFileToMove(null)
            setMoveError(null)
          }}
        />
      ) : null}
    </>
  )
}
