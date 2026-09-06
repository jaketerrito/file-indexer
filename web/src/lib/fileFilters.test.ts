import { describe, expect, it } from 'vitest'
import { DEFAULT_FILTERS, isBrowsing, normalizeFilters, toBrowsePath } from './fileFilters'

describe('normalizeFilters', () => {
  it('returns defaults for empty search params', () => {
    expect(normalizeFilters({})).toEqual(DEFAULT_FILTERS)
  })

  it('keeps valid values', () => {
    expect(
      normalizeFilters({ prefix: 'docs/', type: 'image/', sort: 'size', order: 'desc' }),
    ).toEqual({
      prefix: 'docs/',
      type: 'image/',
      sort: 'size',
      order: 'desc',
    })
  })

  it('drops invalid values back to defaults', () => {
    expect(normalizeFilters({ prefix: 42, type: ['image/'], sort: 'bogus', order: 'up' })).toEqual(
      DEFAULT_FILTERS,
    )
  })
})

describe('normalizeFilters round-trip', () => {
  it('is stable when re-parsing its own output', () => {
    const filters = normalizeFilters({
      prefix: 'a',
      type: 'text/',
      sort: 'lastModified',
      order: 'desc',
    })
    expect(normalizeFilters({ ...filters })).toEqual(filters)
  })

  it('is stable for a browse-mode path, including the root ("")', () => {
    const filters = normalizeFilters({ path: '' })
    expect(filters.path).toBe('')
    expect(normalizeFilters({ ...filters })).toEqual(filters)

    const nested = normalizeFilters({ path: 'docs/sub/' })
    expect(nested.path).toBe('docs/sub/')
    expect(normalizeFilters({ ...nested })).toEqual(nested)
  })
})

describe('isBrowsing', () => {
  it('is false when path is absent (search mode)', () => {
    expect(isBrowsing(normalizeFilters({}))).toBe(false)
    expect(isBrowsing(normalizeFilters({ prefix: 'docs/' }))).toBe(false)
  })

  it('is true when path is present, even at the root', () => {
    expect(isBrowsing(normalizeFilters({ path: '' }))).toBe(true)
    expect(isBrowsing(normalizeFilters({ path: 'docs/' }))).toBe(true)
  })
})

describe('toBrowsePath', () => {
  it('sets path and clears prefix/type', () => {
    const filters = normalizeFilters({ prefix: 'old/', type: 'image/' })
    const next = toBrowsePath(filters, 'docs/')
    expect(next.path).toBe('docs/')
    expect(next.prefix).toBe('')
    expect(next.type).toBe('')
    // Sort/order are preserved across the navigation.
    expect(next.sort).toBe(filters.sort)
    expect(next.order).toBe(filters.order)
  })
})
