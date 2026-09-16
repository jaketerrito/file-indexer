# TODO

Open work items. Consolidated 2026-09-02 from NOTES.md, DESIGN.md's Plan
section, and in-code TODO comments; parenthetical dates record when an item
was first noted. Delete entries as they land — no strike-throughs, git
history is the archive.

## Backend

- **gRPC introspection tooling** (noted 7/2/26): nothing is set up — no
  server reflection registered in any cmd/, and proto/buf.gen.yaml runs only
  protoc-gen-go + protoc-gen-go-grpc (no OpenAPI/gateway plugin). Pick one:
  enable gRPC reflection and use a port-forward + an external client
  (grpcurl/grpcui), or add a swagger-style generation path for FilesService
  and SearchService.
- **Video/audio/PDF metadata** (7/27/26): imagemeta covers images + camera
  RAW only. A future mp4/id3 extractor can reuse index_exif_result's common
  columns (make, model, taken_at, gps, dimensions) rather than inventing a
  parallel schema.
- **Tagging and tag search** (DESIGN.md): no tags table, no tagging index
  type, no tag filter in search. DESIGN.md wants AI-based tagging (heavy-duty
  indexing, separate process) and "string match tags" search.
- **Name-substring search** (DESIGN.md): prefix matching is the only name
  match today (proto/service/v1/search.proto); DESIGN.md wants string-match
  name search.
- **Resumable large uploads** (DESIGN.md): upload is a single presigned PUT
  followed by CommitUpload; an interrupted large upload restarts from zero.
  Add presigned S3 multipart upload for per-part resumability.

## Frontend

- **Styling / design system** (7/5/26): the UI is intentionally bare semantic
  HTML — no styling dependencies in web/package.json. Needs an actual design
  pass.
- **SSR first-page data** (7/5/26): the file list fetches client-side after
  hydration; nothing in web/src uses a router loader. Consider a TanStack
  Router loader + react-query SSR integration so the first page renders
  server-side.

## CI / infra

- **Tilt web dev loop** (7/5/26): no live_update in the Tiltfile — every web
  change triggers a full image rebuild. Iterate with `npm run dev` against
  port-forwards for now; add live_update or a tilt-managed dev server.

## Known limitations (not scheduled)

- **No HEIC/AVIF previews** (7/26/26): no pure-Go decoder exists and the
  binaries are CGO_ENABLED=0 on scratch.
- **Garbled Sony ARW MakerNote fields** (7/27/26): an upstream imagemeta
  reverse-offset bug garbles some Sony lens fields;
  sanitize()/maxSanitizedFieldLen in internal/service/indexer/exif.go bounds
  the damage but doesn't fix it.
- **Presigned preview URLs bypass the browser cache** (7/26/26):
  GetPreviewURL presigns per request, so the browser HTTP cache never hits
  across page loads. If thumbnail bandwidth becomes a problem, swap
  GetPreviewURL for a cacheable BFF route serving bytes with immutable cache
  headers.
