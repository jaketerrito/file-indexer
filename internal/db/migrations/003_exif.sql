-- +goose Up

-- index_exif_result holds the exif index type's output: EXIF and XMP
-- metadata extracted from image and camera-RAW files (JPEG, TIFF, PNG,
-- HEIC/HEIF/AVIF, CR2/CR3/CRW, DNG, NEF, ARW, RW2). Every column is
-- nullable, unlike index_stat_result's NOT NULL columns: EXIF/XMP fields
-- are sparse by nature (most cameras omit most tags) and a NULL must be
-- distinguishable from a legitimate zero value (e.g. ISO 0, rating 0).
-- Files with neither EXIF nor XMP present are marked done in index_queue
-- with no row here, same absent-row convention as index_preview_result.
CREATE TABLE IF NOT EXISTS index_exif_result (
    file_id BIGINT PRIMARY KEY REFERENCES files (id) ON DELETE CASCADE,

    -- Sniffed image type from the metadata parser (e.g.
    -- "image/x-canon-cr2"), which distinguishes camera RAW formats that
    -- object storage reports only as application/octet-stream. Deliberately
    -- separate from index_stat_result.content_type, which is what S3
    -- reports and what the browser/API use for serving.
    image_type TEXT,

    -- Camera / lens attribution (EXIF IFD0 + ExifIFD).
    camera_make   TEXT,
    camera_model  TEXT,
    camera_serial TEXT,
    lens_make     TEXT,
    lens_model    TEXT,

    -- Naive (zone-less) local capture time as recorded by the camera clock.
    -- Deliberately TIMESTAMP, not TIMESTAMPTZ like every other timestamp in
    -- this schema: EXIF DateTimeOriginal carries no offset, so attaching one
    -- (UTC or otherwise) would fabricate a timezone the source never
    -- specified and shift what the photographer's wall clock actually read.
    taken_at TIMESTAMP,

    -- Exposure settings.
    iso                INT,
    f_number           REAL,
    exposure_time      REAL, -- seconds
    focal_length       REAL, -- millimeters
    focal_length_35mm  REAL, -- millimeters, 35mm-equivalent
    exposure_program   SMALLINT,
    metering_mode      SMALLINT,
    flash              SMALLINT,

    -- Image properties as recorded in EXIF, which may differ from the
    -- decoded pixel dimensions previewed by index_preview_result (e.g. a
    -- RAW's embedded preview vs. its sensor dimensions).
    orientation   SMALLINT,
    image_width   INT,
    image_height  INT,

    -- GPS. Full precision is kept because this is a private, single-tenant
    -- bucket; if this index type is ever exposed multi-tenant or to
    -- external clients, revisit before joining these columns into any
    -- shared view or API response (see file_infos / proto FileInfo).
    -- Range-checked (NULL passes either way) as a defensive backstop:
    -- MakerNote/GPS-adjacent decoding is not always reliable (see the
    -- sanitize() comment on the known Sony field-garbling issue in
    -- internal/service/indexer/exif.go), and a garbage coordinate is
    -- otherwise silently accepted.
    gps_latitude  DOUBLE PRECISION CHECK (gps_latitude BETWEEN -90 AND 90),
    gps_longitude DOUBLE PRECISION CHECK (gps_longitude BETWEEN -180 AND 180),
    gps_altitude  REAL,
    -- Unlike taken_at, GPSTimeStamp/GPSDateStamp are unambiguously defined
    -- by the EXIF spec as UTC, so this is TIMESTAMPTZ (not naive) on purpose.
    gps_at        TIMESTAMPTZ,

    -- Free-text attribution fields, sanitized (non-printable bytes
    -- stripped, length-capped) before storage: MakerNote decoding is not
    -- always reliable (see internal/service/indexer/exif.go) and this
    -- column accepts whatever the camera firmware wrote.
    software          TEXT,
    artist            TEXT,
    copyright         TEXT,
    image_description TEXT,

    -- XMP (sidecar or embedded). Distinct from the EXIF fields above: a
    -- file can carry XMP with no EXIF at all (e.g. edited-in-Lightroom
    -- JPEGs commonly do).
    xmp_title       TEXT,
    xmp_description TEXT,
    xmp_creator     TEXT,
    xmp_label       TEXT,
    xmp_rating      SMALLINT,
    xmp_keywords    TEXT[],
    -- Unlike EXIF DateTimeOriginal, XMP xmp:CreateDate is an ISO-8601 string
    -- that does carry a zone offset, so this is TIMESTAMPTZ (not naive) on
    -- purpose — the opposite of taken_at's reasoning.
    xmp_create_date TIMESTAMPTZ,

    -- Which of EXIF/XMP contributed to this row, so an absent EXIF field
    -- can be told apart from "no EXIF block was present at all".
    has_exif BOOLEAN NOT NULL DEFAULT false,
    has_xmp  BOOLEAN NOT NULL DEFAULT false,

    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Sort/filter support for the obvious future search use case (sort by
-- capture date). Partial: most rows will have no EXIF at all.
CREATE INDEX IF NOT EXISTS index_exif_result_taken_at_idx ON index_exif_result (taken_at) WHERE taken_at IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS index_exif_result;
