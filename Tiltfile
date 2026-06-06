local_resource('proto',
   cmd='protoc --proto_path=proto --go_out=internal/pb --go_opt=paths=source_relative --go-grpc_out=internal/pb --go-grpc_opt=paths=source_relative proto/*.proto',
   deps=['proto'],
   auto_init=False,
)

local_resource('sqlc',
   cmd='sqlc generate',
   deps=['internal/db/queries', 'internal/db/migrations'],
   auto_init=False,
)

k8s_context('microk8s')
default_registry('localhost:32000')

docker_build('migrate', '.', build_args={'BUILD_TARGET': './cmd/migrate'})
docker_build('indexer', '.', build_args={'BUILD_TARGET': './cmd/indexer'})

k8s_yaml('deploy/postgres.yaml')
k8s_yaml('deploy/indexer.yaml')
k8s_yaml('deploy/migrate.yaml')

k8s_resource('migrate', resource_deps=['postgres'])
k8s_resource('indexer', resource_deps=['postgres', 'migrate'], port_forwards=50051)


