local_resource('generate',
   cmd='just generate',
   deps=['internal/db/queries', 'internal/db/migrations', 'proto'],
   auto_init=False,
)

local_resource('lint',
   cmd='just lint',
   deps=['internal/'],
)

k8s_context('microk8s')
default_registry('localhost:32000')

docker_build('migrate', '.', build_args={'BUILD_TARGET': './cmd/migrate'})
docker_build('indexer', '.', build_args={'BUILD_TARGET': './cmd/indexer'})

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
k8s_resource('indexer', resource_deps=['postgres', 'migrate'], port_forwards=50051)
