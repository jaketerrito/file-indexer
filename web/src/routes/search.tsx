import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { FileList } from '../components/FileList'
import {
  DEFAULT_SEARCH_FILTERS,
  normalizeSearchFilters,
  type SearchFilters,
} from '../lib/fileFilters'

export const Route = createFileRoute('/search')({
  validateSearch: normalizeSearchFilters,
  search: {
    // Keep default values out of the URL so an unfiltered search is just "/search".
    middlewares: [stripSearchParams(DEFAULT_SEARCH_FILTERS)],
  },
  component: SearchPage,
})

/**
 * Full search results page: destination of the header search's Enter key
 * (the dropdown stays a quick-preview shortcut to individual files). Hosts
 * the flat, recursive, filterable FileList with URL-backed filters, so a
 * search is shareable and survives reload.
 */
function SearchPage() {
  const filters = Route.useSearch()
  const navigate = Route.useNavigate()

  function handleFiltersChange(next: SearchFilters) {
    // In-place filter tweaks (type/sort/order) replace — they shouldn't pile
    // up in browser history. New queries push via the header search's Enter.
    void navigate({ search: next, replace: true })
  }

  return (
    <main style={{ padding: '0 1rem' }}>
      <h1>Search results</h1>
      <FileList
        filters={filters}
        onFiltersChange={handleFiltersChange}
        onOpenFile={(id) => void navigate({ to: '/file/$id', params: { id } })}
      />
    </main>
  )
}
