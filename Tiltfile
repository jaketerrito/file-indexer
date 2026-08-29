# The web resources (lint, test-web, and the web image) and codegen need npm,
# which ships with Node; install Node >= 24 (see README).
if not str(local('command -v npm || true', quiet=True, echo_off=True)).strip():
    fail('npm not found on PATH; install Node >= 24 (https://nodejs.org) and restart tilt')

# Per-checkout dev environment: hack/dev-env.sh (sourced by the just recipes)
# exports CLUSTER_NAME/K8S_CONTEXT and every host port below, so concurrent
# git worktrees each get their own cluster and non-conflicting port-forwards.
# Defaults reproduce the main checkout's historical values.
DB_PORT = int(os.getenv('DB_PORT', '5432'))
S3_PORT = int(os.getenv('S3_PORT', '9000'))
S3_CONSOLE_PORT = int(os.getenv('S3_CONSOLE_PORT', '9001'))
FILES_PORT = int(os.getenv('FILES_PORT', '50052'))
SEARCH_PORT = int(os.getenv('SEARCH_PORT', '50053'))
WEB_PORT = int(os.getenv('WEB_PORT', '3000'))

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
# checkout's namespace. All checkouts share one kind cluster created by ctlptl
# via `just cluster-up` (see ctlptl.yaml), which also provides the image
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

k8s_yaml(kustomize('deploy'))

# seed-data (minio.yaml's seed sidecar, see deploy/seed.md) is generated here
# rather than via kustomize's configMapGenerator: that only accepts explicit
# file paths, not a directory or glob. This way every file dropped into
# deploy/seed/ is picked up automatically, with no list to keep in sync.
watch_file('deploy/seed')
k8s_yaml(local(
    'kubectl create configmap seed-data --from-file=deploy/seed --dry-run=client -o yaml',
    quiet=True,
))

# The s3-secret is generated here rather than via kustomization.yaml's
# secretGenerator: S3_PUBLIC_ENDPOINT must point at this checkout's forwarded
# MinIO port, which differs per worktree — kustomize has no env substitution.
# Same pattern as the seed-data ConfigMap above.
k8s_yaml(local(
    'kubectl create secret generic s3-secret'
    + ' --from-literal=S3_ENDPOINT=local-s3:9000'
    + ' --from-literal=S3_PUBLIC_ENDPOINT=localhost:%d' % S3_PORT
    + ' --from-literal=S3_ACCESS_ID=user'
    + ' --from-literal=S3_SECRET=password'
    + ' --from-literal=S3_BUCKET=test'
    + ' --from-literal=S3_REGION=us-east-1'
    + ' --dry-run=client -o yaml',
    quiet=True,
))

k8s_resource(
    'local-s3',
    port_forwards=[
        port_forward(local_port=S3_PORT, container_port=9000, name='S3 API Endpoint'),
        port_forward(local_port=S3_CONSOLE_PORT, container_port=9001, name='MinIO Web Console')
    ],
)
k8s_resource('postgres', port_forwards='%d:5432' % DB_PORT)

k8s_resource('migrate', resource_deps=['postgres'])
k8s_resource('index-stat', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('index-preview', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('index-exif', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('files', resource_deps=['postgres', 'migrate', 'local-s3'], port_forwards='%d:50051' % FILES_PORT)
k8s_resource('search', resource_deps=['postgres', 'migrate'], port_forwards='%d:50051' % SEARCH_PORT)
k8s_resource(
    'web',
    resource_deps=['files', 'search'],
    port_forwards=port_forward(local_port=WEB_PORT, container_port=3000, name='Web UI'),
)
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
