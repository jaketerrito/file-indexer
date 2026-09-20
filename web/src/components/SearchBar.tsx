import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { listFiles, searchDirectories } from '../server/files'
import type { FileDto } from '../server/impl'

const DEBOUNCE_MS = 300
const RESULT_LIMIT = 10
const FOLDER_RESULT_LIMIT = 5

interface SearchBarProps {
  /** Called with the chosen file; the parent owns what happens next (navigation). */
  onSelect: (file: FileDto) => void
  /** Called with the chosen directory's full trailing-slash path; the parent
   * owns what happens next (navigation). */
  onSelectFolder: (path: string) => void
}

/**
 * Everpresent header search: debounced text query against ListFiles and
 * SearchDirectories (case-insensitive substring + trigram fuzzy match),
 * showing a dropdown of matching folders (up to FOLDER_RESULT_LIMIT) above
 * matching file keys (path + name).
 * Selecting one hands it to onSelectFolder/onSelect and clears the box.
 */
export function SearchBar({ onSelect, onSelectFolder }: SearchBarProps) {
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

  const foldersQuery = useQuery({
    queryKey: ['search-dropdown-folders', query],
    queryFn: () => searchDirectories({ data: { query, pageSize: FOLDER_RESULT_LIMIT } }),
    enabled: query !== '',
  })

  function handleSelect(file: FileDto) {
    onSelect(file)
    setInput('')
    setQuery('')
    setOpen(false)
  }

  function handleSelectFolder(path: string) {
    onSelectFolder(path)
    setInput('')
    setQuery('')
    setOpen(false)
  }

  return (
    <div style={{ position: 'relative' }}>
      <input
        type="search"
        aria-label="Search files and folders"
        placeholder="Search files and folders…"
        value={input}
        onChange={(e) => {
          setInput(e.target.value)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') setOpen(false)
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
          {isFetching || foldersQuery.isFetching ? (
            <li style={{ padding: '0.25rem 0.5rem' }}>Searching…</li>
          ) : null}
          {!isFetching &&
          !foldersQuery.isFetching &&
          (data?.files.length ?? 0) === 0 &&
          (foldersQuery.data?.directories.length ?? 0) === 0 ? (
            <li style={{ padding: '0.25rem 0.5rem' }}>No matches</li>
          ) : null}
          {foldersQuery.data?.directories.map((dir) => (
            <li key={dir}>
              <button
                type="button"
                // onMouseDown + preventDefault: fires before the input's blur
                // closes the dropdown, and keeps focus in the input.
                onMouseDown={(e) => {
                  e.preventDefault()
                  handleSelectFolder(dir)
                }}
                style={{
                  display: 'block',
                  width: '100%',
                  textAlign: 'left',
                  padding: '0.25rem 0.5rem',
                  fontWeight: 'bold',
                }}
              >
                {dir}
              </button>
            </li>
          ))}
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
