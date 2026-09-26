import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { FileList } from '../components/FileList'
import {
  DEFAULT_LIST_FILTERS,
  type ListFilters,
  normalizeListFilters,
  type SearchFilters,
} from '../lib/fileFilters'

export const Route = createFileRoute('/photos')({
  validateSearch: normalizeListFilters,
  search: {
    middlewares: [stripSearchParams(DEFAULT_LIST_FILTERS)],
  },
  component: PhotosPage,
})

const PHOTOS_FILTERS: SearchFilters = {
  query: '',
  type: 'image/',
  sort: DEFAULT_LIST_FILTERS.sort,
  order: DEFAULT_LIST_FILTERS.order,
}

function PhotosPage() {
  const filters = Route.useSearch()
  const navigate = Route.useNavigate()

  function handleFiltersChange(next: ListFilters) {
    void navigate({ search: next, replace: true })
  }

  return (
    <main style={{ padding: '0 1rem' }}>
      <FileList
        filters={{ ...PHOTOS_FILTERS, ...filters }}
        onFiltersChange={(next) => handleFiltersChange({ sort: next.sort, order: next.order })}
        onOpenFile={(id) => void navigate({ to: '/file/$id', params: { id } })}
        hideSearchFilters
      />
    </main>
  )
}
