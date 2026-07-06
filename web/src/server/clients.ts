import { type Client, createClient } from '@connectrpc/connect'
import { createGrpcTransport } from '@connectrpc/connect-node'
import { FilesService } from '../gen/service/v1/files_pb'
import { SearchService } from '../gen/service/v1/search_pb'

// Server-only module: dials the Go gRPC services directly. Defaults match the
// Tilt port-forwards (files 50052, search 50053) so `pnpm dev` works against
// a running `just up` environment; in-cluster the deployment sets
// FILES_ADDR/SEARCH_ADDR to the k8s service DNS names.

function grpcTransport(addr: string) {
  return createGrpcTransport({ baseUrl: `http://${addr}` })
}

let searchClient: Client<typeof SearchService> | undefined
let filesClient: Client<typeof FilesService> | undefined

export function getSearchClient(): Client<typeof SearchService> {
  searchClient ??= createClient(
    SearchService,
    grpcTransport(process.env.SEARCH_ADDR ?? 'localhost:50053'),
  )
  return searchClient
}

export function getFilesClient(): Client<typeof FilesService> {
  filesClient ??= createClient(
    FilesService,
    grpcTransport(process.env.FILES_ADDR ?? 'localhost:50052'),
  )
  return filesClient
}
