import { describe, expect, it } from 'vitest'
import { normalizeUploadPath } from './uploadPath'

describe('normalizeUploadPath', () => {
  it('returns empty string for empty/whitespace input (root)', () => {
    expect(normalizeUploadPath('')).toBe('')
    expect(normalizeUploadPath('   ')).toBe('')
  })

  it('appends a trailing slash when missing', () => {
    expect(normalizeUploadPath('docs/housing')).toBe('docs/housing/')
  })

  it('leaves an already-trailing-slash path alone', () => {
    expect(normalizeUploadPath('docs/housing/')).toBe('docs/housing/')
  })

  it('strips leading slashes', () => {
    expect(normalizeUploadPath('/docs/housing')).toBe('docs/housing/')
  })

  it('collapses doubled slashes', () => {
    expect(normalizeUploadPath('docs//housing')).toBe('docs/housing/')
  })

  it('trims surrounding whitespace', () => {
    expect(normalizeUploadPath('  docs/housing  ')).toBe('docs/housing/')
  })
})
