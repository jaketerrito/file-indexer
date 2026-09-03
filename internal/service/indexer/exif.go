package indexer

import (
	"bytes"
	"context"
	"errors"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/evanoberholster/imagemeta"
	"github.com/evanoberholster/imagemeta/meta/exif"
	"github.com/evanoberholster/imagemeta/meta/xmp"

	"github.com/jackc/pgx/v5/pgtype"
)

// ExifResult is the exif index type's output: EXIF and XMP metadata
// extracted from an image or camera-RAW file. Skipped marks files that carry
// neither EXIF nor XMP metadata (or are not a supported type at all) — never
// retried, mirroring PreviewResult's policy. Every field beyond ImageType is
// a pointer/nil-able zero value so "absent" (nil) can be told apart from a
// legitimate zero (ISO 0, rating 0, etc); string fields use "" for absent
// since EXIF/XMP text fields have no meaningful legitimate empty value.
type ExifResult struct {
	Skipped bool

	ImageType string

	CameraMake   string
	CameraModel  string
	CameraSerial string
	LensMake     string
	LensModel    string

	// TakenAt is the camera's own wall-clock reading (EXIF carries no time
	// zone), preferring DateTimeOriginal, then CreateDate/Digitized, then
	// ModifyDate — see exif.Exif.SelectedDate.
	TakenAt *time.Time

	ISO             *int32
	FNumber         *float32
	ExposureTime    *float32 // seconds
	FocalLength     *float32 // millimeters
	FocalLength35mm *float32 // millimeters, 35mm-equivalent
	ExposureProgram *int16
	MeteringMode    *int16
	Flash           *int16

	Orientation *int16
	ImageWidth  *int32
	ImageHeight *int32

	GPSLatitude  *float64
	GPSLongitude *float64
	GPSAltitude  *float32
	GPSAt        *time.Time

	Software         string
	Artist           string
	Copyright        string
	ImageDescription string

	XMPTitle       string
	XMPDescription string
	XMPCreator     string
	XMPLabel       string
	XMPRating      *int16
	XMPKeywords    []string
	XMPCreateDate  *time.Time

	// HasExif and HasXMP record which source(s) actually contributed data.
	// A file can carry XMP with no EXIF at all (common for Lightroom-edited
	// JPEGs) or vice versa; StoreExifResult writes no row when both are
	// false.
	HasExif bool
	HasXMP  bool
}

// ExifObjectStore is the slice of storage.Storage the exif index type
// depends on. Deliberately separate from PreviewObjectStore: this index type
// never writes to storage.
type ExifObjectStore interface {
	Stat(ctx context.Context, key string) (storage.ObjectInfo, error)
	Get(ctx context.Context, key string) (*storage.Object, error)
}

// ExifConfig mirrors config.ExifConfig so this package stays decoupled from
// application config; the composition root converts by direct cast, as
// cmd/index-stat does for Config.
type ExifConfig struct {
	// MaxHeaderBytes bounds the first read attempt. EXIF/XMP metadata lives
	// near the start of every modern format measured (JPEG, HEIC, AVIF,
	// CR2, NEF, ARW all resolved within ~200KB), so this is sized generously
	// above that rather than to fit any single format exactly.
	MaxHeaderBytes int64
	// MaxSourceBytes bounds the fallback full read used only when the
	// header read was truncated and yielded no metadata (legacy formats
	// like Canon CRW keep their directory at end-of-file).
	MaxSourceBytes int64
}

// ExifIndexer extracts EXIF and XMP metadata from image and camera-RAW
// files.
type ExifIndexer struct {
	storage     ExifObjectStore
	indexPrefix string
	cfg         ExifConfig
}

// defaultMaxHeaderBytes/defaultMaxSourceBytes back-stop a misconfigured
// ExifConfig (e.g. EXIF_MAX_HEADER_BYTES=0 from an operator's typo): without
// a floor, a non-positive limit makes readBounded return no bytes at all for
// every file, silently marking the entire bucket "no EXIF" with no error to
// surface the misconfiguration.
const (
	defaultMaxHeaderBytes = 1 << 20
	defaultMaxSourceBytes = 64 << 20
)

// NewExifIndexer constructs an ExifIndexer with its dependencies already
// built by the caller (composition root).
func NewExifIndexer(store ExifObjectStore, indexPrefix string, cfg ExifConfig) *ExifIndexer {
	if cfg.MaxHeaderBytes <= 0 {
		cfg.MaxHeaderBytes = defaultMaxHeaderBytes
	}
	if cfg.MaxSourceBytes <= 0 {
		cfg.MaxSourceBytes = defaultMaxSourceBytes
	}
	return &ExifIndexer{storage: store, indexPrefix: indexPrefix, cfg: cfg}
}

// exifExtensions supplements the content-type gate: S3/MinIO commonly report
// camera-RAW objects as application/octet-stream since they have no
// registered MIME type, so a content-type-only gate (as PreviewIndexer uses)
// would silently skip every RAW file.
var exifExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true,
	".tif": true, ".tiff": true,
	".heic": true, ".heif": true, ".avif": true,
	".cr2": true, ".cr3": true, ".crw": true,
	".dng": true, ".nef": true, ".arw": true, ".rw2": true,
}

// isExifCandidate reports whether key/contentType is a format the metadata
// parser supports, without downloading anything.
func isExifCandidate(key, contentType string) bool {
	if strings.HasPrefix(contentType, "image/") {
		return true
	}
	return exifExtensions[strings.ToLower(path.Ext(key))]
}

// Process implements ProcessFunc[ExifResult]. Only I/O failures return an
// error, so only those are retried with backoff; anything inherent to the
// file's content — unsupported type, no metadata present, corrupt bytes —
// returns a skipped result instead, following PreviewIndexer's policy
// (preview.go).
func (x *ExifIndexer) Process(ctx context.Context, job Job) (ExifResult, error) {
	// Defensive: an EXIF row for a derived object (a generated preview) is
	// never wanted. The crawler already skips this prefix, so this only
	// fires for rows that predate the ignore rule — see preview.go's
	// identical guard.
	if x.indexPrefix != "" && strings.HasPrefix(job.Key, x.indexPrefix) {
		return skipExif(job, "key is under the index prefix"), nil
	}

	info, err := x.storage.Stat(ctx, job.Key)
	if err != nil {
		return ExifResult{}, err
	}
	if !isExifCandidate(job.Key, info.ContentType) {
		return skipExif(job, "content type "+info.ContentType+" and extension are not a supported image/RAW type"), nil
	}

	data, truncated, err := x.readBounded(ctx, job.Key, x.cfg.MaxHeaderBytes)
	if err != nil {
		return ExifResult{}, err
	}

	outcome := parseExifXMP(job, data)
	// Escalate to a full, bounded re-download only when the header read
	// actually left data on the table (truncated) AND the specific parser
	// that came back empty is also the one that hit an inconclusive error
	// ("ran out of bytes", not "this file has none of this metadata"). A
	// parser that already found something doesn't need re-running just
	// because the other one didn't, and a parser that conclusively found
	// nothing (err == nil, or its own definitive not-present sentinel)
	// won't find anything from more bytes either — most images have no
	// EXIF or no XMP at all, and re-downloading up to MaxSourceBytes for
	// every one of them would be a large, pointless cost. Legacy formats
	// like Canon CRW (directory at end-of-file) are exactly the case this
	// exists for.
	needsExif := outcome.exifIncomplete && !outcome.result.HasExif
	needsXMP := outcome.xmpIncomplete && !outcome.result.HasXMP
	if truncated && x.cfg.MaxSourceBytes > x.cfg.MaxHeaderBytes && (needsExif || needsXMP) {
		full, _, err := x.readBounded(ctx, job.Key, x.cfg.MaxSourceBytes)
		if err != nil {
			return ExifResult{}, err
		}
		outcome.result = mergeOutcomes(outcome.result, parseExifXMP(job, full).result)
	}

	if !outcome.result.HasExif && !outcome.result.HasXMP {
		return skipExif(job, "no EXIF or XMP metadata present"), nil
	}
	return outcome.result, nil
}

// mergeOutcomes combines a header-read result with an escalated (full-read)
// result, never letting the escalated pass erase what the header pass
// already found. The escalated parse reads a strict superset of the header
// bytes, so in the ordinary case it can only add information — but a panic
// specifically on the larger buffer (recovered in parseExifXMP as a zero
// outcome) must not discard metadata the header pass already found. Each
// half (EXIF-derived, XMP-derived) is copied over independently, so
// escalation finding one doesn't clobber the other half if the header pass
// had already found it.
func mergeOutcomes(base, escalated ExifResult) ExifResult {
	if escalated.HasExif {
		copyEXIFFields(&base, escalated)
	}
	if escalated.HasXMP {
		copyXMPFields(&base, escalated)
	}
	return base
}

// copyEXIFFields copies only the EXIF-derived fields of src into dst (and
// sets HasExif), leaving any XMP-derived fields already in dst untouched.
func copyEXIFFields(dst *ExifResult, src ExifResult) {
	dst.ImageType = src.ImageType
	dst.CameraMake = src.CameraMake
	dst.CameraModel = src.CameraModel
	dst.CameraSerial = src.CameraSerial
	dst.LensMake = src.LensMake
	dst.LensModel = src.LensModel
	dst.TakenAt = src.TakenAt
	dst.ISO = src.ISO
	dst.FNumber = src.FNumber
	dst.ExposureTime = src.ExposureTime
	dst.FocalLength = src.FocalLength
	dst.FocalLength35mm = src.FocalLength35mm
	dst.ExposureProgram = src.ExposureProgram
	dst.MeteringMode = src.MeteringMode
	dst.Flash = src.Flash
	dst.Orientation = src.Orientation
	dst.ImageWidth = src.ImageWidth
	dst.ImageHeight = src.ImageHeight
	dst.GPSLatitude = src.GPSLatitude
	dst.GPSLongitude = src.GPSLongitude
	dst.GPSAltitude = src.GPSAltitude
	dst.GPSAt = src.GPSAt
	dst.Software = src.Software
	dst.Artist = src.Artist
	dst.Copyright = src.Copyright
	dst.ImageDescription = src.ImageDescription
	dst.HasExif = true
}

// copyXMPFields copies only the XMP-derived fields of src into dst (and
// sets HasXMP), leaving any EXIF-derived fields already in dst untouched.
func copyXMPFields(dst *ExifResult, src ExifResult) {
	dst.XMPTitle = src.XMPTitle
	dst.XMPDescription = src.XMPDescription
	dst.XMPCreator = src.XMPCreator
	dst.XMPLabel = src.XMPLabel
	dst.XMPRating = src.XMPRating
	dst.XMPKeywords = src.XMPKeywords
	dst.XMPCreateDate = src.XMPCreateDate
	dst.HasXMP = true
}

// readBounded downloads key and reads at most limit bytes, reporting whether
// the object had more data beyond that (one extra byte is requested past
// limit purely to detect this, then trimmed).
func (x *ExifIndexer) readBounded(ctx context.Context, key string, limit int64) (data []byte, truncated bool, err error) {
	if limit <= 0 {
		return nil, false, nil
	}

	obj, err := x.storage.Get(ctx, key)
	if err != nil {
		return nil, false, err
	}
	defer func() {
		if cerr := obj.Reader.Close(); cerr != nil {
			slog.Warn("close source object", "key", key, "error", cerr)
		}
	}()

	data, err = io.ReadAll(io.LimitReader(obj.Reader, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

// skipExif records why a file gets no metadata row and returns the sentinel
// result. Logged at warn because a skip is a silent absence from the index.
func skipExif(job Job, reason string) ExifResult {
	slog.Warn("exif skipped", "key", job.Key, "fileID", job.FileID, "reason", reason)
	return ExifResult{Skipped: true}
}

// parseOutcome is parseExifXMP's return value: the extracted result, plus
// whether either parser's failure looked like "ran out of bytes" rather
// than "this file conclusively has none of this metadata". Process uses the
// latter to decide whether re-downloading more of the file could possibly
// help.
type parseOutcome struct {
	result         ExifResult
	exifIncomplete bool
	xmpIncomplete  bool
}

// parseExifXMP decodes both EXIF and XMP from the same bytes — two
// independent passes, since imagemeta.Decode and xmp.Parse each need their
// own read from the start. A panic anywhere during decoding (untrusted
// input; the parser is not guaranteed panic-free on arbitrary bytes) is
// recovered and treated as "no metadata" for the whole file rather than
// partially-parsed data, so a skip is always all-or-nothing.
func parseExifXMP(job Job, data []byte) (outcome parseOutcome) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("exif/xmp parser panic recovered", "key", job.Key, "fileID", job.FileID, "panic", r)
			outcome = parseOutcome{}
		}
	}()

	switch e, err := imagemeta.Decode(bytes.NewReader(data)); {
	case err == nil:
		applyExif(&outcome.result, e)
	case errors.Is(err, imagemeta.ErrNoExif), errors.Is(err, imagemeta.ErrImageTypeNotFound):
		// Conclusive: this file has no EXIF (or isn't a recognized
		// container at all), and reading more of it won't change that.
	default:
		// Some other error — most commonly an EOF-shaped one from data
		// ending mid-structure. Rather than pattern-match every parser's
		// exact error strings (verified unstable across formats: plain
		// io.EOF, "no JPEG Marker", etc.), treat any non-sentinel error as
		// "inconclusive, more bytes might resolve it".
		outcome.exifIncomplete = true
		slog.Debug("exif decode inconclusive", "key", job.Key, "fileID", job.FileID, "error", err)
	}

	switch parsed, err := xmp.Parse(bytes.NewReader(data)); {
	case err == nil:
		applyXMP(&outcome.result, parsed)
	case errors.Is(err, xmp.ErrNoXMP):
		// Conclusive: no XMP packet found.
	default:
		outcome.xmpIncomplete = true
		slog.Debug("xmp parse inconclusive", "key", job.Key, "fileID", job.FileID, "error", err)
	}

	return outcome
}

// applyExif copies fields worth persisting out of a decoded exif.Exif into
// result and sets HasExif.
//
// HasExif is computed from a definitive subset of fields only — deliberately
// excluding Orientation, ImageWidth/Height, and GPSAltitude: measurement
// against imagemeta's own test images showed these can be populated from
// container structure (JPEG SOF dimensions, a default Orientation tag) even
// on files with no other EXIF data at all, which would make an
// empty-looking file register as having metadata.
func applyExif(result *ExifResult, e exif.Exif) {
	result.ImageType = e.ImageType.String()
	result.CameraMake = sanitize(e.IFD0.Make)
	result.CameraModel = sanitize(e.IFD0.Model)
	result.CameraSerial = sanitize(e.ExifIFD.BodySerialNumber)
	result.LensMake = sanitize(e.ExifIFD.LensMake)
	result.LensModel = sanitize(e.ExifIFD.LensModel)
	result.Software = sanitize(e.IFD0.Software)
	result.Artist = sanitize(e.IFD0.Artist)
	result.Copyright = sanitize(e.IFD0.Copyright)
	result.ImageDescription = sanitize(e.IFD0.ImageDescription)

	if t := e.SelectedDate(); !t.IsZero() {
		result.TakenAt = &t
	}
	if v := int32(e.ExifIFD.ISOSpeedRatings); v != 0 {
		result.ISO = &v
	}
	if f := float32(e.ExifIFD.FNumber); f != 0 {
		result.FNumber = &f
	} else if f := float32(e.ExifIFD.ApertureValue); f != 0 {
		result.FNumber = &f
	}
	if f := float32(e.ExifIFD.ExposureTime); f != 0 {
		result.ExposureTime = &f
	}
	if f := float32(e.ExifIFD.FocalLength); f != 0 {
		result.FocalLength = &f
	}
	if f := float32(e.ExifIFD.FocalLengthIn35mmFormat); f != 0 {
		result.FocalLength35mm = &f
	}
	// GPSLatitude/GPSLongitude always arrive together in real EXIF (a
	// GPSLatitudeRef+GPSLatitude pair and a GPSLongitudeRef+GPSLongitude
	// pair). Requiring both non-zero avoids fabricating a coordinate on the
	// equator/prime-meridian when only one half of the pair actually
	// parsed.
	if lat, lon := e.GPS.Latitude(), e.GPS.Longitude(); lat != 0 && lon != 0 {
		result.GPSLatitude = &lat
		result.GPSLongitude = &lon
	}
	if t := e.GPS.GPSTimestamp(); !t.IsZero() {
		// GPSTimeStamp/GPSDateStamp are defined by the EXIF spec as UTC
		// (unlike DateTimeOriginal, which is the camera's naive local
		// clock) — force it explicitly rather than trust whatever
		// Location the parsed time.Time happens to carry.
		t = t.UTC()
		result.GPSAt = &t
	}
	// Orientation values are spec'd 1-8; 0 is not a valid EXIF orientation,
	// so unlike the enums below, 0 unambiguously means "tag absent" even
	// without corroborating fields.
	if v := int16(e.IFD0.Orientation); v != 0 {
		result.Orientation = &v
	}
	if v := int32(e.ExifIFD.PixelXDimension); v != 0 {
		result.ImageWidth = &v
	} else if v := int32(e.IFD0.ImageWidth); v != 0 {
		result.ImageWidth = &v
	}
	if v := int32(e.ExifIFD.PixelYDimension); v != 0 {
		result.ImageHeight = &v
	} else if v := int32(e.IFD0.ImageHeight); v != 0 {
		result.ImageHeight = &v
	}

	result.HasExif = result.CameraMake != "" || result.CameraModel != "" || result.CameraSerial != "" ||
		result.LensMake != "" || result.LensModel != "" ||
		result.TakenAt != nil || result.ISO != nil || result.FNumber != nil || result.ExposureTime != nil ||
		result.FocalLength != nil || result.GPSLatitude != nil ||
		result.Software != "" || result.Artist != "" || result.Copyright != "" || result.ImageDescription != ""

	// ExposureProgram, MeteringMode, and Flash are all EXIF enums whose
	// zero value ("Not Defined", "Unknown", "did not fire") is itself a
	// common, legitimate reading — 0 is not a reliable "tag absent" signal
	// the way it is for Orientation above. imagemeta does not expose a
	// per-tag presence bitset for these three (only for its time tags), so
	// non-zero values are always trusted, but a zero is only trusted once
	// some other definitive field has already confirmed a real EXIF block
	// is present; otherwise it's left nil rather than risk manufacturing
	// "flash did not fire" out of a file with no EXIF at all.
	if v := int16(e.ExifIFD.ExposureProgram); v != 0 {
		result.ExposureProgram = &v
	} else if result.HasExif {
		result.ExposureProgram = &v
	}
	if v := int16(e.ExifIFD.MeteringMode); v != 0 {
		result.MeteringMode = &v
	} else if result.HasExif {
		result.MeteringMode = &v
	}
	if v := int16(e.ExifIFD.Flash); v != 0 {
		result.Flash = &v
	} else if result.HasExif {
		result.Flash = &v
	}
	// Same reasoning for GPSAltitude: 0 legitimately means "sea level", but
	// is only trustworthy once GPSLatitude/GPSLongitude already confirmed a
	// real GPS block.
	if alt := e.GPS.Altitude(); alt != 0 {
		result.GPSAltitude = &alt
	} else if result.GPSLatitude != nil {
		result.GPSAltitude = &alt
	}
}

// applyXMP copies fields worth persisting out of a parsed xmp.XMP into
// result and sets HasXMP.
func applyXMP(result *ExifResult, x xmp.XMP) {
	if len(x.DC.Title) > 0 {
		result.XMPTitle = sanitize(x.DC.Title[0])
	}
	if len(x.DC.Description) > 0 {
		result.XMPDescription = sanitize(x.DC.Description[0])
	}
	if len(x.DC.Creator) > 0 {
		result.XMPCreator = sanitize(strings.Join(x.DC.Creator, "; "))
	}
	result.XMPLabel = sanitize(x.Basic.Label)
	if x.Basic.Rating != 0 {
		v := int16(x.Basic.Rating)
		result.XMPRating = &v
	}
	if len(x.DC.Subject) > 0 {
		n := min(len(x.DC.Subject), maxKeywordCount)
		keywords := make([]string, 0, n)
		for _, s := range x.DC.Subject[:n] {
			if s := sanitize(s); s != "" {
				keywords = append(keywords, s)
			}
		}
		if len(keywords) > 0 {
			result.XMPKeywords = keywords
		}
	}
	if !x.Basic.CreateDate.IsZero() {
		t := x.Basic.CreateDate
		result.XMPCreateDate = &t
	}

	result.HasXMP = result.XMPTitle != "" || result.XMPDescription != "" || result.XMPCreator != "" ||
		result.XMPLabel != "" || result.XMPRating != nil || len(result.XMPKeywords) > 0 || result.XMPCreateDate != nil
}

// maxSanitizedFieldLen caps free-text fields before storage, counted in
// runes (not bytes — a rune can be up to 4 bytes in UTF-8, so this bounds a
// column to at most 4*maxSanitizedFieldLen bytes, not exactly that many).
// MakerNote decoding is not always reliable (imagemeta's own known issue
// list includes a reverse-offset bug affecting some Sony fields — tracked
// as a known limitation in TODO.md), so a generous but finite cap guards
// against a misparsed field dumping unbounded or binary-looking data into a
// text column.
const maxSanitizedFieldLen = 512

// maxKeywordCount caps the number of XMP dc:subject entries stored per file.
// Each entry is itself capped by maxSanitizedFieldLen, but a crafted or
// buggy-generator XMP packet could otherwise put an unbounded number of
// short entries into one TEXT[] row.
const maxKeywordCount = 64

// sanitize strips trailing NUL padding (EXIF ASCII fields are fixed-width,
// NUL-padded), collapses any run of whitespace — including newlines/tabs and
// non-ASCII spaces like NBSP, which unicode.IsPrint treats as unprintable —
// into a single ' ' (rather than deleting it and jamming words together),
// drops other non-printable runes, and caps length. Applied to every
// free-text EXIF/XMP field before it reaches the database.
func sanitize(s string) string {
	s = strings.TrimRight(s, "\x00")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s))
	count := 0
	lastWasSpace := false
	for _, r := range s {
		if count >= maxSanitizedFieldLen {
			break
		}
		if unicode.IsSpace(r) {
			r = ' '
		}
		if !unicode.IsPrint(r) {
			continue
		}
		if r == ' ' && lastWasSpace {
			continue
		}
		b.WriteRune(r)
		lastWasSpace = r == ' '
		count++
	}
	return strings.TrimSpace(b.String())
}

// StoreExifResult is the exif index type's StoreFunc: persists the result
// row inside Complete's transaction. Skipped results write nothing — the
// queue row still flips to done, so the file is not reprocessed until its
// marked_at changes.
func StoreExifResult(ctx context.Context, q *db.Queries, fileID int64, result ExifResult) error {
	if result.Skipped {
		return nil
	}
	return q.UpsertIndexExifResult(ctx, db.UpsertIndexExifResultParams{
		FileID:           fileID,
		ImageType:        pgText(result.ImageType),
		CameraMake:       pgText(result.CameraMake),
		CameraModel:      pgText(result.CameraModel),
		CameraSerial:     pgText(result.CameraSerial),
		LensMake:         pgText(result.LensMake),
		LensModel:        pgText(result.LensModel),
		TakenAt:          pgTimestamp(result.TakenAt),
		Iso:              pgInt4(result.ISO),
		FNumber:          pgFloat4(result.FNumber),
		ExposureTime:     pgFloat4(result.ExposureTime),
		FocalLength:      pgFloat4(result.FocalLength),
		FocalLength35mm:  pgFloat4(result.FocalLength35mm),
		ExposureProgram:  pgInt2(result.ExposureProgram),
		MeteringMode:     pgInt2(result.MeteringMode),
		Flash:            pgInt2(result.Flash),
		Orientation:      pgInt2(result.Orientation),
		ImageWidth:       pgInt4(result.ImageWidth),
		ImageHeight:      pgInt4(result.ImageHeight),
		GpsLatitude:      pgFloat8(result.GPSLatitude),
		GpsLongitude:     pgFloat8(result.GPSLongitude),
		GpsAltitude:      pgFloat4(result.GPSAltitude),
		GpsAt:            pgTimestamptz(result.GPSAt),
		Software:         pgText(result.Software),
		Artist:           pgText(result.Artist),
		Copyright:        pgText(result.Copyright),
		ImageDescription: pgText(result.ImageDescription),
		XmpTitle:         pgText(result.XMPTitle),
		XmpDescription:   pgText(result.XMPDescription),
		XmpCreator:       pgText(result.XMPCreator),
		XmpLabel:         pgText(result.XMPLabel),
		XmpRating:        pgInt2(result.XMPRating),
		XmpKeywords:      result.XMPKeywords,
		XmpCreateDate:    pgTimestamptz(result.XMPCreateDate),
		HasExif:          result.HasExif,
		HasXmp:           result.HasXMP,
	})
}

// pgText converts s to pgtype.Text, treating "" as NULL: EXIF/XMP text
// fields have no meaningful legitimate empty value.
func pgText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func pgInt4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

func pgInt2(v *int16) pgtype.Int2 {
	if v == nil {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: *v, Valid: true}
}

func pgFloat4(v *float32) pgtype.Float4 {
	if v == nil {
		return pgtype.Float4{}
	}
	return pgtype.Float4{Float32: *v, Valid: true}
}

func pgFloat8(v *float64) pgtype.Float8 {
	if v == nil {
		return pgtype.Float8{}
	}
	return pgtype.Float8{Float64: *v, Valid: true}
}

// pgTimestamp converts a naive (zone-less) time, used for TakenAt: EXIF
// DateTimeOriginal carries no offset, so this must not attach one.
func pgTimestamp(t *time.Time) pgtype.Timestamp {
	if t == nil {
		return pgtype.Timestamp{}
	}
	return pgtype.Timestamp{Time: *t, Valid: true}
}

// pgTimestamptz converts a zone-aware time, used for GPSAt (spec'd UTC) and
// XMPCreateDate (carries a real parsed offset) — see the column comments in
// migrations/003_exif.sql for why these two differ from TakenAt.
func pgTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}
