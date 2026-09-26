import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createRootRoute,
  HeadContent,
  Link,
  Outlet,
  Scripts,
  useNavigate,
} from '@tanstack/react-router'
import { type ReactNode, useState } from 'react'
import { SearchBar } from '../components/SearchBar'
import { DEFAULT_BROWSE_FILTERS, DEFAULT_SEARCH_FILTERS } from '../lib/fileFilters'

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { title: 'file-indexer' },
    ],
  }),
  component: RootComponent,
})

function RootComponent() {
  const [queryClient] = useState(() => new QueryClient())
  return (
    <RootDocument>
      <QueryClientProvider client={queryClient}>
        <Header />
        <Outlet />
      </QueryClientProvider>
    </RootDocument>
  )
}

/**
 * Everpresent top bar: primary view navigation plus the global file search.
 * The bar is sticky so it stays visible while scrolling long lists.
 */
function Header() {
  const navigate = useNavigate()
  const linkStyle: React.CSSProperties = {
    padding: '0.25rem 0.5rem',
    textDecoration: 'none',
    color: '#000',
  }

  return (
    <header
      style={{
        position: 'sticky',
        top: 0,
        display: 'flex',
        alignItems: 'center',
        gap: '1rem',
        padding: '0.5rem 1rem',
        borderBottom: '1px solid #ddd',
        background: '#fff',
        zIndex: 10,
      }}
    >
      <nav aria-label="Main" style={{ display: 'flex', gap: '0.5rem' }}>
        <Link
          to="/"
          search={{ ...DEFAULT_BROWSE_FILTERS }}
          activeOptions={{ exact: true }}
          activeProps={{
            style: { ...linkStyle, fontWeight: 'bold', borderBottom: '2px solid #000' },
          }}
          style={linkStyle}
        >
          Files
        </Link>
        <Link
          to="/recents"
          activeOptions={{ exact: true }}
          activeProps={{
            style: { ...linkStyle, fontWeight: 'bold', borderBottom: '2px solid #000' },
          }}
          style={linkStyle}
        >
          Recents
        </Link>
        <Link
          to="/photos"
          activeOptions={{ exact: true }}
          activeProps={{
            style: { ...linkStyle, fontWeight: 'bold', borderBottom: '2px solid #000' },
          }}
          style={linkStyle}
        >
          Photos
        </Link>
      </nav>
      <SearchBar
        onSelect={(file) => void navigate({ to: '/file/$id', params: { id: file.id } })}
        onSelectFolder={(path) =>
          void navigate({ to: '/', search: { ...DEFAULT_BROWSE_FILTERS, path } })
        }
        onSearch={(query) =>
          void navigate({ to: '/search', search: { ...DEFAULT_SEARCH_FILTERS, query } })
        }
      />
    </header>
  )
}

function RootDocument({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}
