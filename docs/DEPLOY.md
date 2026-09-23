# Production deployment guide (agent-focused)

How to deploy file-indexer to a real environment. Written for agents
executing the deployment: every step is explicit, every contract is stated,
and every gotcha we hit is recorded. The reference environment is a
DigitalOcean Kubernetes (DOKS) cluster managed GitOps-style by a separate
infra repo (`jaketerrito/cloud-configs`), with DigitalOcean Spaces as the S3
backend — generalize from there.

## Architecture contract (what the environment must supply)

`deploy/overlays/production` is a complete kustomization **except** for
environment-specifics. It deliberately ships no `db-secret` / `s3-secret`
and no HTTP route — the environment provides them. Every workload consumes
secrets via `envFrom`, so the secret **keys** are the contract:

| Secret | Keys | Source |
| --- | --- | --- |
| `db-secret` | `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_NAME`, `DB_PASSWORD` | Environment's Postgres |
| `s3-secret` | `S3_ENDPOINT`, `S3_ACCESS_ID`, `S3_SECRET`, `S3_BUCKET`, `S3_REGION`, `S3_SECURE` | Environment's bucket credentials |

Hard contracts an agent must not violate:

1. **`S3_ENDPOINT` is `host:port`, no scheme.** `validateEndpoint` in
   `internal/storage/s3.go` rejects any value containing `://`. Hosted
   providers document endpoints with a scheme (e.g.
   `https://sfo3.digitaloceanspaces.com`) — strip it and append the port
   (`sfo3.digitaloceanspaces.com:443`).
2. **TLS requires `S3_SECURE=true`** (added in v0.1.1). Every binary
   passes it to the minio client's `secure` flag. Any hosted S3 provider
   (Spaces, AWS, B2) requires `true`; only local MinIO uses the `false`
   default. Deploying an image `< v0.1.1` against a hosted provider fails:
   the env var doesn't exist and the client speaks plaintext HTTP.
3. **Postgres is reached with `sslmode=disable`** (hardcoded in
   `internal/config/config.go`'s `DatabaseConfig.URL`). Use an in-cluster
   Postgres or a trusted-network database; a managed database that
   *requires* TLS will not work without a code change.
4. **`S3_REGION` is the SigV4 signing region.** For Spaces, use the
   bucket's region slug (e.g. `sfo3`), derivable from the endpoint host.
5. **The migrate Job must run before the workloads.** The production
   overlay sets `argocd.argoproj.io/sync-wave: "-1"` on it; if your
   deployer doesn't honor sync waves, order it yourself.
6. **All images share one tag** — the release workflow's sed depends on
   it. Bump tags only via release (see below), never by hand-editing one
   image.

## Step-by-step: the reference (cloud-configs / DOKS) deployment

Everything below already exists in `jaketerrito/cloud-configs` (PR #52,
`kubernetes-resources/file-indexer/`) — read it as the worked example.

1. **Create the DB password secret.** A random 32-byte password in the
   cluster's secret backend (GCP Secret Manager for the reference cluster):
   `gcloud secrets create file-indexer-postgres-password --data-file=-`.
2. **Deploy in-cluster Postgres.** `postgres.yaml`: Deployment
   (`postgres:18-alpine`, `Recreate` strategy, `pg_isready` TCP probe,
   `PGDATA` on a PVC subPath) + Service + 5Gi PVC. It reads
   `POSTGRES_USER`/`POSTGRES_DB`/`POSTGRES_PASSWORD` from the same
   `db-secret` keys the app consumes, so one ExternalSecret serves both.
3. **Create the two ExternalSecrets** (`secrets.yaml`) against the
   `gcp-secret-store` ClusterSecretStore. The `s3-secret` template
   normalizes the stored credential at sync time:
   `S3_ENDPOINT: '{{ .endpoint | trimPrefix "https://" }}:443'` and
   `S3_REGION: '{{ .endpoint | trimPrefix "https://" | trimSuffix ".digitaloceanspaces.com" }}'`
   (sprig functions, external-secrets `engineVersion: v2`). This reuses
   the terraform-managed `personal-storage-do-s3-key` secret as-is — no
   terraform change needed.
4. **Expose the web UI.** `routes.yaml`: one HTTPRoute
   (`files.territo.dev` → Service `web` port 3000) on the shared Gateway.
   The reference cluster has a **gateway-level** authelia OIDC
   SecurityPolicy, so the route gets auth by default — do not add a
   route-level policy. Register the OAuth callback
   (`https://files.territo.dev/oauth2/callback`) in the authelia client's
   `redirect_uris` (`infra/authelia/authelia-chart.yaml` in cloud-configs).
   DNS/cert are automatic (external-dns + wildcard TLS on the Gateway).
5. **Register two ArgoCD Applications** (`appsets/file-indexer.yaml`):
   - `file-indexer-env` → the infra repo path holding steps 2–4
     (destination namespace `file-indexer`, `CreateNamespace=true`).
   - `file-indexer` → **this repo's** `deploy/overlays/production`, same
     namespace. ArgoCD's eventual consistency handles the startup race
     (workloads may crash until the secrets exist; they retry).
6. **Cut the release that deployable images come from** (GitHub UI or
   `gh release create vX.Y.Z`). The release workflow builds and pushes all
   9 images to `ghcr.io/jaketerrito/file-indexer/*` and opens a mechanical
   PR (`release/vX.Y.Z` branch) bumping the production overlay tag.
   **Merge that PR** — until then ArgoCD still deploys the old tag.

## Verification checklist

Run after the deployer syncs (agent-executable, in order):

1. `kubectl -n file-indexer get externalsecrets` — both `Ready`; a bad
   template or missing GCP key shows up here first, not in pod logs.
2. `kubectl -n file-indexer get pods` — `postgres` Running before
   anything else can be; `migrate-*` Completed (check
   `kubectl -n file-indexer logs job/migrate` on failure — usually the DB
   secret).
3. Workers Running: `files`, `search`, `web`, `crawler`,
   `index-stat`, `index-preview`, `index-exif`. CrashLoopBackOff on all
   workers at once almost always means `s3-secret` (endpoint scheme,
   missing `:443`, or `S3_SECURE` unset on a hosted provider).
4. Trigger a crawl and confirm rows: port-forward `files`/`search` gRPC,
   or check the DB directly via the postgres pod
   (`psql -U postgres -c 'select count(*) from files;'`).
5. `curl -sI https://files.territo.dev` — expect a redirect to the auth
   provider (302), not a 502/503.

## Gotchas (learned doing this deployment)

- **Pre-v0.1.1 images silently ignore `S3_SECURE`.** The env var simply
  doesn't exist in the config loader; failures surface as S3 connection
  errors in every worker, not as a config error.
- **Release workflow tag-PR step runs on a tag checkout** where
  `origin/main` doesn't exist; `gh pr create --fill` fails there. Fixed
  for future releases (explicit `--title`/`--body`), but if a release run
  shows red while images published fine, check whether only the
  "Open overlay tag PR" job failed — the `release/vX.Y.Z` branch is
  already pushed and the PR can be opened manually.
- **Endpoint scheme asymmetry:** the Spaces credential we store (for
  rclone and other consumers) keeps the `https://` scheme; this app is
  the odd one out requiring `host:port`. Normalize at the secret-template
  layer, not by storing a second credential.
- **`sslmode=disable` is fine in-cluster** (pod-to-pod traffic on the
  cluster network) but rules out managed databases that enforce TLS.
- **The first GitHub Release had to be v0.1.0** (the overlay ships pinned
  to it; see the bootstrap note in `.github/workflows/release.yml`).
- **argocd-image-updater is not used for this app** (unlike sibling apps
  in the reference cluster): the release workflow owns tag bumps via PR,
  keeping the pinned-tag invariant reviewable.
- **Browser-facing presigned URLs** (`S3_PUBLIC_ENDPOINT`) are only
  needed when the cluster-internal endpoint differs from what a browser
  can reach (local MinIO). With Spaces the endpoint is already public, so
  leave it unset.

## Rolling out upgrades

1. Merge the app changes; cut release `vX.Y.Z`.
2. Confirm the release workflow is green (all 9 image builds + the
   tag-PR job).
3. Merge the `release/vX.Y.Z` tag-bump PR.
4. ArgoCD (selfHeal + automated sync) rolls the new tag; migrate runs
   first via sync-wave. Verify with the checklist above.
