import { describe, expect, it } from 'vitest'
import { DEFAULT_FILTERS, normalizeFilters } from './fileFilters'

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
      sort: 'createdAt',
      order: 'desc',
    })
    expect(normalizeFilters({ ...filters })).toEqual(filters)
  })
})
