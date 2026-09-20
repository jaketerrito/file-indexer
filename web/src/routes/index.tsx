import { createFileRoute, stripSearchParams } from '@tanstack/react-router'
import { Browser } from '../components/Browser'
import { FolderTree } from '../components/FolderTree'
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
            onNavigate={(path) => handleFiltersChange({ ...filters, path })}
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
