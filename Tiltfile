load('ext://namespace', 'namespace_create')

# The web resources (lint, test-web, and the web image) and codegen need npm,
# which ships with Node; install Node >= 24 (see README).
if not str(local('command -v npm || true', quiet=True, echo_off=True)).strip():
    fail('npm not found on PATH; install Node >= 24 (https://nodejs.org) and restart tilt')

local_resource('generate',
   cmd='just generate',
   deps=['internal/db/queries', 'internal/db/migrations', 'proto'],
   auto_init=False,
)

local_resource('lint',
   cmd='just lint',
   deps=['internal/', 'web/src', 'web/biome.json'],
)

local_resource('test-web',
   cmd='just test-web',
   deps=['web/src', 'web/package.json', 'web/vitest.config.ts'],
)

local_resource('test-integration',
   cmd='just test-integration',
   deps=['internal/', 'cmd/'],
   resource_deps=['postgres', 'local-s3', 'migrate'],
)

# Per-checkout dev environment: all checkouts share one kind cluster (`just
# cluster-up`); each deploys into its own namespace via `tilt --namespace`
# (`just tilt-up` derives it from the checkout directory name). Services are
# reached at http://<svc>.<namespace>.localhost via the shared gateway
# (cluster/gateway.yaml) — no per-service host ports.
ns = k8s_namespace()
if ns == '' or ns == 'default':
    fail('no per-checkout namespace: run `just tilt-up` (or pass `tilt up --namespace <name>`)')

# Guard against accidentally deploying to a non-dev cluster. Local clusters
# are created by ctlptl (see cluster/ctlptl.yaml), which also provides the image
# registry that Tilt auto-detects; run `just cluster-up`.
if not k8s_context().startswith('kind-'):
    fail('expected a kind k8s context (see `just cluster-up`), got "%s"' % k8s_context())

# Create this checkout's namespace as a Tilt-managed object (one-time
# extension fetch, cached outside the repo). On the very first `tilt up`,
# namespaced objects may briefly fail to apply while the Namespace resource
# reconciles; Tilt retries automatically and it self-heals.
namespace_create(ns)

docker_build('migrate', '.', build_args={'BUILD_TARGET': './cmd/migrate'})
docker_build('index-stat', '.', build_args={'BUILD_TARGET': './cmd/index-stat'})
docker_build('index-preview', '.', build_args={'BUILD_TARGET': './cmd/index-preview'})
docker_build('index-exif', '.', build_args={'BUILD_TARGET': './cmd/index-exif'})
docker_build('files', '.', build_args={'BUILD_TARGET': './cmd/files'})
docker_build('search', '.', build_args={'BUILD_TARGET': './cmd/search'})
docker_build('crawler', '.', build_args={'BUILD_TARGET': './cmd/crawler'})
docker_build('preview-gc', '.', build_args={'BUILD_TARGET': './cmd/preview-gc'})
docker_build('web', 'web')

k8s_yaml(kustomize('deploy/overlays/local'))

# HTTPRoutes into the shared cluster gateway (cluster/gateway.yaml), generated
# here rather than kept as a manifest: Gateway API hostnames have no downward
# API or env-expansion mechanism, so the per-checkout hostname needs the
# namespace — which only Tilt knows (`tilt --namespace`). Generating the routes
# here keeps every committed manifest namespace-free; no token substitution.
route = """apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: %(name)s
spec:
  parentRefs:
    - name: shared-gateway
      namespace: gateway-system
  hostnames:
    - %(name)s.%(ns)s.localhost
  rules:
    - backendRefs:
        - name: %(svc)s
          port: %(port)d"""
routes = []
for name, svc, port in [
    ('web', 'web', 3000),
    ('s3', 'local-s3', 9000),
    ('s3-console', 'local-s3', 9001),
]:
    routes.append(route % {'name': name, 'ns': ns, 'svc': svc, 'port': port})
k8s_yaml(blob('\n---\n'.join(routes)))

# seed-data (the overlay's seed sidecar, see deploy/overlays/local/seed.md) is generated here
# rather than via kustomize's configMapGenerator: that only accepts explicit
# file paths, not a directory or glob. This way every file dropped into
# deploy/overlays/local/seed/ is picked up automatically, with no list to keep in sync.
watch_file('deploy/overlays/local/seed')
k8s_yaml(local(
    'kubectl create configmap seed-data --from-file=deploy/overlays/local/seed --dry-run=client -o yaml',
    quiet=True,
))

k8s_resource('local-s3', links=['http://s3-console.%s.localhost' % ns])

k8s_resource('migrate', resource_deps=['postgres'])
k8s_resource('index-stat', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('index-preview', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('index-exif', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('files', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('search', resource_deps=['postgres', 'migrate'])
k8s_resource('web', resource_deps=['files', 'search'], links=['http://web.%s.localhost' % ns])
k8s_resource(
    'crawler',
    resource_deps=['postgres', 'migrate', 'local-s3'],
    trigger_mode=TRIGGER_MODE_MANUAL,
    auto_init=False,
)
k8s_resource(
    'preview-gc',
    resource_deps=['postgres', 'migrate', 'local-s3'],
    trigger_mode=TRIGGER_MODE_MANUAL,
    auto_init=False,
)
