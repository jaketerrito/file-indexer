# file-indexer

## Dev
### Dependencies
- [protoc](https://protobuf.dev/installation)

### Commands
protoc --proto_path=proto \
  --go_out=internal/pb --go_opt=paths=source_relative \
  --go-grpc_out=internal/pb --go-grpc_opt=paths=source_relative \
  proto/*.proto

GRPC_ADDR=:50051 go run -tags proto ./cmd/crawler
GRPC_ADDR=:50051 go run -tags proto ./cmd/indexer
