# The web resources (lint, test-web, and the web image) and codegen need npm,
# which ships with Node; install Node >= 24 (see README).
if not str(local('command -v npm || true', quiet=True, echo_off=True)).strip():
    fail('npm not found on PATH; install Node >= 24 (https://nodejs.org) and restart tilt')

# Per-checkout dev environment: all checkouts share one kind cluster (see
# `tract cluster-up`). Local addressing is per-checkout HOSTNAME —
# `<namespace>.<service>.localhost:$GATEWAY_PORT` — routed by the shared
# cloud-provider-kind gateway; `tract env` (sourced by the just recipes)
# exports K8S_NAMESPACE and friends. There are no per-checkout host ports
# except the Tilt UI.

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

# Guard against accidentally deploying to a non-dev cluster — or into another
# checkout's namespace. All checkouts share one kind cluster created by
# `tract cluster-up`, which also provides the image
# registry Tilt auto-detects. Since the context is shared, the namespace is
# what isolates checkouts: K8S_NAMESPACE pins the expected one, so a bare
# `tilt up` here can't deploy into the main checkout's "default" namespace or
# another worktree's.
expected_namespace = os.getenv('K8S_NAMESPACE', '')
if expected_namespace and k8s_namespace() != expected_namespace:
    fail('expected k8s namespace "%s" for this checkout, got "%s" — run `just up` instead of `tilt up`' % (expected_namespace, k8s_namespace()))
if not k8s_context().startswith('kind-'):
    fail('expected a kind k8s context (see `just cluster-up`), got "%s"' % k8s_context())

docker_build('migrate', '.', build_args={'BUILD_TARGET': './cmd/migrate'})
docker_build('index-stat', '.', build_args={'BUILD_TARGET': './cmd/index-stat'})
docker_build('index-preview', '.', build_args={'BUILD_TARGET': './cmd/index-preview'})
docker_build('index-exif', '.', build_args={'BUILD_TARGET': './cmd/index-exif'})
docker_build('files', '.', build_args={'BUILD_TARGET': './cmd/files'})
docker_build('search', '.', build_args={'BUILD_TARGET': './cmd/search'})
docker_build('crawler', '.', build_args={'BUILD_TARGET': './cmd/crawler'})
docker_build('preview-gc', '.', build_args={'BUILD_TARGET': './cmd/preview-gc'})
docker_build('web', 'web')

# tract render substitutes the per-checkout tokens ($checkout = namespace,
# $gateway_port) in deploy/ manifests — kustomize has no env substitution and
# the gateway port is only known at runtime. Fails loudly if the gateway is
# down, so run via `just up`.
k8s_yaml(local('kubectl kustomize deploy | tract render', quiet=True))
# routes.yaml carries the same placeholders but stays OUT of the
# kustomization so `just lint-k8s` (kubeconform, no tract) never sees them.
k8s_yaml(local('tract render < deploy/routes.yaml', quiet=True))

# seed-data (minio.yaml's seed sidecar, see deploy/seed.md) is generated here
# rather than via kustomize's configMapGenerator: that only accepts explicit
# file paths, not a directory or glob. This way every file dropped into
# deploy/seed/ is picked up automatically, with no list to keep in sync.
watch_file('deploy/seed')
k8s_yaml(local(
    'kubectl create configmap seed-data --from-file=deploy/seed --dry-run=client -o yaml',
    quiet=True,
))

k8s_resource('migrate', resource_deps=['postgres'])
k8s_resource('index-stat', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('index-preview', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('index-exif', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('files', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('search', resource_deps=['postgres', 'migrate'])
k8s_resource('web', resource_deps=['files', 'search'])
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
