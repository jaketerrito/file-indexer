import { Code, ConnectError } from '@connectrpc/connect'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { Breadcrumbs } from '../components/Breadcrumbs'
import { DeleteFileConfirmation } from '../components/DeleteFileConfirmation'
import { FileBasicInfo, FileMetadataTable } from '../components/FileMetadataTable'
import { FileViewer } from '../components/FileViewer'
import { MoveFileDialog } from '../components/MoveFileDialog'
import { DEFAULT_BROWSE_FILTERS } from '../lib/fileFilters'
import { deleteFile, getDownloadUrl, getFileMetadata, getOpenUrl, moveFile } from '../server/files'

export const Route = createFileRoute('/file/$id')({
  component: FilePage,
})

/**
 * Standalone file page: a single vertical stack that works on small and
 * large screens. Basic stats sit next to the file name, action buttons sit
 * right under it, followed by the preview area. The full metadata table
 * opens in a modal. Deleting returns the user to the parent folder.
 */
export function FilePage() {
  const { id } = Route.useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { data, isPending, isError, error } = useQuery({
    queryKey: ['file-metadata', id],
    queryFn: () => getFileMetadata({ data: { id } }),
  })

  const {
    data: openData,
    isPending: openPending,
    isError: openIsError,
    error: openError,
  } = useQuery({
    queryKey: ['file-open-url', id],
    queryFn: () => getOpenUrl({ data: { id } }),
  })

  const [confirmDelete, setConfirmDelete] = useState(false)
  const [metadataOpen, setMetadataOpen] = useState(false)
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
      <main
        style={{
          padding: '0 1rem',
          height: '100vh',
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <p>Loading…</p>
      </main>
    )
  }
  if (isError) {
    return (
      <main
        style={{
          padding: '0 1rem',
          height: '100vh',
          overflow: 'hidden',
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <p role="alert">Failed to load file: {String(error)}</p>
      </main>
    )
  }

  // Browse-mode path of the file's parent directory; '' (root, shown as "/")
  // when the key has no '/'.
  const parentPath = data.key.slice(0, data.key.lastIndexOf('/') + 1)

  return (
    <main
      style={{
        padding: '0 1rem',
        height: '100vh',
        overflow: 'hidden',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div style={{ margin: '0.5rem 0' }}>
        <Breadcrumbs
          path={parentPath}
          lastIsCurrent={false}
          onNavigate={(path) =>
            void navigate({ to: '/', search: { ...DEFAULT_BROWSE_FILTERS, path } })
          }
        />
      </div>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'baseline',
          flexWrap: 'wrap',
          gap: '1rem',
          margin: '0.5rem 0',
        }}
      >
        <h1
          style={{
            margin: 0,
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'baseline',
            gap: '0.75rem',
          }}
        >
          {data.key.split('/').pop()}
          <FileBasicInfo file={data} />
        </h1>
        <nav
          aria-label="File actions"
          style={{ display: 'flex', gap: '0.75rem', flexWrap: 'wrap', margin: '0.25rem 0' }}
        >
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
          <button type="button" onClick={() => setMetadataOpen(true)}>
            Metadata
          </button>
        </nav>
      </div>
      <section
        style={{
          margin: '0.5rem 0 1rem',
          padding: '1rem',
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          minHeight: 0,
          overflow: 'hidden',
          border: '1px dashed #ccc',
          background: '#fafafa',
        }}
      >
        {openPending ? (
          <p>Loading preview…</p>
        ) : openIsError ? (
          <p role="alert">Failed to load preview: {String(openError)}</p>
        ) : (
          <FileViewer contentType={data.contentType} openUrl={openData.url} />
        )}
      </section>
      {confirmDelete ? (
        <DeleteFileConfirmation
          fileKey={data.key}
          pending={deleteMutation.isPending}
          error={deleteMutation.isError ? deleteMutation.error : null}
          onConfirm={() => deleteMutation.mutate()}
          onCancel={() => setConfirmDelete(false)}
        />
      ) : null}
      {metadataOpen ? (
        <div
          role="dialog"
          aria-modal="true"
          aria-label="File metadata"
          style={{
            position: 'fixed',
            inset: 0,
            background: 'rgba(0, 0, 0, 0.5)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
          onClick={(e) => {
            if (e.target === e.currentTarget) setMetadataOpen(false)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Escape') setMetadataOpen(false)
          }}
        >
          <div
            style={{
              background: 'white',
              padding: '1rem',
              minWidth: '24rem',
              maxWidth: '90vw',
              maxHeight: '90vh',
              overflow: 'auto',
            }}
          >
            <h2>Metadata</h2>
            <FileMetadataTable file={data} />
            <button type="button" onClick={() => setMetadataOpen(false)}>
              Close
            </button>
          </div>
        </div>
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
