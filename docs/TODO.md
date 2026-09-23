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

## Frontend

- **Styling / design system** (7/5/26): the UI is intentionally bare semantic
  HTML — no styling dependencies in web/package.json. Needs an actual design
  pass.
- **SSR first-page data** (7/5/26): the file list fetches client-side after
  hydration; nothing in web/src uses a router loader. Consider a TanStack
  Router loader + react-query SSR integration so the first page renders
  server-side.

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

## Production deployment (noted 9/23/26)

Omissions from the initial production overlay work (deploy/overlays/production),
recorded here after the quickstart doc was trimmed from the repo:

- **No index-worker health checks**: index-stat, index-preview and index-exif
  have no probes — the images are FROM scratch (no shell, no listener) and
  restart policy only covers crashes, not a worker that is alive but stuck.
  Tracked in #117 (shared indexer.Run heartbeat + tiny /healthz per worker).
- **No NetworkPolicies**: nothing segments in-cluster traffic; every pod can
  reach every pod. Tracked in #115.
- **No committed ingress/gateway manifest**: only the web Service ships;
  exposure (Ingress, Gateway API HTTPRoute, LoadBalancer) is the user's
  choice. files/search gRPC stay cluster-internal.
- **BYO Postgres and S3 only**: no embedded database, no HA/backup story for
  Postgres, no secret-management operator integration — users supply
  db-secret/s3-secret and operate their own infrastructure.
- **Conservative single-replica defaults**: replicas stay 1, no HPA/VPA, and
  resource requests/limits are starting points users should tune in their
  own overlay.
