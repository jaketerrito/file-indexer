local_resource('proto',
   cmd='just proto',
   deps=['proto'],
   auto_init=False,
)

local_resource('sqlc',
   cmd='just sqlc',
   deps=['internal/db/queries', 'internal/db/migrations'],
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

k8s_yaml('deploy/postgres.yaml')
k8s_yaml('deploy/indexer.yaml')
k8s_yaml('deploy/migrate.yaml')

k8s_resource('postgres', port_forwards=5432)
k8s_resource('migrate', resource_deps=['postgres'])
k8s_resource('indexer', resource_deps=['postgres', 'migrate'], port_forwards=50051)


