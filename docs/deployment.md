# Deploying file-indexer

> **WIP:** this guide reflects the committed manifests
> (`deploy/base`, `deploy/overlays/production`) but has **not** been
> validated against a clean external cluster. Unvalidated areas are marked
> TODO and tracked in [TODO.md](TODO.md#production-deployment-noted-92326).

file-indexer is a set of Kubernetes workloads that index an S3-compatible
bucket into a Postgres-backed search index, served through a web UI. You
bring your own Postgres and bucket; the rest deploys from `deploy/`.

## Prerequisites

- A Kubernetes cluster and `kubectl` (with kustomize support).
- A Postgres database reachable from the cluster.
- An S3-compatible bucket and credentials (AWS S3, DigitalOcean Spaces,
  MinIO, RustFS, ...).
- Recommended: ArgoCD — the overlay's migration ordering uses its sync
  hooks (see Deploying).

## Configuration and secrets

Workloads read two Secrets via `envFrom`, deliberately not defined in the
manifests — create them per environment:

- `db-secret`: `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_NAME`, `DB_PASSWORD`
- `s3-secret`: `S3_ENDPOINT`, `S3_ACCESS_ID`, `S3_SECRET`, `S3_BUCKET`,
  `S3_REGION` (SigV4 signing region)

```sh
kubectl -n <ns> create secret generic db-secret --from-env-file=db.env
kubectl -n <ns> create secret generic s3-secret --from-env-file=s3.env
```

(each `.env` file holds the keys listed above). Non-credential config lives
in the `app-config` ConfigMap (base `app.env` merged with the overlay's;
production sets `S3_SECURE=true`). The `index-config` ConfigMap sets
`INDEX_PREFIX` (default `.index/`): derived objects (previews) live under
that prefix and the crawler ignores it.

## Deploying

The production overlay pins all images to the current release tag. Render
to inspect:

```sh
kubectl kustomize deploy/overlays/production
```

TODO (unvalidated): apply ordering without ArgoCD. The migrate Job must
complete before the workloads roll; the overlay expresses this only via
ArgoCD annotations (ServiceAccount in sync-wave `-2`, migrate as a Sync
hook in wave `-1` with `BeforeHookCreation` deletion). With ArgoCD, point
an Application at `deploy/overlays/production`; a verified plain
`kubectl apply -k` ordering is still needed.

**Migration behavior:** migrations run as a one-shot Job (`migrate`,
goose). `ttlSecondsAfterFinished: 300` garbage-collects finished Jobs, so
a later apply recreates the Job; under ArgoCD the hook-delete policy
re-runs it on every sync. (Job `spec.template` is immutable, so
re-applying while a previous Job still exists fails.)

## Exposure

Only the `web` Service (HTTP, port 3000) is meant to leave the cluster;
`files` and `search` are gRPC-only and stay cluster-internal. The overlay
ships **no** Ingress/Gateway/TLS manifest — TODO (unvalidated): example
Ingress or HTTPRoute with TLS. Point your preferred exposure at `web`.

**Presigned URLs:** browser uploads/downloads use presigned S3 URLs
minted by the `files` service, signed against `S3_ENDPOINT` by default. If
that endpoint is not browser-reachable, set `S3_PUBLIC_ENDPOINT` on the
files Deployment (pattern: `deploy/overlays/local/files-public-endpoint.yaml`).
TODO: confirm against a hosted S3 provider.

## S3 bucket CORS (required for browser uploads)

Uploads go straight from the browser to S3 via presigned PUT URLs, so the
bucket must answer the browser's cross-origin preflight. Misconfiguration
surfaces as `Cross-Origin Request Blocked: ... (Reason: CORS header
'Access-Control-Allow-Origin' missing). Status code: 403.`

| Setting | Value |
| --- | --- |
| Allowed origin | the origin the web app is served from (e.g. `https://files.example.com`) |
| Allowed methods | `PUT` (small files and multipart parts both use presigned PUTs), `GET` (downloads/previews) |
| Allowed headers | `*` is simplest; otherwise at least `Content-Type` and `host` |
| Max age | any reasonable value (e.g. 3000 seconds) |

Example AWS S3 / DigitalOcean Spaces CORS XML:

```xml
<CORSConfiguration>
  <CORSRule>
    <AllowedOrigin>https://files.example.com</AllowedOrigin>
    <AllowedMethod>PUT</AllowedMethod>
    <AllowedMethod>GET</AllowedMethod>
    <AllowedHeader>*</AllowedHeader>
    <MaxAgeSeconds>3000</MaxAgeSeconds>
  </CORSRule>
</CORSConfiguration>
```

The local-dev overlay sets `RUSTFS_CORS_ALLOWED_ORIGINS='*'` for the same
reason (`deploy/overlays/local/local-s3.yaml`).

## Verifying

- `kubectl -n <ns> get pods` — `web`, `files`, `search`, and the three
  index workers become ready; `migrate` completes
  (`kubectl -n <ns> logs job/migrate`).
- The index workers expose `/healthz` and `/metrics` on port `8081`
  (liveness probes are wired to `/healthz`).
- Open the web URL: crawled/uploaded files appear, previews populate as the
  index workers drain.

## Scheduling

Two CronJobs (both `concurrencyPolicy: Forbid`): `crawler` runs a
full-bucket reconciliation scan daily at 01:00 UTC; `preview-gc` deletes
orphaned preview objects under the index prefix weekly (Sunday 02:00 UTC).
To index immediately:
`kubectl -n <ns> create job --from=cronjob/crawler crawler-manual-1`

## Upgrades and rollback

TODO (unvalidated): upgrade, rollback, and uninstall procedures. All
images move together on a single release tag; migrations run forward via
the migrate hook. Backups are out of scope — the bucket is the source of
truth; back it (and Postgres) up with your own tooling.

## Current limitations

Single-replica deployments, no NetworkPolicies, no HPA/VPA, BYO Postgres
and S3, full-bucket-scan crawler. Full list: [TODO.md](TODO.md#production-deployment-noted-92326).
