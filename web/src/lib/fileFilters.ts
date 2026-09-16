import { SORT_FIELDS, SORT_ORDERS, type SortFieldInput, type SortOrderInput } from '../server/impl'

// URL-backed filter state for the file list. The route's validateSearch runs
// normalizeFilters over raw ?path=&query=&type=&sort=&order= params; a
// stripSearchParams middleware on the route drops default values from the URL.
//
// path and query are mutually exclusive modes, not independent filters:
// path present (even "" for root) means browse mode (ListDirectory, folders
// + direct children); query present means search mode (ListFiles, flat
// recursive results). Setting either clears the other — see toBrowsePath
// below — because a request can't sensibly carry both (ListDirectory has no
// query param, and folder navigation while a recursive text search is
// still applied would be confusing).
//
// path defaults to undefined, not "", specifically so the default,
// filter-free view is search mode ("/") rather than browse mode at the root
// — matching the pre-existing UX above e.g. `stripSearchParams` cleanly
// drops it from the URL.

export interface FileFilters {
  /** Browse mode: current directory ("" is root). undefined means search mode. */
  path?: string
  /** Text query to search keys by; '' means no query filter. */
  query: string
  /** Content-type filter ('image/', ...); '' means all types. */
  type: string
  sort: SortFieldInput
  order: SortOrderInput
}

export const DEFAULT_FILTERS: FileFilters = {
  query: '',
  type: '',
  sort: 'key',
  order: 'asc',
}

/** Parses raw URL search params into FileFilters, dropping invalid values. */
export function normalizeFilters(search: Record<string, unknown>): FileFilters {
  return {
    path: typeof search.path === 'string' ? search.path : undefined,
    query: typeof search.query === 'string' ? search.query : DEFAULT_FILTERS.query,
    type: typeof search.type === 'string' ? search.type : DEFAULT_FILTERS.type,
    sort: SORT_FIELDS.includes(search.sort as SortFieldInput)
      ? (search.sort as SortFieldInput)
      : DEFAULT_FILTERS.sort,
    order: SORT_ORDERS.includes(search.order as SortOrderInput)
      ? (search.order as SortOrderInput)
      : DEFAULT_FILTERS.order,
  }
}

/** True when filters are in browse mode (ListDirectory) rather than search (ListFiles). */
export function isBrowsing(filters: FileFilters): boolean {
  return filters.path !== undefined
}

/** Filters for navigating to a directory: clears query/type, which don't apply in browse mode. */
export function toBrowsePath(filters: FileFilters, path: string): FileFilters {
  return { ...filters, path, query: '', type: '' }
}
