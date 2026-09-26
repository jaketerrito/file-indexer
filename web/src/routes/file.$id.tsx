import { Code, ConnectError } from '@connectrpc/connect'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { DeleteFileConfirmation } from '../components/DeleteFileConfirmation'
import { FileMetadataTable } from '../components/FileMetadataTable'
import { MoveFileDialog } from '../components/MoveFileDialog'
import { DEFAULT_BROWSE_FILTERS } from '../lib/fileFilters'
import { deleteFile, getDownloadUrl, getFileMetadata, moveFile } from '../server/files'

export const Route = createFileRoute('/file/$id')({
  component: FilePage,
})

/**
 * Standalone file page (destination of the header search results and the
 * lists' Metadata buttons): full metadata plus the per-file actions —
 * download, delete (two-step confirm), and a link back into browse mode at
 * the parent folder.
 */
function FilePage() {
  const { id } = Route.useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { data, isPending, isError, error } = useQuery({
    queryKey: ['file-metadata', id],
    queryFn: () => getFileMetadata({ data: { id } }),
  })

  const [confirmDelete, setConfirmDelete] = useState(false)
  const deleteMutation = useMutation({
    mutationFn: () => deleteFile({ data: { id } }),
    onSuccess: () => {
      // The file is gone: refresh every surface that could still list it
      // (search list, browse list, sidebar tree) and leave for the parent
      // folder — the file page has nothing left to show.
      queryClient.invalidateQueries({ queryKey: ['files'] })
      queryClient.invalidateQueries({ queryKey: ['directory'] })
      queryClient.invalidateQueries({ queryKey: ['folder-tree'] })
      if (data) {
        void navigate({
          to: '/',
          search: {
            ...DEFAULT_BROWSE_FILTERS,
            path: data.key.slice(0, data.key.lastIndexOf('/') + 1),
          },
        })
      }
    },
  })

  const [moveOpen, setMoveOpen] = useState(false)
  const [moveError, setMoveError] = useState<string | null>(null)
  const moveMutation = useMutation({
    mutationFn: (destinationKey: string) => moveFile({ data: { id, destinationKey } }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['files'] })
      queryClient.invalidateQueries({ queryKey: ['directory'] })
      queryClient.invalidateQueries({ queryKey: ['folder-tree'] })
      queryClient.invalidateQueries({ queryKey: ['file-metadata', id] })
      setMoveOpen(false)
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

  async function handleDownload() {
    const { url } = await getDownloadUrl({ data: { id } })
    window.open(url, '_blank', 'noopener')
  }

  if (isPending) {
    return (
      <main style={{ padding: '0 1rem' }}>
        <p>Loading…</p>
      </main>
    )
  }
  if (isError) {
    return (
      <main style={{ padding: '0 1rem' }}>
        <p role="alert">Failed to load file: {String(error)}</p>
      </main>
    )
  }

  // Browse-mode path of the file's parent directory; '' (root, shown as "/")
  // when the key has no '/'.
  const parentPath = data.key.slice(0, data.key.lastIndexOf('/') + 1)

  return (
    <main style={{ padding: '0 1rem' }}>
      <p>
        <Link to="/" search={{ ...DEFAULT_BROWSE_FILTERS, path: parentPath }}>
          📁 {parentPath === '' ? '/' : parentPath}
        </Link>
      </p>
      <h1>{data.key.split('/').pop()}</h1>
      <p>
        <button type="button" onClick={() => void handleDownload()}>
          Download
        </button>{' '}
        <button type="button" onClick={() => setMoveOpen(true)}>
          Move
        </button>{' '}
        <button type="button" onClick={() => setConfirmDelete(true)}>
          Delete
        </button>
      </p>
      <FileMetadataTable file={data} />
      {confirmDelete ? (
        <DeleteFileConfirmation
          fileKey={data.key}
          pending={deleteMutation.isPending}
          error={deleteMutation.isError ? deleteMutation.error : null}
          onConfirm={() => deleteMutation.mutate()}
          onCancel={() => setConfirmDelete(false)}
        />
      ) : null}
      {moveOpen ? (
        <MoveFileDialog
          fileKey={data.key}
          moving={moveMutation.isPending}
          error={moveError}
          onConfirm={(destinationPath) => {
            const destinationKey = destinationPath + data.key.slice(data.key.lastIndexOf('/') + 1)
            moveMutation.mutate(destinationKey)
          }}
          onCancel={() => {
            setMoveOpen(false)
            setMoveError(null)
          }}
        />
      ) : null}
    </main>
  )
}
