import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { listFiles } from '../server/files'
import type { FileDto } from '../server/impl'

const DEBOUNCE_MS = 300
const RESULT_LIMIT = 10

interface SearchBarProps {
  /** Called with the chosen file; the parent owns what happens next (navigation). */
  onSelect: (file: FileDto) => void
  /** Called with the trimmed query on Enter; the parent owns navigation to the results page. */
  onSearch: (query: string) => void
}

/**
 * Everpresent header search: debounced text query against ListFiles
 * (case-insensitive substring + trigram fuzzy match), showing a dropdown of
 * matching file keys (path + name). The dropdown is a quick preview into
 * individual files — selecting one hands the file to onSelect and clears the
 * box; Enter hands the trimmed query to onSearch for the full results page.
 */
export function SearchBar({ onSelect, onSearch }: SearchBarProps) {
  const [input, setInput] = useState('')
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const timer = setTimeout(() => setQuery(input.trim()), DEBOUNCE_MS)
    return () => clearTimeout(timer)
  }, [input])

  const { data, isFetching } = useQuery({
    queryKey: ['search-dropdown', query],
    queryFn: () =>
      listFiles({
        data: { query, pageSize: RESULT_LIMIT, sortField: 'key', sortOrder: 'asc' },
      }),
    enabled: query !== '',
  })

  function handleSelect(file: FileDto) {
    onSelect(file)
    setInput('')
    setQuery('')
    setOpen(false)
  }

  return (
    <div style={{ position: 'relative' }}>
      <input
        type="search"
        aria-label="Search files by path"
        placeholder="Search files…"
        value={input}
        onChange={(e) => {
          setInput(e.target.value)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') setOpen(false)
          if (e.key === 'Enter') {
            const trimmed = input.trim()
            if (trimmed !== '') {
              // Enter goes to the full results page. The input keeps its text
              // so the box still shows what was searched after navigation;
              // empty input is a no-op — there is no useful "everything"
              // destination.
              e.preventDefault()
              onSearch(trimmed)
              setOpen(false)
            }
          }
        }}
        style={{ width: '24rem', maxWidth: '50vw' }}
      />
      {open && query !== '' ? (
        <ul
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            right: 0,
            margin: 0,
            padding: 0,
            listStyle: 'none',
            background: 'white',
            border: '1px solid #ccc',
            zIndex: 10,
          }}
        >
          {isFetching ? <li style={{ padding: '0.25rem 0.5rem' }}>Searching…</li> : null}
          {!isFetching && data?.files.length === 0 ? (
            <li style={{ padding: '0.25rem 0.5rem' }}>No matches</li>
          ) : null}
          {data?.files.map((file) => (
            <li key={file.id}>
              <button
                type="button"
                // onMouseDown + preventDefault: fires before the input's blur
                // closes the dropdown, and keeps focus in the input.
                onMouseDown={(e) => {
                  e.preventDefault()
                  handleSelect(file)
                }}
                style={{
                  display: 'block',
                  width: '100%',
                  textAlign: 'left',
                  padding: '0.25rem 0.5rem',
                }}
              >
                {file.key}
              </button>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}
