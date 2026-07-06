import { SORT_FIELDS, SORT_ORDERS, type SortFieldInput, type SortOrderInput } from '../server/impl'

// URL-backed filter state for the file list. The route's validateSearch runs
// normalizeFilters over raw ?prefix=&type=&sort=&order= params; a
// stripSearchParams middleware on the route drops default values from the URL.

export interface FileFilters {
  /** Key prefix to search by; '' means no prefix filter. */
  prefix: string
  /** Content-type filter ('image/', ...); '' means all types. */
  type: string
  sort: SortFieldInput
  order: SortOrderInput
}

export const DEFAULT_FILTERS: FileFilters = {
  prefix: '',
  type: '',
  sort: 'key',
  order: 'asc',
}

/** Parses raw URL search params into FileFilters, dropping invalid values. */
export function normalizeFilters(search: Record<string, unknown>): FileFilters {
  return {
    prefix: typeof search.prefix === 'string' ? search.prefix : DEFAULT_FILTERS.prefix,
    type: typeof search.type === 'string' ? search.type : DEFAULT_FILTERS.type,
    sort: SORT_FIELDS.includes(search.sort as SortFieldInput)
      ? (search.sort as SortFieldInput)
      : DEFAULT_FILTERS.sort,
    order: SORT_ORDERS.includes(search.order as SortOrderInput)
      ? (search.order as SortOrderInput)
      : DEFAULT_FILTERS.order,
  }
}
