# Production quickstart

This guide deploys the file-indexer stack to a self-hosted Kubernetes cluster
using the `deploy/overlays/production/` kustomize overlay. It assumes you bring
your own Postgres 14+ database and S3-compatible object store.

## 1. Prerequisites

- A Kubernetes cluster (1.28+ recommended).
- [ArgoCD](https://argo-cd.readthedocs.io/) is optional but recommended; a
  plain `kubectl` fallback is documented below.
- Postgres 14+ with the `pg_trgm` extension installed:

  ```sql
  CREATE EXTENSION IF NOT EXISTS pg_trgm;
  ```

- An S3-compatible bucket and credentials with read/write access.
- `kubectl` and `kustomize` installed locally.

## 2. Create secrets

The production overlay reads database and object-store configuration from two
secrets referenced by the base manifests. Create them in the target namespace
before deploying. Replace the placeholder values with your own.

```bash
kubectl create secret generic db-secret \
  --from-literal=DB_HOST=your-db-host.example.com \
  --from-literal=DB_PORT=5432 \
  --from-literal=DB_USER=file_indexer \
  --from-literal=DB_PASSWORD='<REDACTED>' \
  --from-literal=DB_NAME=file_indexer

kubectl create secret generic s3-secret \
  --from-literal=S3_ENDPOINT=s3.example.com \
  --from-literal=S3_ACCESS_ID='<REDACTED>' \
  --from-literal=S3_SECRET='<REDACTED>' \
  --from-literal=S3_BUCKET=your-bucket \
  --from-literal=S3_REGION=us-east-1
```

If browser-facing presigned URLs need a different host than the in-cluster S3
endpoint, patch the files Deployment in your own overlay to add
`S3_PUBLIC_ENDPOINT` as an environment variable. The production overlay does not
set this because the value is environment-specific.

## 3. Preview the overlay

Render the manifests and inspect them before applying:

```bash
kubectl kustomize deploy/overlays/production
```

The rendered output contains:

- Nine workloads pinned to `ghcr.io/jaketerrito/file-indexer/<name>:v0.1.0`.
- A shared `file-indexer` ServiceAccount referenced by every pod.
- A `grpc-health-probe` binary shipped in the release image, with exec readiness/liveness probes on the `files` and `search` app containers.
- HTTP probes for `web`.
- PodDisruptionBudgets for `files`, `search`, and `web`.
- A `migrate` Job annotated with ArgoCD sync-wave `-1`.
- No Postgres, MinIO, Secret, or `.localhost` resources.

All of the hardening items (ServiceAccount, probes, resource requests/limits,
and PodDisruptionBudgets) live in `deploy/base` and apply to every
environment; the production overlay only pins the registry image tags and
adds the ArgoCD sync-wave annotation for `migrate` ordering.

## 4. Deploy with ArgoCD

Apply the example Application. It points at the production overlay and uses
automated sync with prune and self-heal:

```bash
kubectl apply -f deploy/argocd/application.yaml
```

The `migrate` Job runs at sync-wave `-1`, so ArgoCD waits for it to complete
before rolling out the Deployments. Once sync finishes, verify:

```bash
kubectl -n file-indexer get pods
kubectl -n file-indexer get deploy
```

If you prefer to pin ArgoCD to a different release tag, edit
`targetRevision` in `deploy/argocd/application.yaml` or maintain the
Application in your own GitOps repo.

## 5. Deploy with kubectl (no ArgoCD)

Without ArgoCD there is no automatic sync-wave ordering. Apply the full
render, then wait for the `migrate` Job to complete before sending traffic:

```bash
# 1. Render and apply everything.
kubectl kustomize deploy/overlays/production | kubectl apply -f -

# 2. Wait for migrations to finish before considering the deployment healthy.
kubectl wait --for=condition=complete job/migrate -n file-indexer --timeout=300s
```

The Deployments may briefly crash-loop while the Job is running because the
services need a migrated database. The Job's `restartPolicy: OnFailure` will
retry until migrations complete.

## 6. Expose the app

Only the `web` Service is shipped in the overlay. The `files` and `search`
gRPC services on port 50051 are intended to stay cluster-internal. Choose one
of the following exposure patterns for `web`:

### Ingress example (nginx)

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: file-indexer
  namespace: file-indexer
spec:
  rules:
    - host: files.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: web
                port:
                  number: 3000
```

### Gateway API example

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: file-indexer
  namespace: file-indexer
spec:
  parentRefs:
    - name: your-gateway
      namespace: gateway
  hostnames:
    - files.example.com
  rules:
    - backendRefs:
        - name: web
          port: 3000
```

### LoadBalancer example

```bash
kubectl -n file-indexer patch service web -p '{"spec":{"type":"LoadBalancer"}}'
```

No Ingress or Gateway API manifest is committed in `deploy/` because the
hostname and gateway are environment-specific.

## 7. Releases and upgrades

Releases are driven by the `.github/workflows/release.yml` workflow:

1. Publish a GitHub Release with a tag matching `vX.Y.Z`.
2. The workflow builds and pushes all nine images to
   `ghcr.io/jaketerrito/file-indexer/<name>:vX.Y.Z` and `:latest`.
3. A commit-back job opens a PR from `release/vX.Y.Z` that updates only the
   `newTag` fields in `deploy/overlays/production/kustomization.yaml`.
4. Merge the PR to advance the overlay's pinned tag.

The first release should be `v0.1.0` because the overlay ships pinned at that
tag. For that release the commit-back job computes an empty diff and opens no
PR.

## 8. Rollback

To revert to the previous release tag:

```bash
# Edit the overlay tag back to the previous release.
sed -i 's/newTag: .*/newTag: v0.0.9/' deploy/overlays/production/kustomization.yaml
kubectl kustomize deploy/overlays/production | kubectl apply -f -
```

If you are using ArgoCD, you can also roll back from the UI or CLI:

```bash
argocd app rollback file-indexer <revision-id>
```

## 9. Known omissions in this slice

- **Worker and Job health checks:** Probes for `index-stat`, `index-preview`,
  `index-exif`, `crawler`, `migrate`, and `preview-gc` are deferred to
  [issue #117](https://github.com/jaketerrito/file-indexer/issues/117).
- **NetworkPolicies:** A default-deny ingress/egress policy plus allow rules
  for `web`→`files`/`search`, all workloads→Postgres, and all workloads→S3
  are deferred. Track progress in
  [issue #115](https://github.com/jaketerrito/file-indexer/issues/115).
- **HPA / multi-replica:** The overlay keeps `replicas: 1`. Scale in your own
  overlay if you need high availability.
- **Resource limits:** The values in the overlay are conservative starting
  points. Monitor actual usage and adjust them in your own overlay.
