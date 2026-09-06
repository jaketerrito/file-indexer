import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { Browser } from '../components/Browser'
import { FolderTree } from '../components/FolderTree'
import {
  DEFAULT_FILTERS,
  type FileFilters,
  normalizeFilters,
  toBrowsePath,
} from '../lib/fileFilters'

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
    <main style={{ padding: '0 1rem' }}>
      <h1>Files</h1>
      <div style={{ display: 'flex', gap: '1.5rem', alignItems: 'flex-start' }}>
        <aside
          style={{
            flex: '0 0 14rem',
            borderRight: '1px solid #ddd',
            paddingRight: '1rem',
          }}
        >
          <h2 style={{ fontSize: '1rem' }}>Folders</h2>
          <FolderTree
            currentPath={filters.path}
            onNavigate={(path) => handleFiltersChange(toBrowsePath(filters, path))}
          />
        </aside>
        <section style={{ flex: 1, minWidth: 0 }}>
          <Browser
            filters={filters}
            onFiltersChange={handleFiltersChange}
            onOpenFile={(id) => void navigate({ to: '/file/$id', params: { id } })}
          />
        </section>
      </div>
    </main>
  )
}
