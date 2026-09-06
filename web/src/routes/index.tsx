import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { Browser } from '../components/Browser'
import { DEFAULT_FILTERS, type FileFilters, normalizeFilters } from '../lib/fileFilters'

export const Route = createFileRoute('/')({
  validateSearch: normalizeFilters,
  search: {
    // Keep default values out of the URL so an unfiltered list is just "/".
    middlewares: [stripSearchParams(DEFAULT_FILTERS)],
  },
  component: Home,
})

function Home() {
  const filters = Route.useSearch()
  const navigate = Route.useNavigate()

  function handleFiltersChange(next: FileFilters) {
    // Folder navigation (any path change, including entering/leaving browse
    // mode) pushes a history entry so browser Back/Forward moves between
    // folders. In-place filter tweaks (debounced typing, sort/order) replace
    // — they shouldn't pile up in the browser history.
    void navigate({ search: next, replace: next.path === filters.path })
  }

  return (
    <main>
      <h1>Files</h1>
      <Browser filters={filters} onFiltersChange={handleFiltersChange} />
    </main>
  )
}
