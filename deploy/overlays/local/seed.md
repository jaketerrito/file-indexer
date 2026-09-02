# Seed data

Local-dev-only sample objects, uploaded into the MinIO bucket by the `seed`
sidecar (`seed-sidecar.yaml`, patched into the base MinIO Deployment by this
overlay — seeding is dev-only, so base MinIO stays seed-free). The `seed-data`
ConfigMap is generated straight from this directory in the Tiltfile
(`kubectl create configmap --from-file`) —
not via kustomize's `configMapGenerator`, which can't take a directory or
glob. **Every file dropped into `seed/` automatically becomes a bucket
object**, keyed by filename, uploaded at the bucket root — there's no list
to keep in sync elsewhere. Keep the directory's total size comfortably under
1MiB (the ConfigMap size limit); it's currently ~500KB.

Chosen to exercise the exif index type and the frontend metadata modal
across a few different cases:

| File               | Purpose                                              |
|--------------------|-------------------------------------------------------|
| `photo-gps-1.jpg`  | EXIF + real GPS coordinates                            |
| `photo-gps-2.jpg`  | EXIF + real GPS coordinates (different camera reading) |
| `photo-plain.jpg`  | Camera EXIF (make/model/settings), no GPS fix          |
| `photo-no-exif.jpg`| No EXIF at all — exercises the modal's absent-exif path|
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

The text files are original, trivial placeholder content.
