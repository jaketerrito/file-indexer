// Turns freeform user input for an upload destination into a clean S3 key
// prefix. S3 has no real directories — a "path" is just the leading bytes of
// a key — so this is pure string normalization, not filesystem validation.
// The server (FilesServer.validateUploadKey) is the real gate against
// traversal (".."), leading "/", and the reserved index prefix; this only
// spares the user from producing keys like "docscat.jpg" (missing slash) or
// "docs//cat.jpg" (doubled slash) by mistake.
export function normalizeUploadPath(path: string): string {
  const trimmed = path.trim().replace(/^\/+/, '')
  if (trimmed === '') return ''
  const collapsed = trimmed.replace(/\/+/g, '/')
  return collapsed.endsWith('/') ? collapsed : `${collapsed}/`
}
