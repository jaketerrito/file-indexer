/**
 * Formats an ISO date string for display, e.g. "Mar 28, 2026".
 * Uses UTC so the rendered date is stable regardless of the viewer's
 * timezone (server timestamps are UTC).
 */
export function formatDate(isoDate: string | null | undefined): string {
  if (!isoDate) return ''
  const date = new Date(isoDate)
  if (Number.isNaN(date.getTime())) return isoDate
  return new Intl.DateTimeFormat('en-US', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC',
  }).format(date)
}
