interface BreadcrumbsProps {
  /** Current browse path; "" is the bucket root. */
  path: string
  onNavigate: (path: string) => void
}

/**
 * Renders "Home / docs / sub" for a browse path, each ancestor segment
 * clickable to jump straight there. The current (last) segment is plain
 * text, matching the usual breadcrumb convention of not linking to the page
 * you're already on.
 */
export function Breadcrumbs({ path, onNavigate }: BreadcrumbsProps) {
  const segments = path === '' ? [] : path.replace(/\/$/, '').split('/')

  return (
    <nav aria-label="Breadcrumb">
      <button type="button" onClick={() => onNavigate('')} disabled={segments.length === 0}>
        Home
      </button>
      {segments.map((segment, i) => {
        const segmentPath = `${segments.slice(0, i + 1).join('/')}/`
        const isLast = i === segments.length - 1
        return (
          // Index is part of the identity here: segments are positional path
          // components, and duplicate names at different depths are legal.
          // biome-ignore lint/suspicious/noArrayIndexKey: positional path segments
          <span key={i}>
            {' / '}
            {isLast ? (
              <span aria-current="page">{segment}</span>
            ) : (
              <button type="button" onClick={() => onNavigate(segmentPath)}>
                {segment}
              </button>
            )}
          </span>
        )
      })}
    </nav>
  )
}
