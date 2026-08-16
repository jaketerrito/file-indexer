package indexer

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"sort"
	"strconv"
)

// This file hand-builds minimal EXIF/XMP-carrying JPEGs for exif_test.go, so
// tests need no vendored sample photos (avoiding both licensing questions and
// binary fixture files in the repo). Every value written here is verified in
// exif_test.go by asserting it round-trips through imagemeta/xmp correctly.

// tEntry is one TIFF IFD directory entry: a tag, its TIFF type code, the
// number of values of that type, and the raw encoded value bytes. Values of
// 4 bytes or fewer are stored inline; longer values are relocated to the
// TIFF "heap" area by encodeIFD.
type tEntry struct {
	tag   uint16
	typ   uint16
	count uint32
	data  []byte
}

// TIFF type codes used below (see TIFF6 spec section 2).
const (
	tiffASCII    = 2
	tiffShort    = 3
	tiffRational = 5
	tiffByte     = 1
)

func leU16(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }
func leU32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }

// mustWrite writes v to buf. binary.Write only ever errors on an
// unsupported type or a failing io.Writer; bytes.Buffer never fails, so any
// error here is a fixture-construction bug, not a runtime condition to
// handle gracefully.
func mustWrite(buf *bytes.Buffer, v any) {
	if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
		panic(err)
	}
}

func rationalBytes(n, d uint32) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b[0:4], n)
	binary.LittleEndian.PutUint32(b[4:8], d)
	return b
}

func shortEntry(tag uint16, v uint16) tEntry {
	return tEntry{tag: tag, typ: tiffShort, count: 1, data: leU16(v)}
}

func asciiEntry(tag uint16, s string) tEntry {
	v := append([]byte(s), 0) // NUL-terminated per TIFF ASCII type
	return tEntry{tag: tag, typ: tiffASCII, count: uint32(len(v)), data: v}
}

func rationalEntry(tag uint16, n, d uint32) tEntry {
	return tEntry{tag: tag, typ: tiffRational, count: 1, data: rationalBytes(n, d)}
}

func longEntry(tag uint16, v uint32) tEntry {
	return tEntry{tag: tag, typ: 4, count: 1, data: leU32(v)}
}

// rational3Entry encodes a degrees/minutes/seconds GPS coordinate component
// (EXIF GPSLatitude/GPSLongitude are 3 rationals: deg, min, sec).
func rational3Entry(tag uint16, dms [3][2]uint32) tEntry {
	var b bytes.Buffer
	for _, v := range dms {
		b.Write(rationalBytes(v[0], v[1]))
	}
	return tEntry{tag: tag, typ: tiffRational, count: 3, data: b.Bytes()}
}

func byteEntry(tag uint16, v byte) tEntry {
	return tEntry{tag: tag, typ: tiffByte, count: 1, data: []byte{v}}
}

// encodeIFD serializes entries as one TIFF IFD: entry count, 12-byte entries
// (relocating oversized values into heap), and a next-IFD offset. Entries
// are written in ascending tag order per the TIFF6 spec (imagemeta's own
// scanner tolerates unordered tags, but a stricter reader might not, and
// there's no reason for a test fixture to encode an invalid stream).
func encodeIFD(entries []tEntry, heap *bytes.Buffer, heapBase uint32, next uint32) []byte {
	sorted := append([]tEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].tag < sorted[j].tag })

	var dir bytes.Buffer
	mustWrite(&dir, uint16(len(sorted)))
	for _, e := range sorted {
		mustWrite(&dir, e.tag)
		mustWrite(&dir, e.typ)
		mustWrite(&dir, e.count)
		if len(e.data) <= 4 {
			v := make([]byte, 4)
			copy(v, e.data)
			dir.Write(v)
		} else {
			off := heapBase + uint32(heap.Len())
			mustWrite(&dir, off)
			heap.Write(e.data)
			if heap.Len()%2 == 1 {
				heap.WriteByte(0) // TIFF values are word-aligned
			}
		}
	}
	mustWrite(&dir, next)
	return dir.Bytes()
}

// buildExifTIFF lays out a little-endian TIFF byte stream: IFD0, optionally
// followed by an ExifIFD and/or GPS IFD (linked via the standard 0x8769 /
// 0x8825 pointer tags, injected automatically), followed by a shared heap
// for every value too large to store inline.
func buildExifTIFF(ifd0Entries, exifEntries, gpsEntries []tEntry) []byte {
	const headerLen = 8
	ifd0 := append([]tEntry(nil), ifd0Entries...)
	if exifEntries != nil {
		ifd0 = append(ifd0, tEntry{tag: 0x8769, typ: 4, count: 1}) // patched below
	}
	if gpsEntries != nil {
		ifd0 = append(ifd0, tEntry{tag: 0x8825, typ: 4, count: 1}) // patched below
	}

	ifd0Off := uint32(headerLen)
	ifd0Size := uint32(2 + 12*len(ifd0) + 4)
	exifOff := ifd0Off + ifd0Size
	var exifSize uint32
	if exifEntries != nil {
		exifSize = uint32(2 + 12*len(exifEntries) + 4)
	}
	gpsOff := exifOff + exifSize
	var gpsSize uint32
	if gpsEntries != nil {
		gpsSize = uint32(2 + 12*len(gpsEntries) + 4)
	}
	heapBase := gpsOff + gpsSize

	for i, e := range ifd0 {
		switch e.tag {
		case 0x8769:
			ifd0[i].data = leU32(exifOff)
		case 0x8825:
			ifd0[i].data = leU32(gpsOff)
		}
	}

	var heap bytes.Buffer
	dir0 := encodeIFD(ifd0, &heap, heapBase, 0)
	var dirExif, dirGPS []byte
	if exifEntries != nil {
		dirExif = encodeIFD(exifEntries, &heap, heapBase, 0)
	}
	if gpsEntries != nil {
		dirGPS = encodeIFD(gpsEntries, &heap, heapBase, 0)
	}

	var out bytes.Buffer
	out.WriteString("II")
	mustWrite(&out, uint16(42))
	mustWrite(&out, ifd0Off)
	out.Write(dir0)
	out.Write(dirExif)
	out.Write(dirGPS)
	out.Write(heap.Bytes())
	return out.Bytes()
}

// blankJPEGBytes is a tiny (2x2) baseline JPEG with no APP1 segment at all,
// used as the carrier that APP1 payloads get spliced into.
func blankJPEGBytes() []byte {
	var buf bytes.Buffer
	m := image.NewRGBA(image.Rect(0, 0, 2, 2))
	m.Set(0, 0, color.White)
	if err := jpeg.Encode(&buf, m, nil); err != nil {
		panic(err) // fixture helper; a stdlib encode failure is a test bug
	}
	return buf.Bytes()
}

// jpegWithAPP1s splices one or more raw APP1 payloads (each already
// including its identifier string, e.g. "Exif\x00\x00..." or
// "http://ns.adobe.com/xap/1.0/\x00...") right after a JPEG's SOI marker.
// Multiple APP1 segments — one Exif, one XMP — is valid per the JPEG spec
// and is exactly how real cameras/editors carry both at once.
func jpegWithAPP1s(payloads ...[]byte) []byte {
	j := blankJPEGBytes()

	var out bytes.Buffer
	out.Write(j[:2]) // SOI
	for _, p := range payloads {
		out.WriteByte(0xFF)
		out.WriteByte(0xE1)
		if err := binary.Write(&out, binary.BigEndian, uint16(len(p)+2)); err != nil {
			panic(err)
		}
		out.Write(p)
	}
	out.Write(j[2:])
	return out.Bytes()
}

// exifApp1 wraps a TIFF stream with the Exif APP1 identifier.
func exifApp1(tiff []byte) []byte {
	return append([]byte("Exif\x00\x00"), tiff...)
}

// xmpApp1 wraps an XMP XML packet with the standard XMP APP1 identifier.
func xmpApp1(xml string) []byte {
	return append([]byte("http://ns.adobe.com/xap/1.0/\x00"), []byte(xml)...)
}

// minimalXMP builds a small but well-formed XMP packet carrying dc:title,
// dc:creator, dc:subject and xmp:Rating, the fields exif.go extracts.
func minimalXMP(title, creator string, subject []string, rating int) string {
	var subj bytes.Buffer
	for _, s := range subject {
		subj.WriteString("<rdf:li>" + s + "</rdf:li>")
	}
	return `<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		`<rdf:Description rdf:about=""` +
		` xmlns:dc="http://purl.org/dc/elements/1.1/"` +
		` xmlns:xmp="http://ns.adobe.com/xap/1.0/">` +
		`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">` + title + `</rdf:li></rdf:Alt></dc:title>` +
		`<dc:creator><rdf:Seq><rdf:li>` + creator + `</rdf:li></rdf:Seq></dc:creator>` +
		`<dc:subject><rdf:Bag>` + subj.String() + `</rdf:Bag></dc:subject>` +
		`<xmp:Rating>` + strconv.Itoa(rating) + `</xmp:Rating>` +
		`</rdf:Description></rdf:RDF></x:xmpmeta>`
}
