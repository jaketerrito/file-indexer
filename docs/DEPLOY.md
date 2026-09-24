# Production deployment guide (agent-focused)

How to deploy file-indexer to a real environment. Written for agents
executing the deployment: every step is explicit, every contract is stated,
and every gotcha we hit is recorded. The examples assume a Kubernetes cluster
with an S3-compatible object store and a GitOps deployer (e.g. ArgoCD), but
only the contracts below are load-bearing — the rest is one way to satisfy
them.

## Architecture contract (what the environment must supply)

`deploy/overlays/production` is a complete kustomization **except** for
environment-specifics. It deliberately ships no `db-secret` / `s3-secret`
and no HTTP route — the environment provides them. Every workload consumes
secrets via `envFrom`, so the secret **keys** are the contract:

| Secret | Keys | Source |
| --- | --- | --- |
| `db-secret` | `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_NAME`, `DB_PASSWORD` | Environment's Postgres |
| `s3-secret` | `S3_ENDPOINT`, `S3_ACCESS_ID`, `S3_SECRET`, `S3_BUCKET`, `S3_REGION` | Environment's bucket credentials |

Hard contracts an agent must not violate:

1. **`S3_ENDPOINT` is `host:port`, no scheme.** `validateEndpoint` in
   `internal/storage/s3.go` rejects any value containing `://`. Hosted
   providers document endpoints with a scheme (e.g.
   `https://s3.example.com`) — strip it and append the port
   (`s3.example.com:443`).
2. **TLS requires `S3_SECURE=true`** (added in v0.1.1). Every binary
   passes it to the S3 client's `secure` flag. This is plain
   configuration, not credential material, so it is **not** part of
   `s3-secret`: base generates an `app-config` ConfigMap from
   `deploy/base/app.env` (`S3_SECURE=false` for local plaintext stores),
   every S3-consuming workload `envFrom`s it, and overlays override keys
   with their own env file (`configMapGenerator behavior: merge`) — the
   production overlay sets `S3_SECURE=true`. New environment knobs follow
   the same pattern: base default + overlay override. Deploying an image
   `< v0.1.1` against a hosted provider fails regardless: the config
   loader doesn't know the variable and the client speaks plaintext HTTP.
3. **Postgres is reached with `sslmode=disable`** (hardcoded in
   `internal/config/config.go`'s `DatabaseConfig.URL`). Use an in-cluster
   Postgres or a database reachable over a trusted network; a managed
   database that *requires* TLS will not work without a code change.
4. **`S3_REGION` is the SigV4 signing region.** Use whatever region the
   provider expects for the bucket; it is usually derivable from the
   endpoint host.
5. **The migrate Job must run before the workloads.** The production
   overlay sets `argocd.argoproj.io/sync-wave: "-1"` on it; if your
   deployer doesn't honor sync waves, order it yourself.
6. **All images share one tag** — the release workflow's sed depends on
   it. Bump tags only via release (see below), never by hand-editing one
   image.

## Step-by-step

One way to satisfy the contract on Kubernetes, using ExternalSecrets-style
templating for secret material (any secret-injection mechanism that lands
the same keys works — sealed secrets, CSI drivers, handwritten Secrets in a
private repo):

1. **Create the DB password** in the environment's secret backend — a
   random 32-byte string is fine.
2. **Deploy Postgres** (or point at an existing one on a trusted network —
   see contract 3). Reference shape: a single-replica Deployment with the
   `Recreate` strategy, a `pg_isready` **TCP** probe (during initdb the
   entrypoint's temporary server listens only on the unix socket, so a
   socket probe reports ready prematurely), `PGDATA` on a PVC subPath, and
   `POSTGRES_USER`/`POSTGRES_DB`/`POSTGRES_PASSWORD` read from the same
   `db-secret` keys the app consumes — one secret serves both.
3. **Create the two secrets.** If the stored bucket credential keeps a
   scheme in its endpoint (common when the same credential is shared with
   other tooling), normalize at the secret-template layer rather than
   storing a second credential — e.g. with external-secrets
   (`engineVersion: v2`, sprig functions):
   `S3_ENDPOINT: '{{ .endpoint | trimPrefix "https://" }}:443'` and the
   region via a matching `trimSuffix`.
4. **Expose the web UI.** Route ingress to Service `web` port 3000. Put
   the route behind whatever SSO/auth proxy the environment uses — the app
   itself has no authn — and register the OAuth callback for the hostname
   with that provider. If TLS and DNS are handled at the edge (ingress /
   Gateway API), no app-side configuration is needed.
5. **Deploy two units, one namespace.** (a) the environment resources from
   steps 2–4, and (b) this repo's `deploy/overlays/production`, both into
   the same namespace. With a GitOps deployer, ordering races at bootstrap
   are self-healing: workloads may crash until the secrets and database
   exist, then retry.
6. **Cut the release that deployable images come from** (GitHub UI or
   `gh release create vX.Y.Z`). The release workflow builds and pushes all
   9 images and opens a mechanical PR (`release/vX.Y.Z` branch) bumping
   the production overlay tag. **Merge that PR** — until then the old tag
   is still deployed.

## Verification checklist

Run after the deployer syncs (agent-executable, in order):

1. Secret injection — both secrets exist in the namespace and the
   injection mechanism reports them ready; a bad template or missing
   backend key shows up here first, not in pod logs.
2. `postgres` Running; `migrate-*` Job Completed (check
   `kubectl logs job/migrate` on failure — usually the DB secret).
3. Workers Running: `files`, `search`, `web`, `crawler`,
   `index-stat`, `index-preview`, `index-exif`. CrashLoopBackOff on all
   workers at once almost always means `s3-secret` (endpoint scheme,
   missing port, or `S3_SECURE` unset against a hosted provider).
4. Trigger a crawl and confirm rows land: query the `files` table via the
   postgres pod, or exercise the `files`/`search` gRPC services over a
   port-forward.
5. Request the web hostname — expect a response from the auth layer (or
   the app, if no auth proxy), not a 502/503.

## Gotchas (learned doing a real rollout)

- **Pre-v0.1.1 images silently ignore `S3_SECURE`.** The env var simply
  doesn't exist in the config loader; failures surface as S3 connection
  errors in every worker, not as a config error.
- **Keep non-secret config out of secrets.** An earlier revision of the
  reference environment carried `S3_SECURE` in `s3-secret`; moving it to
  the `app-config` ConfigMap means rotating the credential never touches
  configuration and the pod spec shows the effective value in plain view.
- **kustomize `behavior: merge` needs matching generatorOptions.** The
  overlay must restate `disableNameSuffixHash: true` — options are
  per-kustomization, and a hashed overlay name never matches base's
  hash-free merge target.
- **Release workflow tag-PR step runs on a tag checkout** where the
  default-branch ref doesn't exist locally; `gh pr create --fill` fails
  there. Fixed for future releases (explicit `--title`/`--body`), but if
  a release run shows red while images published fine, check whether only
  the "Open overlay tag PR" job failed — the `release/vX.Y.Z` branch is
  already pushed and the PR can be opened manually.
- **Endpoint scheme asymmetry:** credentials shared with other tooling
  often store the endpoint with an `https://` scheme; this app is the odd
  one out requiring `host:port`. Normalize at the secret-template layer,
  not by storing a second credential.
- **`sslmode=disable` is fine in-cluster** (pod-to-pod traffic on the
  cluster network) but rules out managed databases that enforce TLS.
- **The first GitHub Release had to be v0.1.0** (the overlay ships pinned
  to it; see the bootstrap note in `.github/workflows/release.yml`).
- **Image tag bumps are owned by the release workflow's PR**, not by an
  image-updater controller, so the pinned-tag invariant stays reviewable.
- **Browser-facing presigned URLs** (`S3_PUBLIC_ENDPOINT`) are only
  needed when the cluster-internal endpoint differs from what a browser
  can reach (e.g. an in-cluster object store in dev). With a hosted
  provider the endpoint is already public, so leave it unset.

## Rolling out upgrades

1. Merge the app changes; cut release `vX.Y.Z`.
2. Confirm the release workflow is green (all 9 image builds + the
   tag-PR job).
3. Merge the `release/vX.Y.Z` tag-bump PR.
4. The deployer rolls the new tag; migrate runs first (sync-wave).
   Verify with the checklist above.
