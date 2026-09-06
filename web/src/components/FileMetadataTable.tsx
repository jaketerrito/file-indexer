import type { ExifMetadataDto, FileMetadataDto } from '../server/impl'

/** One label/value row; omitted entirely when value is null/undefined/empty. */
function Row({ label, value }: { label: string; value: string | number | null | undefined }) {
  if (value === null || value === undefined || value === '') return null
  return (
    <tr>
      <th>{label}</th>
      <td>{value}</td>
    </tr>
  )
}

/** Exported for reuse by the delete-folder confirmation (DeleteFolderConfirmation.tsx). */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB', 'TB']
  let value = bytes / 1024
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(1)} ${units[unit]}`
}

function ExifSection({ exif }: { exif: ExifMetadataDto }) {
  return (
    <>
      {exif.hasExif ? (
        <>
          <h3>Camera (EXIF)</h3>
          <table>
            <tbody>
              <Row label="Image type" value={exif.imageType} />
              <Row label="Camera make" value={exif.cameraMake} />
              <Row label="Camera model" value={exif.cameraModel} />
              <Row label="Camera serial" value={exif.cameraSerial} />
              <Row label="Lens make" value={exif.lensMake} />
              <Row label="Lens model" value={exif.lensModel} />
              <Row label="Taken at" value={exif.takenAt} />
              <Row label="ISO" value={exif.iso} />
              <Row label="F-number" value={exif.fNumber ? `f/${exif.fNumber}` : undefined} />
              <Row
                label="Exposure time"
                value={exif.exposureTime ? `${exif.exposureTime}s` : undefined}
              />
              <Row
                label="Focal length"
                value={exif.focalLength ? `${exif.focalLength}mm` : undefined}
              />
              <Row
                label="Focal length (35mm equiv.)"
                value={exif.focalLength35mm ? `${exif.focalLength35mm}mm` : undefined}
              />
              <Row label="Exposure program" value={exif.exposureProgram} />
              <Row label="Metering mode" value={exif.meteringMode} />
              <Row label="Flash" value={exif.flash} />
              <Row label="Orientation" value={exif.orientation} />
              <Row label="Image width" value={exif.imageWidth} />
              <Row label="Image height" value={exif.imageHeight} />
              <Row label="Software" value={exif.software} />
              <Row label="Artist" value={exif.artist} />
              <Row label="Copyright" value={exif.copyright} />
              <Row label="Description" value={exif.imageDescription} />
            </tbody>
          </table>
          {exif.gpsLatitude !== undefined && exif.gpsLongitude !== undefined ? (
            <>
              <h3>GPS</h3>
              <table>
                <tbody>
                  <Row label="Latitude" value={exif.gpsLatitude} />
                  <Row label="Longitude" value={exif.gpsLongitude} />
                  <Row
                    label="Altitude"
                    value={exif.gpsAltitude ? `${exif.gpsAltitude}m` : undefined}
                  />
                  <Row label="GPS time" value={exif.gpsAt} />
                </tbody>
              </table>
            </>
          ) : null}
        </>
      ) : null}
      {exif.hasXmp ? (
        <>
          <h3>XMP</h3>
          <table>
            <tbody>
              <Row label="Title" value={exif.xmpTitle} />
              <Row label="Description" value={exif.xmpDescription} />
              <Row label="Creator" value={exif.xmpCreator} />
              <Row label="Label" value={exif.xmpLabel} />
              <Row label="Rating" value={exif.xmpRating} />
              <Row
                label="Keywords"
                value={exif.xmpKeywords.length > 0 ? exif.xmpKeywords.join(', ') : undefined}
              />
              <Row label="Created" value={exif.xmpCreateDate} />
            </tbody>
          </table>
        </>
      ) : null}
    </>
  )
}

interface FileMetadataTableProps {
  file: FileMetadataDto
}

/**
 * Metadata display for the standalone file page (routes/file.$id.tsx): the
 * base stat columns plus EXIF/XMP sections when present. Also the home of
 * formatBytes, reused by the delete-folder confirmation.
 */
export function FileMetadataTable({ file }: FileMetadataTableProps) {
  return (
    <>
      <table>
        <tbody>
          <Row label="Content type" value={file.contentType} />
          <Row label="Size" value={formatBytes(file.sizeBytes)} />
          <Row label="Created" value={file.createdAt} />
          <Row label="Updated" value={file.updatedAt} />
        </tbody>
      </table>
      {file.exif ? <ExifSection exif={file.exif} /> : null}
    </>
  )
}
