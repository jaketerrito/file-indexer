interface BreadcrumbsProps {
  /** Current browse path; "" is the bucket root. */
  path: string
  onNavigate: (path: string) => void
  /**
   * When true (default), the last segment is rendered as plain text because
   * the user is already on that page. When false, the last segment is also
   * clickable — used on the file page where the breadcrumb shows the file's
   * parent directory and we want to let the user enter it.
   */
  lastIsCurrent?: boolean
}

/**
 * Renders "Home / docs / sub" for a browse path, each ancestor segment
 * clickable to jump straight there. The last segment follows `lastIsCurrent`.
 */
export function Breadcrumbs({ path, onNavigate, lastIsCurrent = true }: BreadcrumbsProps) {
  const segments = path === '' ? [] : path.replace(/\/$/, '').split('/')

  return (
    <nav aria-label="Breadcrumb">
      <button type="button" onClick={() => onNavigate('')}>
        Home
      </button>
      {segments.map((segment, i) => {
        const segmentPath = `${segments.slice(0, i + 1).join('/')}/`
        const isLast = i === segments.length - 1
        const clickable = !isLast || !lastIsCurrent
        return (
          // Index is part of the identity here: segments are positional path
          // components, and duplicate names at different depths are legal.
          // biome-ignore lint/suspicious/noArrayIndexKey: positional path segments
          <span key={i}>
            {' / '}
            {clickable ? (
              <button type="button" onClick={() => onNavigate(segmentPath)}>
                {segment}
              </button>
            ) : (
              <span aria-current="page">{segment}</span>
            )}
          </span>
        )
      })}
    </nav>
  )
}
