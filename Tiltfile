local_resource('generate',
   cmd='just generate',
   deps=['internal/db/queries', 'internal/db/migrations', 'proto'],
   auto_init=False,
)

local_resource('lint',
   cmd='just lint',
   deps=['internal/'],
)

local_resource('test',
   cmd='just test',
   deps=['internal/', 'cmd/'],
)

# Guard against accidentally deploying to a non-dev cluster. Local clusters
# are created by ctlptl (see ctlptl.yaml), which also provides the image
# registry that Tilt auto-detects; run `just cluster-up`.
if not k8s_context().startswith('kind-'):
    fail('expected a kind k8s context (see `just cluster-up`), got "%s"' % k8s_context())

docker_build('migrate', '.', build_args={'BUILD_TARGET': './cmd/migrate'})
docker_build('indexer', '.', build_args={'BUILD_TARGET': './cmd/indexer'})
docker_build('files', '.', build_args={'BUILD_TARGET': './cmd/files'})
docker_build('crawler', '.', build_args={'BUILD_TARGET': './cmd/crawler'})

k8s_yaml(kustomize('deploy'))

k8s_resource(
    'local-s3',
    port_forwards=[
        port_forward(local_port=9000, container_port=9000, name='S3 API Endpoint'),
        port_forward(local_port=9001, container_port=9001, name='MinIO Web Console')
    ],
)
k8s_resource('postgres', port_forwards=5432)

# Full test suite (unit + integration) against the port-forwarded postgres and
# MinIO above, including the coverage threshold check. Runs once on `tilt up`;
# re-run manually from the UI (deliberately not on every file save).
local_resource('test-integration',
   cmd='just test-integration',
   resource_deps=['postgres', 'local-s3'],
   trigger_mode=TRIGGER_MODE_MANUAL,
   auto_init=True,
)
k8s_resource('migrate', resource_deps=['postgres'])
k8s_resource('indexer', resource_deps=['postgres', 'migrate'], port_forwards=50051)
k8s_resource('files', resource_deps=['postgres', 'migrate'], port_forwards=50052)
k8s_resource(
    'crawler',
    resource_deps=['indexer', 'local-s3'],
    trigger_mode=TRIGGER_MODE_MANUAL,
    auto_init=False,
)
