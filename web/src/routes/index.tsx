import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { Browser } from '../components/Browser'
import {
  type BrowseFilters,
  DEFAULT_BROWSE_FILTERS,
  normalizeBrowseFilters,
} from '../lib/fileFilters'

export const Route = createFileRoute('/')({
  validateSearch: normalizeBrowseFilters,
  search: {
    // Keep default values out of the URL so the bucket root is just "/".
    middlewares: [stripSearchParams(DEFAULT_BROWSE_FILTERS)],
  },
  component: Home,
})

function Home() {
  const filters = Route.useSearch()
  const navigate = Route.useNavigate()

  function handleFiltersChange(next: BrowseFilters) {
    // Folder navigation (any path change) pushes a history entry so browser
    // Back/Forward moves between folders. In-place sort/order tweaks replace
    // — they shouldn't pile up in the browser history.
    void navigate({ search: next, replace: next.path === filters.path })
  }

  return (
    <main style={{ padding: '0 1rem' }}>
      <Browser
        filters={filters}
        onFiltersChange={handleFiltersChange}
        onOpenFile={(id) => void navigate({ to: '/file/$id', params: { id } })}
      />
    </main>
  )
}
