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
 * Everpresent top bar: app link plus the global file search. SearchBar is
 * navigation-agnostic; wiring to the file route lives here so the component
 * stays testable without a router.
 */
function Header() {
  const navigate = useNavigate()
  return (
    <header
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: '1rem',
        padding: '0.5rem 1rem',
        borderBottom: '1px solid #ddd',
      }}
    >
      <Link to="/" style={{ fontWeight: 'bold' }}>
        file-indexer
      </Link>
      <SearchBar onSelect={(file) => void navigate({ to: '/file/$id', params: { id: file.id } })} />
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
