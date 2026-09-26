import { Code, ConnectError } from '@connectrpc/connect'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { Breadcrumbs } from '../components/Breadcrumbs'
import { DeleteFileConfirmation } from '../components/DeleteFileConfirmation'
import { FileMetadataTable } from '../components/FileMetadataTable'
import { MoveFileDialog } from '../components/MoveFileDialog'
import { DEFAULT_BROWSE_FILTERS } from '../lib/fileFilters'
import { deleteFile, getDownloadUrl, getFileMetadata, getOpenUrl, moveFile } from '../server/files'

export const Route = createFileRoute('/file/$id')({
  component: FilePage,
})

/**
 * Standalone file page: a single vertical stack that works on small and
 * large screens. Buttons sit right under the file name, followed by the
 * preview area, then the metadata table. Deleting returns the user to the
 * parent folder.
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

  async function handleOpen() {
    const { url } = await getOpenUrl({ data: { id } })
    window.open(url, '_blank', 'noopener')
  }

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
      <Breadcrumbs
        path={parentPath}
        lastIsCurrent={false}
        onNavigate={(path) =>
          void navigate({ to: '/', search: { ...DEFAULT_BROWSE_FILTERS, path } })
        }
      />
      <h1>{data.key.split('/').pop()}</h1>
      <nav aria-label="File actions" style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap' }}>
        <button type="button" onClick={() => void handleOpen()}>
          Open
        </button>
        <button type="button" onClick={() => void handleDownload()}>
          Download
        </button>
        <button type="button" onClick={() => setMoveOpen(true)}>
          Move
        </button>
        <button type="button" onClick={() => setConfirmDelete(true)}>
          Delete
        </button>
      </nav>
      <section
        style={{
          margin: '1rem 0',
          padding: '1rem',
          minHeight: '10rem',
          border: '1px dashed #ccc',
          background: '#fafafa',
        }}
      >
        Preview not available
      </section>
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
