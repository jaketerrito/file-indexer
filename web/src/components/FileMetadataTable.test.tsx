import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { ExifMetadataDto, FileMetadataDto } from '../server/impl'
import { FileMetadataTable, formatBytes } from './FileMetadataTable'

function dto(overrides: Partial<FileMetadataDto> = {}): FileMetadataDto {
  return {
    id: '1',
    key: 'docs/notes.txt',
    contentType: 'text/plain',
    sizeBytes: 2048,
    createdAt: '2026-01-02T03:04:05.000Z',
    updatedAt: null,
    exif: null,
    ...overrides,
  }
}

describe('formatBytes', () => {
  it('formats bytes, KB, and MB', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(2048)).toBe('2.0 KB')
    expect(formatBytes(5 * 1024 * 1024)).toBe('5.0 MB')
  })
})

describe('FileMetadataTable', () => {
  it('renders the base rows and omits absent values', () => {
    render(<FileMetadataTable file={dto()} />)
    expect(screen.getByText('Content type')).toBeTruthy()
    expect(screen.getByText('text/plain')).toBeTruthy()
    expect(screen.getByText('2.0 KB')).toBeTruthy()
    expect(screen.getByText('Created')).toBeTruthy()
    // updatedAt is null → the row is omitted entirely.
    expect(screen.queryByText('Updated')).toBeNull()
  })

  it('omits the content-type row when empty (stat indexing pending)', () => {
    render(<FileMetadataTable file={dto({ contentType: '' })} />)
    expect(screen.queryByText('Content type')).toBeNull()
  })

  it('renders EXIF and GPS sections when present', () => {
    const exif: ExifMetadataDto = {
      hasExif: true,
      hasXmp: false,
      xmpKeywords: [],
      cameraMake: 'Canon',
      cameraModel: 'EOS 40D',
      gpsLatitude: 35,
      gpsLongitude: 139,
    }
    render(<FileMetadataTable file={dto({ exif })} />)
    expect(screen.getByText('Camera (EXIF)')).toBeTruthy()
    expect(screen.getByText('Canon')).toBeTruthy()
    expect(screen.getByText('EOS 40D')).toBeTruthy()
    expect(screen.getByText('GPS')).toBeTruthy()
    expect(screen.getByText('Latitude')).toBeTruthy()
    expect(screen.queryByText('XMP')).toBeNull()
  })

  it('renders the XMP section with joined keywords', () => {
    const exif: ExifMetadataDto = {
      hasExif: false,
      hasXmp: true,
      xmpKeywords: ['alpha', 'beta'],
      xmpTitle: 'A title',
    }
    render(<FileMetadataTable file={dto({ exif })} />)
    expect(screen.getByText('XMP')).toBeTruthy()
    expect(screen.getByText('A title')).toBeTruthy()
    expect(screen.getByText('alpha, beta')).toBeTruthy()
    expect(screen.queryByText('Camera (EXIF)')).toBeNull()
  })
})
