//go:build integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestGetIndexExifResultNotFound covers the "not yet indexed, or has neither
// EXIF nor XMP" case: no row exists, and callers must see pgx.ErrNoRows (see
// internal/service/files/server.go's GetFileInfo, which treats this as "no
// exif metadata").
func TestGetIndexExifResultNotFound(t *testing.T) {
	conn := testConn(t)
	q := New(conn)

	fileID := insertTestFile(t, conn, uniqueKey(t))

	_, err := q.GetIndexExifResult(context.Background(), fileID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetIndexExifResult error = %v, want pgx.ErrNoRows", err)
	}
}

// TestUpsertAndGetIndexExifResult round-trips a fully-populated row (every
// nullable column set, not just the couple of fields the indexer package's
// own integration tests happen to exercise) to cover GetIndexExifResult's
// full Scan.
func TestUpsertAndGetIndexExifResult(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()

	fileID := insertTestFile(t, conn, uniqueKey(t))

	takenAt := time.Date(2022, 6, 1, 12, 0, 0, 0, time.UTC)
	gpsAt := time.Date(2022, 6, 1, 12, 0, 5, 0, time.UTC)
	xmpCreateDate := time.Date(2022, 6, 2, 9, 30, 0, 0, time.UTC)

	params := UpsertIndexExifResultParams{
		FileID:           fileID,
		ImageType:        pgtype.Text{String: "image/jpeg", Valid: true},
		CameraMake:       pgtype.Text{String: "Canon", Valid: true},
		CameraModel:      pgtype.Text{String: "Canon EOS R5", Valid: true},
		CameraSerial:     pgtype.Text{String: "SN123", Valid: true},
		LensMake:         pgtype.Text{String: "Canon", Valid: true},
		LensModel:        pgtype.Text{String: "RF 24-70mm", Valid: true},
		TakenAt:          pgtype.Timestamp{Time: takenAt, Valid: true},
		Iso:              pgtype.Int4{Int32: 400, Valid: true},
		FNumber:          pgtype.Float4{Float32: 2.8, Valid: true},
		ExposureTime:     pgtype.Float4{Float32: 0.01, Valid: true},
		FocalLength:      pgtype.Float4{Float32: 50, Valid: true},
		FocalLength35mm:  pgtype.Float4{Float32: 50, Valid: true},
		ExposureProgram:  pgtype.Int2{Int16: 2, Valid: true},
		MeteringMode:     pgtype.Int2{Int16: 5, Valid: true},
		Flash:            pgtype.Int2{Int16: 0, Valid: true},
		Orientation:      pgtype.Int2{Int16: 1, Valid: true},
		ImageWidth:       pgtype.Int4{Int32: 4000, Valid: true},
		ImageHeight:      pgtype.Int4{Int32: 3000, Valid: true},
		GpsLatitude:      pgtype.Float8{Float64: 43.4674, Valid: true},
		GpsLongitude:     pgtype.Float8{Float64: 11.885, Valid: true},
		GpsAltitude:      pgtype.Float4{Float32: 12.5, Valid: true},
		GpsAt:            pgtype.Timestamptz{Time: gpsAt, Valid: true},
		Software:         pgtype.Text{String: "Adobe Lightroom", Valid: true},
		Artist:           pgtype.Text{String: "Jane Doe", Valid: true},
		Copyright:        pgtype.Text{String: "(c) Jane Doe", Valid: true},
		ImageDescription: pgtype.Text{String: "A photo", Valid: true},
		XmpTitle:         pgtype.Text{String: "Sunset", Valid: true},
		XmpDescription:   pgtype.Text{String: "Sunset over the bay", Valid: true},
		XmpCreator:       pgtype.Text{String: "Jane Doe", Valid: true},
		XmpLabel:         pgtype.Text{String: "Red", Valid: true},
		XmpRating:        pgtype.Int2{Int16: 5, Valid: true},
		XmpKeywords:      []string{"sunset", "bay"},
		XmpCreateDate:    pgtype.Timestamptz{Time: xmpCreateDate, Valid: true},
		HasExif:          true,
		HasXmp:           true,
	}
	if err := q.UpsertIndexExifResult(ctx, params); err != nil {
		t.Fatalf("UpsertIndexExifResult: %v", err)
	}

	got, err := q.GetIndexExifResult(ctx, fileID)
	if err != nil {
		t.Fatalf("GetIndexExifResult: %v", err)
	}
	if got.CameraMake != params.CameraMake || got.CameraModel != params.CameraModel {
		t.Errorf("camera = %+v/%+v, want %+v/%+v", got.CameraMake, got.CameraModel, params.CameraMake, params.CameraModel)
	}
	if got.Iso != params.Iso || got.FNumber != params.FNumber {
		t.Errorf("iso/f_number = %+v/%+v, want %+v/%+v", got.Iso, got.FNumber, params.Iso, params.FNumber)
	}
	if !got.TakenAt.Time.Equal(params.TakenAt.Time) {
		t.Errorf("TakenAt = %v, want %v", got.TakenAt.Time, params.TakenAt.Time)
	}
	if got.GpsLatitude != params.GpsLatitude || got.GpsLongitude != params.GpsLongitude {
		t.Errorf("gps = %+v/%+v, want %+v/%+v", got.GpsLatitude, got.GpsLongitude, params.GpsLatitude, params.GpsLongitude)
	}
	if !got.GpsAt.Time.Equal(params.GpsAt.Time) {
		t.Errorf("GpsAt = %v, want %v", got.GpsAt.Time, params.GpsAt.Time)
	}
	if len(got.XmpKeywords) != 2 || got.XmpKeywords[0] != "sunset" || got.XmpKeywords[1] != "bay" {
		t.Errorf("XmpKeywords = %v, want [sunset bay]", got.XmpKeywords)
	}
	if !got.XmpCreateDate.Time.Equal(params.XmpCreateDate.Time) {
		t.Errorf("XmpCreateDate = %v, want %v", got.XmpCreateDate.Time, params.XmpCreateDate.Time)
	}
	if !got.HasExif || !got.HasXmp {
		t.Errorf("HasExif=%v HasXmp=%v, want both true", got.HasExif, got.HasXmp)
	}

	// Re-upserting must update in place (ON CONFLICT), not error.
	params.CameraModel = pgtype.Text{String: "Canon EOS R5 Mark II", Valid: true}
	if err := q.UpsertIndexExifResult(ctx, params); err != nil {
		t.Fatalf("UpsertIndexExifResult (second run): %v", err)
	}
	updated, err := q.GetIndexExifResult(ctx, fileID)
	if err != nil {
		t.Fatalf("GetIndexExifResult after re-upsert: %v", err)
	}
	if updated.CameraModel != params.CameraModel {
		t.Errorf("CameraModel after re-upsert = %+v, want %+v", updated.CameraModel, params.CameraModel)
	}
}
