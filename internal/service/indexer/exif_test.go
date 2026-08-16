package indexer

import (
	"context"
	"errors"
	"file-indexer/internal/storage"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/mock"
)

func defaultExifConfig() ExifConfig {
	return ExifConfig{
		MaxHeaderBytes: 1 << 20,
		MaxSourceBytes: 64 << 20,
	}
}

// fullExifIFD0/exif/gps entries used by the "full metadata" fixture below.
// Values are chosen so every extracted field has a distinguishable,
// hand-verifiable expectation.
func fullExifEntries() (ifd0, exif, gps []tEntry) {
	ifd0 = []tEntry{
		// Make uses a real, recognized manufacturer name ("Canon") rather
		// than a synthetic one: imagemeta normalizes an *unrecognized* Make
		// string to lowercase in place while checking it against its known-
		// camera list (see makernote.IdentifyCameraMake), so a made-up
		// vendor name round-trips lowercased. Real camera makes don't hit
		// that path — they resolve to the library's canonical, properly
		// cased name instead, which is what real-world data will look like.
		asciiEntry(0x010f, "Canon"),              // Make
		asciiEntry(0x0110, "TestCam1"),           // Model
		asciiEntry(0x0131, "TestFirmware 1.0"),   // Software
		asciiEntry(0x013b, "Jane Photographer"),  // Artist
		asciiEntry(0x8298, "(c) 2021 Jane"),      // Copyright
		asciiEntry(0x010e, "A test description"), // ImageDescription
		shortEntry(0x0112, 1),                    // Orientation
	}
	exif = []tEntry{
		asciiEntry(0x9003, "2021:03:04 05:06:07"), // DateTimeOriginal
		shortEntry(0x8827, 800),                   // ISO
		rationalEntry(0x829d, 28, 10),             // FNumber = 2.8
		rationalEntry(0x829a, 1, 200),             // ExposureTime = 1/200s
		rationalEntry(0x920a, 50000, 1000),        // FocalLength = 50mm
		asciiEntry(0xa433, "TestLensCo"),          // LensMake
		asciiEntry(0xa434, "Test Lens 50mm"),      // LensModel
		asciiEntry(0xa431, "SN12345"),             // BodySerialNumber
		longEntry(0xa002, 4000),                   // PixelXDimension
		longEntry(0xa003, 3000),                   // PixelYDimension
	}
	gps = []tEntry{
		asciiEntry(1, "N"),
		rational3Entry(2, [3][2]uint32{{37, 1}, {46, 1}, {30, 1}}), // GPSLatitude
		asciiEntry(3, "W"),
		rational3Entry(4, [3][2]uint32{{122, 1}, {25, 1}, {10, 1}}), // GPSLongitude
		byteEntry(5, 0),          // GPSAltitudeRef: above sea level
		rationalEntry(6, 100, 1), // GPSAltitude = 100m
	}
	return ifd0, exif, gps
}

func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

func TestExifProcessDecodesFullExifAndXMP(t *testing.T) {
	ifd0, exifEntries, gpsEntries := fullExifEntries()
	tiff := buildExifTIFF(ifd0, exifEntries, gpsEntries)
	xml := minimalXMP("My Title", "Jane Doe", []string{"vacation", "beach"}, 4)
	body := jpegWithAPP1s(exifApp1(tiff), xmpApp1(xml))

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "photo.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "photo.jpg").Return(sourceObject(body, "image/jpeg"), nil)

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "photo.jpg"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got.Skipped {
		t.Fatal("expected not skipped")
	}
	if !got.HasExif || !got.HasXMP {
		t.Errorf("HasExif=%v HasXMP=%v, want both true", got.HasExif, got.HasXMP)
	}
	if got.CameraMake != "Canon" {
		t.Errorf("CameraMake = %q, want Canon", got.CameraMake)
	}
	if got.CameraModel != "TestCam1" {
		t.Errorf("CameraModel = %q, want TestCam1", got.CameraModel)
	}
	if got.LensMake != "TestLensCo" || got.LensModel != "Test Lens 50mm" {
		t.Errorf("lens = %q/%q, want TestLensCo/Test Lens 50mm", got.LensMake, got.LensModel)
	}
	if got.CameraSerial != "SN12345" {
		t.Errorf("CameraSerial = %q, want SN12345", got.CameraSerial)
	}
	wantTaken := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)
	if got.TakenAt == nil || !got.TakenAt.Equal(wantTaken) {
		t.Errorf("TakenAt = %v, want %v", got.TakenAt, wantTaken)
	}
	if got.ISO == nil || *got.ISO != 800 {
		t.Errorf("ISO = %v, want 800", got.ISO)
	}
	if got.FNumber == nil || !almostEqual(float64(*got.FNumber), 2.8, 0.01) {
		t.Errorf("FNumber = %v, want ~2.8", got.FNumber)
	}
	if got.ExposureTime == nil || !almostEqual(float64(*got.ExposureTime), 1.0/200, 0.0001) {
		t.Errorf("ExposureTime = %v, want ~1/200", got.ExposureTime)
	}
	if got.FocalLength == nil || !almostEqual(float64(*got.FocalLength), 50, 0.01) {
		t.Errorf("FocalLength = %v, want ~50", got.FocalLength)
	}
	if got.ImageWidth == nil || *got.ImageWidth != 4000 || got.ImageHeight == nil || *got.ImageHeight != 3000 {
		t.Errorf("dims = %v x %v, want 4000x3000", got.ImageWidth, got.ImageHeight)
	}
	if got.Software != "TestFirmware 1.0" || got.Artist != "Jane Photographer" ||
		got.Copyright != "(c) 2021 Jane" || got.ImageDescription != "A test description" {
		t.Errorf("attribution fields wrong: %+v", got)
	}
	wantLat, wantLon := 37.775, -122.41944444444445
	if got.GPSLatitude == nil || !almostEqual(*got.GPSLatitude, wantLat, 0.0001) {
		t.Errorf("GPSLatitude = %v, want ~%v", got.GPSLatitude, wantLat)
	}
	if got.GPSLongitude == nil || !almostEqual(*got.GPSLongitude, wantLon, 0.0001) {
		t.Errorf("GPSLongitude = %v, want ~%v", got.GPSLongitude, wantLon)
	}
	if got.GPSAltitude == nil || !almostEqual(float64(*got.GPSAltitude), 100, 0.1) {
		t.Errorf("GPSAltitude = %v, want ~100", got.GPSAltitude)
	}
	if got.XMPTitle != "My Title" {
		t.Errorf("XMPTitle = %q, want My Title", got.XMPTitle)
	}
	if got.XMPCreator != "Jane Doe" {
		t.Errorf("XMPCreator = %q, want Jane Doe", got.XMPCreator)
	}
	if len(got.XMPKeywords) != 2 || got.XMPKeywords[0] != "vacation" || got.XMPKeywords[1] != "beach" {
		t.Errorf("XMPKeywords = %v, want [vacation beach]", got.XMPKeywords)
	}
	if got.XMPRating == nil || *got.XMPRating != 4 {
		t.Errorf("XMPRating = %v, want 4", got.XMPRating)
	}
	if got.ImageType != "image/jpeg" {
		t.Errorf("ImageType = %q, want image/jpeg", got.ImageType)
	}
}

func TestExifProcessXMPOnlyNoExif(t *testing.T) {
	xml := minimalXMP("Only XMP", "Someone", nil, 0)
	body := jpegWithAPP1s(xmpApp1(xml))

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "edited.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "edited.jpg").Return(sourceObject(body, "image/jpeg"), nil)

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "edited.jpg"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got.Skipped {
		t.Fatal("expected not skipped: XMP-only files still get a row")
	}
	if got.HasExif {
		t.Error("HasExif should be false: no APP1 Exif segment present")
	}
	if !got.HasXMP {
		t.Error("HasXMP should be true")
	}
	if got.XMPTitle != "Only XMP" {
		t.Errorf("XMPTitle = %q, want Only XMP", got.XMPTitle)
	}
}

func TestExifProcessSkipsFileWithNoMetadata(t *testing.T) {
	body := blankJPEGBytes()

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "plain.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "plain.jpg").Return(sourceObject(body, "image/jpeg"), nil)

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "plain.jpg"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true")
	}
}

func TestExifProcessSkipsUnsupportedType(t *testing.T) {
	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "doc.pdf").
		Return(storage.ObjectInfo{ContentType: "application/pdf", Size: 10}, nil)

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "doc.pdf"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true")
	}
	store.AssertNotCalled(t, "Get", mock.Anything, mock.Anything)
}

// TestExifProcessRawExtensionBypassesOctetStreamGate proves the
// extension-based candidate gate: object storage commonly reports camera
// RAW objects as application/octet-stream (no registered MIME type), which
// a content-type-only gate (as PreviewIndexer uses) would skip entirely.
func TestExifProcessRawExtensionBypassesOctetStreamGate(t *testing.T) {
	ifd0, exifEntries, _ := fullExifEntries()
	// CR2/NEF/ARW etc. are themselves TIFF-structured; imagemeta sniffs the
	// actual type from content, not the key, so a bare TIFF stream exercises
	// the same decode path a real RAW file would.
	body := buildExifTIFF(ifd0, exifEntries, nil)

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "photo.cr2").
		Return(storage.ObjectInfo{ContentType: "application/octet-stream", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "photo.cr2").Return(sourceObject(body, "application/octet-stream"), nil)

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "photo.cr2"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got.Skipped {
		t.Fatal("expected not skipped: .cr2 extension should pass the candidate gate")
	}
	if got.CameraMake != "Canon" {
		t.Errorf("CameraMake = %q, want Canon", got.CameraMake)
	}
}

func TestExifProcessCorruptBytesSkipsWithoutPanic(t *testing.T) {
	body := []byte("this is not an image or any known container format")

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "fake.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "fake.jpg").Return(sourceObject(body, "image/jpeg"), nil)

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "fake.jpg"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true")
	}
}

// TestExifProcessTruncatedHeaderEscalatesToFullRead exercises the tier-2
// fallback: a MaxHeaderBytes smaller than the TIFF directory itself forces
// the first read to be truncated and unparseable, so Process must re-fetch
// with the larger MaxSourceBytes bound to find the metadata at all.
func TestExifProcessTruncatedHeaderEscalatesToFullRead(t *testing.T) {
	ifd0, exifEntries, _ := fullExifEntries()
	tiff := buildExifTIFF(ifd0, exifEntries, nil)
	body := jpegWithAPP1s(exifApp1(tiff))

	cfg := defaultExifConfig()
	cfg.MaxHeaderBytes = 40 // smaller than the JPEG's own SOI+APP1 header

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "photo.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "photo.jpg").Return(sourceObject(body, "image/jpeg"), nil).Once()
	store.EXPECT().Get(mock.Anything, "photo.jpg").Return(sourceObject(body, "image/jpeg"), nil).Once()

	x := NewExifIndexer(store, ".index/", cfg)
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "photo.jpg"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got.Skipped {
		t.Fatal("expected not skipped after escalation")
	}
	if got.CameraMake != "Canon" {
		t.Errorf("CameraMake = %q, want Canon", got.CameraMake)
	}
}

// TestExifProcessDoesNotEscalateForConclusiveEmptyHeader proves the
// escalation is specifically about incomplete reads, not just "truncated and
// found nothing": a large plain JPEG with no EXIF/XMP at all decodes
// conclusively (err == nil) well within a small header limit, so Process
// must not pay for a second, larger download it already knows is pointless.
func TestExifProcessDoesNotEscalateForConclusiveEmptyHeader(t *testing.T) {
	body := blankJPEGBytes() // 621 bytes as of writing
	// Pad well past the real image data with trailing zero bytes so the
	// object is larger than MaxHeaderBytes (forcing truncated=true) while
	// the actual JPEG structure — and the conclusive "no EXIF" answer —
	// resolves entirely within the header.
	body = append(body, make([]byte, 2000)...)

	cfg := defaultExifConfig()
	cfg.MaxHeaderBytes = 700 // > len(blankJPEGBytes()), so the real image data is never cut mid-structure

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "big-plain.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "big-plain.jpg").Return(sourceObject(body, "image/jpeg"), nil).Once()

	x := NewExifIndexer(store, ".index/", cfg)
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "big-plain.jpg"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true")
	}
	// The mock's .Once() expectation on Get already fails the test if
	// Process calls Get a second time; this is an explicit belt-and-braces
	// check of the same thing.
	store.AssertNumberOfCalls(t, "Get", 1)
}

func TestExifProcessSkipsKeyUnderIndexPrefix(t *testing.T) {
	store := NewMockExifObjectStore(t)

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: ".index/previews/7"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true")
	}
	store.AssertNotCalled(t, "Stat", mock.Anything, mock.Anything)
}

// TestMergeOutcomes exercises mergeOutcomes directly rather than via
// byte-level truncation tricks (fragile: exact escalation trigger points
// depend on parser internals). This is the safety net for Process's
// escalation path: an escalated (tier-2) parse must only ever add
// information relative to the header (tier-1) parse, never erase it — the
// scenario this guards is a panic on the larger buffer specifically,
// recovered in parseExifXMP as an all-zero result.
func TestMergeOutcomes(t *testing.T) {
	base := ExifResult{HasXMP: true, XMPTitle: "From Header"}

	t.Run("escalated finding nothing preserves base", func(t *testing.T) {
		got := mergeOutcomes(base, ExifResult{})
		if !got.HasXMP || got.XMPTitle != "From Header" {
			t.Errorf("got %+v, want base's XMP preserved", got)
		}
	})

	t.Run("escalated adds EXIF without touching base's XMP", func(t *testing.T) {
		escalated := ExifResult{HasExif: true, CameraMake: "Canon"}
		got := mergeOutcomes(base, escalated)
		if !got.HasExif || got.CameraMake != "Canon" {
			t.Errorf("EXIF not merged in: %+v", got)
		}
		if !got.HasXMP || got.XMPTitle != "From Header" {
			t.Errorf("base's XMP lost: %+v", got)
		}
	})

	t.Run("escalated XMP replaces base's XMP when escalated also found XMP", func(t *testing.T) {
		escalated := ExifResult{HasXMP: true, XMPTitle: "From Escalation"}
		got := mergeOutcomes(base, escalated)
		if got.XMPTitle != "From Escalation" {
			t.Errorf("XMPTitle = %q, want escalated pass's superset result to win", got.XMPTitle)
		}
	})
}

func TestExifProcessStatError(t *testing.T) {
	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "missing").Return(storage.ObjectInfo{}, errors.New("not found"))

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got.Skipped {
		t.Error("Skipped should be false on I/O error")
	}
}

func TestExifProcessGetError(t *testing.T) {
	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "photo.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: 10}, nil)
	store.EXPECT().Get(mock.Anything, "photo.jpg").Return(nil, errors.New("get failed"))

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	if _, err := x.Process(context.Background(), Job{FileID: 1, Key: "photo.jpg"}); err == nil {
		t.Fatal("expected error")
	}
}

// TestExifProcessGPSRequiresBothLatAndLon guards against fabricating a
// coordinate on the equator/prime-meridian when only one half of a
// lat/lon pair is actually present in the EXIF GPS IFD.
func TestExifProcessGPSRequiresBothLatAndLon(t *testing.T) {
	ifd0, exifEntries, _ := fullExifEntries() // has definitive fields so HasExif is true regardless of GPS
	gps := []tEntry{
		asciiEntry(1, "N"),
		rational3Entry(2, [3][2]uint32{{37, 1}, {46, 1}, {30, 1}}), // GPSLatitude only; no GPSLongitude tag at all
	}
	tiff := buildExifTIFF(ifd0, exifEntries, gps)
	body := jpegWithAPP1s(exifApp1(tiff))

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "photo.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "photo.jpg").Return(sourceObject(body, "image/jpeg"), nil)

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "photo.jpg"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got.GPSLatitude != nil || got.GPSLongitude != nil {
		t.Errorf("GPSLatitude=%v GPSLongitude=%v, want both nil when only one half of the pair is present",
			got.GPSLatitude, got.GPSLongitude)
	}
}

// TestExifProcessEnumZeroTrustedOnlyWithOtherExif covers the
// ExposureProgram/MeteringMode/Flash zero-value ambiguity: 0 is both a
// legitimate real EXIF value ("did not fire", "Unknown", "Not Defined") and
// the Go zero value for "tag not decoded", so it must only be trusted once
// some other definitive field has confirmed a real EXIF block is present.
func TestExifProcessEnumZeroTrustedOnlyWithOtherExif(t *testing.T) {
	ifd0, exifEntries, _ := fullExifEntries() // Make/Model/etc already make HasExif true
	exifEntries = append(exifEntries,
		shortEntry(0x9209, 0), // Flash: did not fire
	)
	tiff := buildExifTIFF(ifd0, exifEntries, nil)
	body := jpegWithAPP1s(exifApp1(tiff))

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "photo.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "photo.jpg").Return(sourceObject(body, "image/jpeg"), nil)

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "photo.jpg"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got.Flash == nil || *got.Flash != 0 {
		t.Errorf("Flash = %v, want pointer to 0 (a real EXIF block confirms the zero is meaningful)", got.Flash)
	}
}

// TestExifProcessCapsKeywordCount guards against an unbounded number of
// dc:subject entries landing in the xmp_keywords column.
func TestExifProcessCapsKeywordCount(t *testing.T) {
	subjects := make([]string, maxKeywordCount+50)
	for i := range subjects {
		subjects[i] = "kw"
	}
	xml := minimalXMP("Title", "Creator", subjects, 0)
	body := jpegWithAPP1s(xmpApp1(xml))

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "photo.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "photo.jpg").Return(sourceObject(body, "image/jpeg"), nil)

	x := NewExifIndexer(store, ".index/", defaultExifConfig())
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "photo.jpg"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(got.XMPKeywords) != maxKeywordCount {
		t.Errorf("len(XMPKeywords) = %d, want %d", len(got.XMPKeywords), maxKeywordCount)
	}
}

// TestNewExifIndexerClampsNonPositiveConfig guards against a misconfigured
// (e.g. unset-and-zero, or negative) ExifConfig silently making every file
// read zero bytes and register as having no metadata.
func TestNewExifIndexerClampsNonPositiveConfig(t *testing.T) {
	body := blankJPEGBytes()

	store := NewMockExifObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "photo.jpg").
		Return(storage.ObjectInfo{ContentType: "image/jpeg", Size: int64(len(body))}, nil)
	store.EXPECT().Get(mock.Anything, "photo.jpg").Return(sourceObject(body, "image/jpeg"), nil)

	x := NewExifIndexer(store, ".index/", ExifConfig{}) // zero value: both limits non-positive
	if x.cfg.MaxHeaderBytes <= 0 || x.cfg.MaxSourceBytes <= 0 {
		t.Fatalf("cfg not clamped: %+v", x.cfg)
	}

	// Confirm the clamp is actually load-bearing: Process must still be
	// able to read the fixture, not silently see zero bytes.
	got, err := x.Process(context.Background(), Job{FileID: 1, Key: "photo.jpg"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !got.Skipped {
		t.Error("expected Skipped = true (blank fixture has no metadata)")
	}
}

func TestIsExifCandidate(t *testing.T) {
	cases := []struct {
		key, contentType string
		want             bool
	}{
		{"a.jpg", "image/jpeg", true},
		{"a.png", "image/png", true},
		{"a.cr2", "application/octet-stream", true},
		{"a.CR2", "application/octet-stream", true}, // extension match is case-insensitive
		{"a.nef", "application/octet-stream", true},
		{"a.heic", "application/octet-stream", true},
		{"a.pdf", "application/pdf", false},
		{"noext", "application/octet-stream", false},
		{"a.txt", "text/plain", false},
	}
	for _, c := range cases {
		if got := isExifCandidate(c.key, c.contentType); got != c.want {
			t.Errorf("isExifCandidate(%q, %q) = %v, want %v", c.key, c.contentType, got, c.want)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"trims NUL padding", "Canon\x00\x00\x00", "Canon"},
		{"trims whitespace", "  Canon  ", "Canon"},
		{"drops control chars", "Ca\x01n\x02on", "Canon"},
		{"keeps internal spaces", "Canon EOS 6D", "Canon EOS 6D"},
		{"collapses newlines to a single space", "line one\nline two", "line one line two"},
		{"collapses tabs and repeated whitespace", "a\t\t  b", "a b"},
		{"empty stays empty", "", ""},
		{"only NUL and control stays empty", "\x00\x01\x02", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sanitize(c.in); got != c.want {
				t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}

	t.Run("caps length in runes, not bytes", func(t *testing.T) {
		long := make([]byte, maxSanitizedFieldLen*2)
		for i := range long {
			long[i] = 'a'
		}
		got := sanitize(string(long))
		if n := utf8.RuneCountInString(got); n != maxSanitizedFieldLen {
			t.Errorf("RuneCountInString(sanitize(long)) = %d, want %d", n, maxSanitizedFieldLen)
		}
	})

	t.Run("caps length in runes on multibyte input", func(t *testing.T) {
		// "日" is 3 bytes in UTF-8; a byte-based cap would truncate mid-rune
		// or count far fewer characters than a rune-based cap.
		long := strings.Repeat("日", maxSanitizedFieldLen*2)
		got := sanitize(long)
		if n := utf8.RuneCountInString(got); n != maxSanitizedFieldLen {
			t.Errorf("RuneCountInString(sanitize(multibyte)) = %d, want %d", n, maxSanitizedFieldLen)
		}
		if !utf8.ValidString(got) {
			t.Error("sanitize produced invalid UTF-8")
		}
	})
}

func TestStoreExifResultSkipped(t *testing.T) {
	// A nil *db.Queries would panic if StoreExifResult tried to use it;
	// passing one proves the skip path returns before that ever happens.
	if err := StoreExifResult(context.Background(), nil, 1, ExifResult{Skipped: true}); err != nil {
		t.Errorf("StoreExifResult(skipped) = %v, want nil", err)
	}
}
