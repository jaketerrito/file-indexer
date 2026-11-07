# Seed data

Local-dev-only sample objects, uploaded into the local RustFS bucket by the
`seed` sidecar (`seed-sidecar.yaml`, patched into the `local-s3` Deployment
defined by this overlay). The `seed-data` ConfigMap is generated straight from
this directory in the Tiltfile (`kubectl create configmap --from-file`) — not
via kustomize's `configMapGenerator`, which can't take a directory or glob.
Files dropped into `seed/` automatically become bucket objects keyed by
filename, with no list to keep in sync elsewhere. The exceptions and additions
are documented below. Keep the directory's total size comfortably under 1MiB
(the ConfigMap size limit); it's currently ~316KiB.

Chosen to exercise the exif index type and the frontend metadata modal
across a few different cases:

| File               | Purpose                                              |
|--------------------|-------------------------------------------------------|
| `photo-gps-1.jpg`  | EXIF + real GPS coordinates                            |
| `photo-gps-2.jpg`  | EXIF + real GPS coordinates (different camera reading) |
| `photo-plain.jpg`  | Camera EXIF (make/model/settings), no GPS fix          |
| `photo-no-exif.jpg`| No EXIF at all — exercises the modal's absent-exif path|
| `photo-extra-source.jpg` | Source for 30 numbered objects used in infinite-scroll testing |
| `notes.txt`, `todo.txt`, `lorem.txt` | Plain text objects (non-image content) |

## Image provenance

`photo-gps-1.jpg`, `photo-gps-2.jpg`, and `photo-plain.jpg` are taken from
[ianare/exif-samples](https://github.com/ianare/exif-samples)
(`jpg/gps/DSCN0010.jpg`, `jpg/gps/DSCN0025.jpg`, and `jpg/Canon_40D.jpg`
respectively) — a widely-used collection of camera-generated EXIF test
fixtures.

That repository states new contributions are licensed
Attribution-ShareAlike 4.0 International (CC BY-SA 4.0); these particular
files are long-standing fixtures used across many open-source EXIF
libraries' test suites. They're used here strictly as local development
fixtures, not redistributed as a standalone asset package.

`photo-no-exif.jpg` is a synthesized solid-color JPEG (generated with
Pillow, no source image), not sourced from exif-samples: despite its name,
that repo's `jpg/xmp/no_exif.jpg` actually carries EXIF (software, artist,
description) and XMP (title, keywords) — it's a baseline for XMP tests, not
a metadata-free image — so it couldn't stand in for the "no exif" case.

`photo-extra-source.jpg` is not uploaded under its own name; the sidecar copies
it to `photo-extra-001.jpg` through `photo-extra-030.jpg`. `todo.txt` is also
copied to `docs/todo.txt` to provide a nested object.

TODO: `photo-extra-source.jpg` provenance is unverified — a 633-byte 8×8 JFIF
JPEG with no EXIF segments (per `file`). Document its source or replace it.

The text files are original, trivial placeholder content.
