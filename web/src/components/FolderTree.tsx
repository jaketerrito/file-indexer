import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { listDirectory } from '../server/files'

/**
 * Fetches every immediate child directory of `path`. ListDirectory paginates
 * with all subdirectories ahead of any file (its two-phase design), so the
 * directory run is complete as soon as a page contains a file or the pages
 * run out.
 */
async function fetchChildDirectories(path: string): Promise<string[]> {
  const directories: string[] = []
  let pageToken: string | undefined
  do {
    const page = await listDirectory({
      data: { path, pageToken, sortField: 'key', sortOrder: 'asc' },
    })
    directories.push(...page.directories)
    if (page.files.length > 0) break
    pageToken = page.nextPageToken === '' ? undefined : page.nextPageToken
  } while (pageToken !== undefined)
  return directories
}

interface FolderTreeProps {
  /** Currently browsed directory ("" is the root); undefined in search mode. */
  currentPath: string | undefined
  onNavigate: (path: string) => void
}

/**
 * Left-sidebar folder tree over the S3 key structure. Children load lazily
 * when a folder is expanded; ancestors of the current path expand
 * automatically so the browsed folder is always visible, and navigation
 * never force-collapses what the user opened.
 */
export function FolderTree({ currentPath, onNavigate }: FolderTreeProps) {
  return (
    <nav aria-label="Folders">
      <ul style={{ listStyle: 'none', margin: 0, paddingLeft: 0 }}>
        <li>
          <button
            type="button"
            onClick={() => onNavigate('')}
            aria-current={currentPath === '' ? 'page' : undefined}
            style={currentPath === '' ? { fontWeight: 'bold' } : undefined}
          >
            Home
          </button>
        </li>
        <FolderLevel path="" depth={1} currentPath={currentPath} onNavigate={onNavigate} />
      </ul>
    </nav>
  )
}

interface FolderLevelProps {
  /** Directory whose children this level lists ("" is the root). */
  path: string
  /** Nesting depth, used for indentation. */
  depth: number
  currentPath: string | undefined
  onNavigate: (path: string) => void
}

function FolderLevel({ path, depth, currentPath, onNavigate }: FolderLevelProps) {
  const { data, isPending, isError, error } = useQuery({
    queryKey: ['folder-tree', path],
    queryFn: () => fetchChildDirectories(path),
    staleTime: 30_000,
  })

  if (isPending) {
    return <li style={{ paddingLeft: `${depth}rem`, color: '#666' }}>Loading…</li>
  }
  if (isError) {
    return (
      <li role="alert" style={{ paddingLeft: `${depth}rem` }}>
        Failed to load folders: {String(error)}
      </li>
    )
  }
  // No subdirectories: render nothing (files don't appear in the tree).
  if (data.length === 0) return null
  return (
    <>
      {data.map((dir) => (
        <FolderNode
          key={dir}
          prefix={dir}
          depth={depth}
          currentPath={currentPath}
          onNavigate={onNavigate}
        />
      ))}
    </>
  )
}

interface FolderNodeProps {
  /** Full directory prefix, trailing slash included ("docs/work/"). */
  prefix: string
  depth: number
  currentPath: string | undefined
  onNavigate: (path: string) => void
}

function FolderNode({ prefix, depth, currentPath, onNavigate }: FolderNodeProps) {
  // Display name: "docs/work/" → "work".
  const name = prefix.replace(/\/$/, '').split('/').pop() as string
  const onCurrentPath = currentPath?.startsWith(prefix) === true
  const [expanded, setExpanded] = useState(onCurrentPath)
  useEffect(() => {
    if (onCurrentPath) setExpanded(true)
  }, [onCurrentPath])
  const isCurrent = currentPath === prefix

  return (
    <li style={{ paddingLeft: `${depth}rem` }}>
      <button
        type="button"
        aria-expanded={expanded}
        aria-label={expanded ? `Collapse folder ${name}` : `Expand folder ${name}`}
        onClick={() => setExpanded((prev) => !prev)}
      >
        {expanded ? '▾' : '▸'}
      </button>{' '}
      <button
        type="button"
        onClick={() => onNavigate(prefix)}
        aria-current={isCurrent ? 'page' : undefined}
        style={isCurrent ? { fontWeight: 'bold' } : undefined}
      >
        {name}
      </button>
      {expanded ? (
        <ul style={{ listStyle: 'none', margin: 0, paddingLeft: 0 }}>
          <FolderLevel
            path={prefix}
            depth={depth + 1}
            currentPath={currentPath}
            onNavigate={onNavigate}
          />
        </ul>
      ) : null}
    </li>
  )
}
