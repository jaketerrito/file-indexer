import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { DeleteFileConfirmation } from '../components/DeleteFileConfirmation'
import { FileMetadataTable } from '../components/FileMetadataTable'
import { deleteFile, getDownloadUrl, getFileMetadata } from '../server/files'

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
            path: data.key.slice(0, data.key.lastIndexOf('/') + 1),
            prefix: '',
            type: '',
            sort: 'key',
            order: 'asc',
          },
        })
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
        <Link to="/" search={{ path: parentPath, prefix: '', type: '', sort: 'key', order: 'asc' }}>
          📁 {parentPath === '' ? '/' : parentPath}
        </Link>
      </p>
      <h1>{data.key.split('/').pop()}</h1>
      <p>
        <button type="button" onClick={() => void handleDownload()}>
          Download
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
    </main>
  )
}
