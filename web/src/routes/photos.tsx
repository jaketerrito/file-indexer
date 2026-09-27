import { createFileRoute } from '@tanstack/react-router'
import { PhotoWall } from '../components/PhotoWall'

export const Route = createFileRoute('/photos')({
  component: PhotosPage,
})

function PhotosPage() {
  const navigate = Route.useNavigate()
  return (
    <main style={{ padding: '0 1rem' }}>
      <PhotoWall onOpenFile={(id) => void navigate({ to: '/file/$id', params: { id } })} />
    </main>
  )
}
