import { SORT_FIELDS, SORT_ORDERS, type SortFieldInput, type SortOrderInput } from '../server/impl'

// URL-backed filter state, one shape per route:
// - "/" (browse): path + sort/order over ListDirectory's direct children.
// - "/search": query/type + sort/order over the flat recursive ListFiles.
// The two used to be one type with mutually exclusive path/query; the split
// routes make the exclusion structural — no param of one route means
// anything on the other.

export interface BrowseFilters {
  /** Current directory; '' is the bucket root. */
  path: string
  sort: SortFieldInput
  order: SortOrderInput
}

export interface SearchFilters {
  /** Text query to search keys by; '' means no query filter. */
  query: string
  /** Content-type filter ('image/', ...); '' means all types. */
  type: string
  sort: SortFieldInput
  order: SortOrderInput
}

export const DEFAULT_BROWSE_FILTERS: BrowseFilters = { path: '', sort: 'key', order: 'asc' }

export const DEFAULT_SEARCH_FILTERS: SearchFilters = {
  query: '',
  type: '',
  sort: 'key',
  order: 'asc',
}

function parseSort(value: unknown): SortFieldInput {
  return SORT_FIELDS.includes(value as SortFieldInput)
    ? (value as SortFieldInput)
    : DEFAULT_BROWSE_FILTERS.sort
}

function parseOrder(value: unknown): SortOrderInput {
  return SORT_ORDERS.includes(value as SortOrderInput)
    ? (value as SortOrderInput)
    : DEFAULT_BROWSE_FILTERS.order
}

/** Parses raw URL search params for the browse route ("/"), dropping invalid values. */
export function normalizeBrowseFilters(search: Record<string, unknown>): BrowseFilters {
  return {
    path: typeof search.path === 'string' ? search.path : DEFAULT_BROWSE_FILTERS.path,
    sort: parseSort(search.sort),
    order: parseOrder(search.order),
  }
}

/** Parses raw URL search params for the search route ("/search"), dropping invalid values. */
export function normalizeSearchFilters(search: Record<string, unknown>): SearchFilters {
  return {
    query: typeof search.query === 'string' ? search.query : DEFAULT_SEARCH_FILTERS.query,
    type: typeof search.type === 'string' ? search.type : DEFAULT_SEARCH_FILTERS.type,
    sort: parseSort(search.sort),
    order: parseOrder(search.order),
  }
}
