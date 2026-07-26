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

# Guard against accidentally deploying to a non-dev cluster. Local clusters
# are created by ctlptl (see ctlptl.yaml), which also provides the image
# registry that Tilt auto-detects; run `just cluster-up`.
if not k8s_context().startswith('kind-'):
    fail('expected a kind k8s context (see `just cluster-up`), got "%s"' % k8s_context())

docker_build('migrate', '.', build_args={'BUILD_TARGET': './cmd/migrate'})
docker_build('indexer', '.', build_args={'BUILD_TARGET': './cmd/indexer'})
docker_build('files', '.', build_args={'BUILD_TARGET': './cmd/files'})
docker_build('search', '.', build_args={'BUILD_TARGET': './cmd/search'})
docker_build('crawler', '.', build_args={'BUILD_TARGET': './cmd/crawler'})
docker_build('web', 'web')

k8s_yaml(kustomize('deploy'))

k8s_resource(
    'local-s3',
    port_forwards=[
        port_forward(local_port=9000, container_port=9000, name='S3 API Endpoint'),
        port_forward(local_port=9001, container_port=9001, name='MinIO Web Console')
    ],
)
k8s_resource('postgres', port_forwards=5432)

k8s_resource('migrate', resource_deps=['postgres'])
k8s_resource('indexer', resource_deps=['postgres', 'migrate', 'local-s3'])
k8s_resource('files', resource_deps=['postgres', 'migrate', 'local-s3'], port_forwards='50052:50051')
k8s_resource('search', resource_deps=['postgres', 'migrate'], port_forwards='50053:50051')
k8s_resource(
    'web',
    resource_deps=['files', 'search'],
    port_forwards=port_forward(local_port=3000, container_port=3000, name='Web UI'),
)
k8s_resource(
    'crawler',
    resource_deps=['postgres', 'migrate', 'local-s3'],
    trigger_mode=TRIGGER_MODE_MANUAL,
    auto_init=False,
)
