import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { FileList } from '../components/FileList'
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
    // replace: filter tweaks (especially debounced typing) shouldn't pile up
    // in the browser history.
    void navigate({ search: next, replace: true })
  }

  return (
    <main>
      <h1>Files</h1>
      <FileList filters={filters} onFiltersChange={handleFiltersChange} />
    </main>
  )
}
