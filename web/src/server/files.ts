import { createServerFn } from '@tanstack/react-start'
import { getFilesClient, getSearchClient } from './clients'
import {
  abortMultipartUploadImpl,
  commitUploadImpl,
  completeMultipartUploadImpl,
  createMultipartUploadImpl,
  deleteDirectoryImpl,
  deleteFileImpl,
  getDirectoryStatsImpl,
  getDownloadUrlImpl,
  getFileMetadataImpl,
  getFilePreviewStatusesImpl,
  getUploadPartUrlImpl,
  getUploadUrlImpl,
  listContentTypesImpl,
  listDirectoryImpl,
  listFilesImpl,
  listUploadedPartsImpl,
  validateCreateMultipartUploadInput,
  validateIdInput,
  validateKeyInput,
  validateListDirectoryInput,
  validateListFilesInput,
  validatePathInput,
  validateUploadIdInput,
  validateUploadPartUrlInput,
} from './impl'

// Thin server-function wrappers; all real logic lives in impl.ts where it is
// unit tested with injected fake clients.

export const listFiles = createServerFn({ method: 'GET' })
  .validator(validateListFilesInput)
  .handler(({ data }) => listFilesImpl(getSearchClient(), getFilesClient(), data))

export const listContentTypes = createServerFn({ method: 'GET' }).handler(() =>
  listContentTypesImpl(getSearchClient()),
)

export const getDownloadUrl = createServerFn({ method: 'GET' })
  .validator(validateIdInput)
  .handler(({ data }) => getDownloadUrlImpl(getFilesClient(), data.id))

export const deleteFile = createServerFn({ method: 'POST' })
  .validator(validateIdInput)
  .handler(({ data }) => deleteFileImpl(getFilesClient(), data.id))

export const getFileMetadata = createServerFn({ method: 'GET' })
  .validator(validateIdInput)
  .handler(({ data }) => getFileMetadataImpl(getFilesClient(), data.id))

export const getUploadUrl = createServerFn({ method: 'GET' })
  .validator(validateKeyInput)
  .handler(({ data }) => getUploadUrlImpl(getFilesClient(), data.key))

export const commitUpload = createServerFn({ method: 'POST' })
  .validator(validateKeyInput)
  .handler(({ data }) => commitUploadImpl(getFilesClient(), data.key))

export const createMultipartUpload = createServerFn({ method: 'POST' })
  .validator(validateCreateMultipartUploadInput)
  .handler(({ data }) => createMultipartUploadImpl(getFilesClient(), data.key, data.contentType))

export const getUploadPartUrl = createServerFn({ method: 'GET' })
  .validator(validateUploadPartUrlInput)
  .handler(({ data }) =>
    getUploadPartUrlImpl(getFilesClient(), data.key, data.uploadId, data.partNumber),
  )

export const listUploadedParts = createServerFn({ method: 'GET' })
  .validator(validateUploadIdInput)
  .handler(({ data }) => listUploadedPartsImpl(getFilesClient(), data.key, data.uploadId))

export const completeMultipartUpload = createServerFn({ method: 'POST' })
  .validator(validateUploadIdInput)
  .handler(({ data }) => completeMultipartUploadImpl(getFilesClient(), data.key, data.uploadId))

export const abortMultipartUpload = createServerFn({ method: 'POST' })
  .validator(validateUploadIdInput)
  .handler(({ data }) => abortMultipartUploadImpl(getFilesClient(), data.key, data.uploadId))

export const listDirectory = createServerFn({ method: 'GET' })
  .validator(validateListDirectoryInput)
  .handler(({ data }) => listDirectoryImpl(getSearchClient(), getFilesClient(), data))

export const getDirectoryStats = createServerFn({ method: 'GET' })
  .validator(validatePathInput)
  .handler(({ data }) => getDirectoryStatsImpl(getFilesClient(), data.path))

export const deleteDirectory = createServerFn({ method: 'POST' })
  .validator(validatePathInput)
  .handler(({ data }) => deleteDirectoryImpl(getFilesClient(), data.path))

export const getFilePreviewStatuses = createServerFn({ method: 'GET' })
  .validator((input: unknown) => {
    const data = (input ?? {}) as Record<string, unknown>
    if (!Array.isArray(data.ids) || !data.ids.every((id) => typeof id === 'string')) {
      throw new Error('ids must be an array of strings')
    }
    return data as { ids: string[] }
  })
  .handler(({ data }) => getFilePreviewStatusesImpl(getFilesClient(), data.ids))
