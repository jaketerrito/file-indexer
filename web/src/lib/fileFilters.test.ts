import { describe, expect, it } from 'vitest'
import {
  DEFAULT_BROWSE_FILTERS,
  DEFAULT_SEARCH_FILTERS,
  normalizeBrowseFilters,
  normalizeSearchFilters,
} from './fileFilters'

describe('normalizeBrowseFilters', () => {
  it('returns defaults for empty search params', () => {
    expect(normalizeBrowseFilters({})).toEqual(DEFAULT_BROWSE_FILTERS)
  })

  it('keeps an explicit path, including the root ("")', () => {
    expect(normalizeBrowseFilters({ path: '' }).path).toBe('')
    expect(normalizeBrowseFilters({ path: 'docs/sub/' }).path).toBe('docs/sub/')
  })

  it('drops invalid values back to defaults', () => {
    expect(normalizeBrowseFilters({ path: 42, sort: 'bogus', order: 'up' })).toEqual(
      DEFAULT_BROWSE_FILTERS,
    )
  })

  it('ignores search-route params', () => {
    expect(normalizeBrowseFilters({ query: 'notes', type: 'image/' })).toEqual(
      DEFAULT_BROWSE_FILTERS,
    )
  })

  it('is stable when re-parsing its own output', () => {
    const filters = normalizeBrowseFilters({ path: 'docs/', sort: 'size', order: 'desc' })
    expect(normalizeBrowseFilters({ ...filters })).toEqual(filters)
  })
})

describe('normalizeSearchFilters', () => {
  it('returns defaults for empty search params', () => {
    expect(normalizeSearchFilters({})).toEqual(DEFAULT_SEARCH_FILTERS)
  })

  it('keeps valid values', () => {
    expect(
      normalizeSearchFilters({ query: 'docs/', type: 'image/', sort: 'size', order: 'desc' }),
    ).toEqual({
      query: 'docs/',
      type: 'image/',
      sort: 'size',
      order: 'desc',
    })
  })

  it('drops non-string and unknown values back to defaults', () => {
    expect(
      normalizeSearchFilters({ query: 42, type: ['image/'], sort: 'bogus', order: 'up' }),
    ).toEqual(DEFAULT_SEARCH_FILTERS)
  })

  it('is stable when re-parsing its own output', () => {
    const filters = normalizeSearchFilters({
      query: 'a',
      type: 'text/',
      sort: 'lastModified',
      order: 'desc',
    })
    expect(normalizeSearchFilters({ ...filters })).toEqual(filters)
  })
})
